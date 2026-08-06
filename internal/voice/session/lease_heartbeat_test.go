package session

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func TestDynamoDBRenewLeaseFencesEveryOwningSessionDimension(t *testing.T) {
	now := time.Date(2026, 8, 7, 8, 0, 0, 123, time.UTC)
	current := leaseHeartbeatSession(now)
	renewed := cloneSession(current)
	renewed.LeaseExpiresAt = now.Add(2 * time.Minute)
	renewed.UpdatedAt = now
	attributes, err := marshalSession(renewed)
	if err != nil {
		t.Fatalf("marshal renewed session: %v", err)
	}
	client := &fakeDynamoDB{updateOutputs: []*dynamodb.UpdateItemOutput{{Attributes: attributes}}}
	store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})

	result, err := store.RenewLease(t.Context(), LeaseRenewal{
		Scope:                  sessionScope(current),
		SessionID:              current.ID,
		ExpectedBranchID:       current.BranchID,
		RuntimeSessionID:       current.RuntimeSessionID,
		ExpectedLeaseExpiresAt: current.LeaseExpiresAt,
		ExpectedExpiresAt:      current.ExpiresAt,
		RenewedAt:              now,
		LeaseDuration:          2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("RenewLease() error = %v", err)
	}
	if result == nil || !result.LeaseExpiresAt.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("renewed session = %#v", result)
	}
	if len(client.updateInputs) != 1 {
		t.Fatalf("UpdateItem calls = %d, want 1", len(client.updateInputs))
	}
	input := client.updateInputs[0]
	condition := aws.ToString(input.ConditionExpression)
	for _, fragment := range []string{
		"#session_id = :session_id",
		"#user_id = :user_id",
		"#business_id = :business_id",
		"#branch_id = :expected_branch_id",
		"#runtime_session_id = :runtime_session_id",
		"#status = :active",
		"#runtime_state = :running",
		"#capacity_released = :false",
		"#lease_expires_at = :expected_lease_expires_at",
		"#lease_expires_at > :renewed_at",
		"#expires_at = :expected_expires_at",
		"#expires_at > :renewed_at_epoch",
		"#expires_at >= :lease_expires_at_epoch",
	} {
		if !strings.Contains(condition, fragment) {
			t.Fatalf("renewal condition missing %q: %s", fragment, condition)
		}
	}
	if got := stringValue(t, input.ExpressionAttributeValues[":runtime_session_id"]); got != current.RuntimeSessionID {
		t.Fatalf("runtime fence = %q", got)
	}
	if got := stringValue(t, input.ExpressionAttributeValues[":expected_lease_expires_at"]); got != formatTime(current.LeaseExpiresAt) {
		t.Fatalf("lease fence = %q", got)
	}
	if got := numberValue(t, input.ExpressionAttributeValues[":expected_expires_at"]); got != current.ExpiresAt.Unix() {
		t.Fatalf("product expiry fence = %d", got)
	}
	if got := numberValue(t, input.ExpressionAttributeValues[":lease_expires_at_epoch"]); got != now.Add(2*time.Minute).Unix() {
		t.Fatalf("new lease product fence = %d", got)
	}
	if got := stringValue(t, input.ExpressionAttributeValues[":lease_expires_at"]); got != formatTime(now.Add(2*time.Minute)) {
		t.Fatalf("new lease = %q", got)
	}
	if got := stringValue(t, input.ExpressionAttributeValues[":updated_at"]); got != formatTime(now) {
		t.Fatalf("server update time = %q", got)
	}
	if got := stringValue(t, input.ExpressionAttributeValues[":lease_sk"]); got != leaseSortKey(now.Add(2*time.Minute), current.ID) {
		t.Fatalf("lease index key = %q", got)
	}
}

func TestDynamoDBRenewLeaseNeverExtendsPastProductExpiry(t *testing.T) {
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC)
	current := leaseHeartbeatSession(now)
	current.ExpiresAt = now.Add(45 * time.Second)
	current.LeaseExpiresAt = now.Add(20 * time.Second)
	renewed := cloneSession(current)
	renewed.LeaseExpiresAt = current.ExpiresAt
	renewed.UpdatedAt = now
	attributes, err := marshalSession(renewed)
	if err != nil {
		t.Fatalf("marshal renewed session: %v", err)
	}
	client := &fakeDynamoDB{updateOutputs: []*dynamodb.UpdateItemOutput{{Attributes: attributes}}}
	store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})

	result, err := store.RenewLease(t.Context(), LeaseRenewal{
		Scope: sessionScope(current), SessionID: current.ID, ExpectedBranchID: current.BranchID,
		RuntimeSessionID: current.RuntimeSessionID, ExpectedLeaseExpiresAt: current.LeaseExpiresAt,
		ExpectedExpiresAt: current.ExpiresAt, RenewedAt: now, LeaseDuration: 2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("RenewLease() error = %v", err)
	}
	if !result.LeaseExpiresAt.Equal(current.ExpiresAt) {
		t.Fatalf("lease expiry = %s, want product expiry %s", result.LeaseExpiresAt, current.ExpiresAt)
	}
	if got := stringValue(t, client.updateInputs[0].ExpressionAttributeValues[":lease_expires_at"]); got != formatTime(current.ExpiresAt) {
		t.Fatalf("stored lease exceeds product expiry: %q", got)
	}
}

func TestDynamoDBRenewLeaseMapsConditionalLossAndRejectsTypedNilClient(t *testing.T) {
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC)
	current := leaseHeartbeatSession(now)
	request := LeaseRenewal{
		Scope: sessionScope(current), SessionID: current.ID, ExpectedBranchID: current.BranchID,
		RuntimeSessionID: current.RuntimeSessionID, ExpectedLeaseExpiresAt: current.LeaseExpiresAt,
		ExpectedExpiresAt: current.ExpiresAt, RenewedAt: now, LeaseDuration: 2 * time.Minute,
	}

	conditional := &types.ConditionalCheckFailedException{}
	store := NewDynamoDBStore(&fakeDynamoDB{updateErrors: []error{conditional}}, DynamoDBStoreConfig{TableName: "voice-table"})
	if result, err := store.RenewLease(t.Context(), request); result != nil || !errors.Is(err, ErrLeaseNotRenewable) {
		t.Fatalf("RenewLease(conditional) = (%#v, %v), want ErrLeaseNotRenewable", result, err)
	}

	var typedNil *fakeDynamoDB
	store = NewDynamoDBStore(typedNil, DynamoDBStoreConfig{TableName: "voice-table"})
	if result, err := store.RenewLease(t.Context(), request); result != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("RenewLease(typed nil client) = (%#v, %v), want ErrUnavailable", result, err)
	}
}

func TestLeaseRenewalAndExpiredReconcilerRaceHasExactlyOneWinner(t *testing.T) {
	for attempt := 0; attempt < 100; attempt++ {
		expiresAt := time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)
		current := leaseHeartbeatSession(expiresAt.Add(-time.Minute))
		current.LeaseExpiresAt = expiresAt
		current.ExpiresAt = expiresAt.Add(10 * time.Minute)
		client := &leaseRaceDynamo{current: cloneSession(current)}
		store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
		renewal := LeaseRenewal{
			Scope: sessionScope(current), SessionID: current.ID, ExpectedBranchID: current.BranchID,
			RuntimeSessionID: current.RuntimeSessionID, ExpectedLeaseExpiresAt: current.LeaseExpiresAt,
			ExpectedExpiresAt: current.ExpiresAt, RenewedAt: expiresAt.Add(-time.Millisecond), LeaseDuration: 2 * time.Minute,
		}
		cutoff := expiresAt.Add(time.Millisecond)

		start := make(chan struct{})
		var renewed *Session
		var renewErr error
		var releaseState ReleaseState
		var releaseErr error
		var group sync.WaitGroup
		group.Add(2)
		go func() {
			defer group.Done()
			<-start
			renewed, renewErr = store.RenewLease(context.Background(), renewal)
		}()
		go func() {
			defer group.Done()
			<-start
			_, releaseState, releaseErr = store.ReleaseExpired(context.Background(), cloneSession(current), cutoff)
		}()
		close(start)
		group.Wait()

		if releaseErr != nil {
			t.Fatalf("attempt %d: ReleaseExpired() error = %v", attempt, releaseErr)
		}
		switch {
		case renewErr == nil:
			if renewed == nil || releaseState.Released || !releaseState.LeaseRenewed {
				t.Fatalf("attempt %d: renewal won but release state = %#v, renewed = %#v", attempt, releaseState, renewed)
			}
		case errors.Is(renewErr, ErrLeaseNotRenewable):
			if renewed != nil || !releaseState.Released || releaseState.LeaseRenewed {
				t.Fatalf("attempt %d: release won but release state = %#v, renewed = %#v", attempt, releaseState, renewed)
			}
		default:
			t.Fatalf("attempt %d: unexpected renewal error = %v", attempt, renewErr)
		}
	}
}

func TestLeaseHeartbeatUsesServerClockCoalescesWritesAndAdvancesFence(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	clock := &leaseTestClock{now: now}
	current := leaseHeartbeatSession(now)
	current.UpdatedAt = now.Add(-31 * time.Second)
	renewer := &recordingLeaseRenewer{calls: make(chan LeaseRenewal, 4)}
	heartbeat, err := NewLeaseHeartbeat(LeaseHeartbeatConfig{
		Context: t.Context(), Renewer: renewer, Session: *current,
		LeaseDuration: 2 * time.Minute, MinWriteInterval: 30 * time.Second,
		WriteTimeout: time.Second, Now: clock.Now,
	})
	if err != nil {
		t.Fatalf("NewLeaseHeartbeat() error = %v", err)
	}
	t.Cleanup(func() { _ = heartbeat.Close() })

	if err := heartbeat.Heartbeat(); err != nil {
		t.Fatalf("first Heartbeat() error = %v", err)
	}
	first := awaitLeaseRenewal(t, renewer.calls)
	if !first.RenewedAt.Equal(now) {
		t.Fatalf("first server renewal time = %s, want %s", first.RenewedAt, now)
	}
	for index := 0; index < 64; index++ {
		if err := heartbeat.Heartbeat(); err != nil {
			t.Fatalf("coalesced Heartbeat(%d) error = %v", index, err)
		}
	}
	assertNoLeaseRenewal(t, renewer.calls)

	clock.Advance(29 * time.Second)
	if err := heartbeat.Heartbeat(); err != nil {
		t.Fatalf("early Heartbeat() error = %v", err)
	}
	assertNoLeaseRenewal(t, renewer.calls)
	clock.Advance(time.Second)
	if err := heartbeat.Heartbeat(); err != nil {
		t.Fatalf("eligible Heartbeat() error = %v", err)
	}
	second := awaitLeaseRenewal(t, renewer.calls)
	if !second.ExpectedLeaseExpiresAt.Equal(first.RenewedAt.Add(first.LeaseDuration)) {
		t.Fatalf("second lease fence = %s, want first committed lease %s", second.ExpectedLeaseExpiresAt, first.RenewedAt.Add(first.LeaseDuration))
	}
}

func TestLeaseHeartbeatFailsClosedOnRenewalErrorOrPanicWithoutLeakingCause(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	for name, renewer := range map[string]LeaseRenewer{
		"error": &failingLeaseRenewer{err: errors.New("renewal failed: secret-canary")},
		"panic": &failingLeaseRenewer{panicValue: "panic-secret-canary"},
	} {
		t.Run(name, func(t *testing.T) {
			current := leaseHeartbeatSession(now)
			current.UpdatedAt = now.Add(-time.Minute)
			heartbeat, err := NewLeaseHeartbeat(LeaseHeartbeatConfig{
				Context: t.Context(), Renewer: renewer, Session: *current,
				LeaseDuration: 2 * time.Minute, MinWriteInterval: 30 * time.Second,
				WriteTimeout: time.Second, Now: func() time.Time { return now },
			})
			if err != nil {
				t.Fatalf("NewLeaseHeartbeat() error = %v", err)
			}
			if err := heartbeat.Heartbeat(); err != nil {
				t.Fatalf("initial Heartbeat() error = %v", err)
			}
			select {
			case <-heartbeat.Done():
			case <-time.After(time.Second):
				t.Fatal("heartbeat did not fail closed")
			}
			if heartbeat.Err() != ErrLeaseHeartbeatFailed || strings.Contains(heartbeat.Err().Error(), "canary") {
				t.Fatalf("heartbeat error = %v", heartbeat.Err())
			}
			if err := heartbeat.Heartbeat(); err != ErrLeaseHeartbeatFailed {
				t.Fatalf("Heartbeat(after failure) error = %v, want sentinel", err)
			}
			if err := heartbeat.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
		})
	}
}

func TestNewLeaseHeartbeatRejectsTypedNilRenewer(t *testing.T) {
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	current := leaseHeartbeatSession(now)
	var renewer *recordingLeaseRenewer
	heartbeat, err := NewLeaseHeartbeat(LeaseHeartbeatConfig{
		Context: t.Context(), Renewer: renewer, Session: *current,
		LeaseDuration: 2 * time.Minute, MinWriteInterval: 30 * time.Second,
		WriteTimeout: time.Second, Now: func() time.Time { return now },
	})
	if heartbeat != nil || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("NewLeaseHeartbeat(typed nil) = (%#v, %v), want ErrInvalidRequest", heartbeat, err)
	}
}

func leaseHeartbeatSession(now time.Time) *Session {
	return &Session{
		ID: "voice_01K1ABCDE2FGHIJK3LMNOPQRST", RuntimeSessionID: "voice-session-01K1ABCDE2FGHIJK3LMNOPQRST",
		UserID: "user-1", BusinessID: "11111111-1111-4111-8111-111111111111",
		BranchID: "22222222-2222-4222-8222-222222222222",
		Status:   StatusActive, RuntimeState: RuntimeStateRunning,
		CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
		LeaseExpiresAt: now.Add(90 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
		RotateAt: now.Add(9 * time.Minute),
	}
}

type leaseTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *leaseTestClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *leaseTestClock) Advance(delta time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(delta)
	clock.mu.Unlock()
}

type recordingLeaseRenewer struct {
	calls chan LeaseRenewal
}

func (renewer *recordingLeaseRenewer) RenewLease(_ context.Context, request LeaseRenewal) (*Session, error) {
	renewer.calls <- request
	value := &Session{
		ID: request.SessionID, RuntimeSessionID: request.RuntimeSessionID,
		UserID: request.Scope.UserID, BusinessID: request.Scope.BusinessID, BranchID: request.ExpectedBranchID,
		Status: StatusActive, RuntimeState: RuntimeStateRunning,
		LeaseExpiresAt: request.RenewedAt.Add(request.LeaseDuration), ExpiresAt: request.ExpectedExpiresAt,
		UpdatedAt: request.RenewedAt,
	}
	if value.LeaseExpiresAt.After(value.ExpiresAt) {
		value.LeaseExpiresAt = value.ExpiresAt
	}
	return value, nil
}

type failingLeaseRenewer struct {
	err        error
	panicValue any
}

func (renewer *failingLeaseRenewer) RenewLease(context.Context, LeaseRenewal) (*Session, error) {
	if renewer.panicValue != nil {
		panic(renewer.panicValue)
	}
	return nil, renewer.err
}

func awaitLeaseRenewal(t *testing.T, calls <-chan LeaseRenewal) LeaseRenewal {
	t.Helper()
	select {
	case request := <-calls:
		return request
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for lease renewal")
		return LeaseRenewal{}
	}
}

func assertNoLeaseRenewal(t *testing.T, calls <-chan LeaseRenewal) {
	t.Helper()
	select {
	case request := <-calls:
		t.Fatalf("unexpected lease renewal: %#v", request)
	case <-time.After(25 * time.Millisecond):
	}
}

// leaseRaceDynamo atomically evaluates only the renewal and expired-release
// conditions used by this test. It gives the two real store methods one shared
// durable record, so the race detector also observes the production fencing.
type leaseRaceDynamo struct {
	mu      sync.Mutex
	current *Session
}

func (client *leaseRaceDynamo) GetItem(ctx context.Context, _ *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	item, err := marshalSession(client.current)
	if err != nil {
		return nil, err
	}
	return &dynamodb.GetItemOutput{Item: item}, nil
}

func (client *leaseRaceDynamo) UpdateItem(ctx context.Context, input *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	values := input.ExpressionAttributeValues
	expectedLease, _ := parseTime(attributeString(values, ":expected_lease_expires_at"))
	renewedAt, _ := parseTime(attributeString(values, ":renewed_at"))
	newLease, _ := parseTime(attributeString(values, ":lease_expires_at"))
	if client.current.Status != StatusActive || client.current.RuntimeState != RuntimeStateRunning || client.current.CapacityReleased ||
		client.current.ID != attributeString(values, ":session_id") || client.current.UserID != attributeString(values, ":user_id") ||
		client.current.BusinessID != attributeString(values, ":business_id") || client.current.BranchID != attributeString(values, ":expected_branch_id") ||
		client.current.RuntimeSessionID != attributeString(values, ":runtime_session_id") || !client.current.LeaseExpiresAt.Equal(expectedLease) ||
		!client.current.LeaseExpiresAt.After(renewedAt) || client.current.ExpiresAt.Unix() != numberAttributeValue(values[":expected_expires_at"]) ||
		client.current.ExpiresAt.Unix() <= renewedAt.Unix() {
		return nil, &types.ConditionalCheckFailedException{}
	}
	client.current.LeaseExpiresAt = newLease
	client.current.UpdatedAt = renewedAt
	item, err := marshalSession(client.current)
	if err != nil {
		return nil, err
	}
	return &dynamodb.UpdateItemOutput{Attributes: item}, nil
}

func (client *leaseRaceDynamo) TransactWriteItems(ctx context.Context, input *dynamodb.TransactWriteItemsInput, _ ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	update := input.TransactItems[0].Update
	values := update.ExpressionAttributeValues
	expectedLease, _ := parseTime(attributeString(values, ":expected_lease_expires_at"))
	cutoff, _ := parseTime(attributeString(values, ":cutoff"))
	if client.current.Status != StatusActive || client.current.CapacityReleased ||
		!client.current.LeaseExpiresAt.Equal(expectedLease) || client.current.LeaseExpiresAt.After(cutoff) {
		return nil, &types.TransactionCanceledException{CancellationReasons: []types.CancellationReason{{Code: aws.String("ConditionalCheckFailed")}}}
	}
	client.current.Status = StatusClosing
	client.current.CapacityReleased = true
	client.current.UpdatedAt = cutoff
	return &dynamodb.TransactWriteItemsOutput{}, nil
}

func (*leaseRaceDynamo) Query(context.Context, *dynamodb.QueryInput, ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	return &dynamodb.QueryOutput{}, nil
}

func numberAttributeValue(value types.AttributeValue) int64 {
	number, ok := value.(*types.AttributeValueMemberN)
	if !ok {
		return 0
	}
	var result int64
	for _, digit := range number.Value {
		result = result*10 + int64(digit-'0')
	}
	return result
}
