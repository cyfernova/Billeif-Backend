package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type fakeDynamoDB struct {
	getInputs      []*dynamodb.GetItemInput
	getOutputs     []*dynamodb.GetItemOutput
	getErrors      []error
	transactInputs []*dynamodb.TransactWriteItemsInput
	transactError  error
	updateInputs   []*dynamodb.UpdateItemInput
	updateOutputs  []*dynamodb.UpdateItemOutput
	updateErrors   []error
	queryInputs    []*dynamodb.QueryInput
	queryOutput    *dynamodb.QueryOutput
	queryError     error
}

func (f *fakeDynamoDB) GetItem(_ context.Context, input *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	f.getInputs = append(f.getInputs, input)
	index := len(f.getInputs) - 1
	var output *dynamodb.GetItemOutput
	if index < len(f.getOutputs) {
		output = f.getOutputs[index]
	}
	if output == nil {
		output = &dynamodb.GetItemOutput{}
	}
	if index < len(f.getErrors) {
		return output, f.getErrors[index]
	}
	return output, nil
}

func (f *fakeDynamoDB) TransactWriteItems(_ context.Context, input *dynamodb.TransactWriteItemsInput, _ ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	f.transactInputs = append(f.transactInputs, input)
	return &dynamodb.TransactWriteItemsOutput{}, f.transactError
}

func (f *fakeDynamoDB) UpdateItem(_ context.Context, input *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	f.updateInputs = append(f.updateInputs, input)
	index := len(f.updateInputs) - 1
	var output *dynamodb.UpdateItemOutput
	if index < len(f.updateOutputs) {
		output = f.updateOutputs[index]
	}
	if output == nil {
		output = &dynamodb.UpdateItemOutput{}
	}
	if index < len(f.updateErrors) {
		return output, f.updateErrors[index]
	}
	return output, nil
}

func (f *fakeDynamoDB) Query(_ context.Context, input *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	f.queryInputs = append(f.queryInputs, input)
	if f.queryOutput == nil {
		f.queryOutput = &dynamodb.QueryOutput{}
	}
	return f.queryOutput, f.queryError
}

func TestDynamoDBCreateUsesOneAtomicAdmissionTransaction(t *testing.T) {
	client := &fakeDynamoDB{}
	store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table", LeaseIndexName: "gsi2", GlobalCapacityLimit: 100, PerUserCapacityLimit: 1})
	record := testCreateRecord()

	got, created, err := store.Create(context.Background(), record)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !created || got.ID != record.Session.ID {
		t.Fatalf("unexpected create result created=%v session=%#v", created, got)
	}
	if len(client.transactInputs) != 1 {
		t.Fatalf("expected exactly one transaction, got %d", len(client.transactInputs))
	}
	input := client.transactInputs[0]
	if len(input.TransactItems) != 4 {
		t.Fatalf("unexpected transaction contract: %#v", input)
	}
	if token := aws.ToString(input.ClientRequestToken); len(token) == 0 || len(token) > 36 {
		t.Fatalf("invalid scoped transaction token %q", token)
	}
	global := input.TransactItems[0].Update
	user := input.TransactItems[1].Update
	if stringValue(t, global.Key["pk"]) != "VOICE#CAPACITY" || stringValue(t, global.Key["sk"]) != "GLOBAL" {
		t.Fatalf("wrong global capacity key: %#v", global.Key)
	}
	if stringValue(t, user.Key["pk"]) != "VOICE#CAPACITY#USER#user-1" || stringValue(t, user.Key["sk"]) != "ACTIVE" {
		t.Fatalf("wrong user capacity key: %#v", user.Key)
	}
	for name, update := range map[string]*types.Update{"global": global, "user": user} {
		if update == nil || !strings.Contains(aws.ToString(update.UpdateExpression), "if_not_exists") || !strings.Contains(aws.ToString(update.ConditionExpression), "#active_count < :capacity_limit") {
			t.Fatalf("%s counter is not conditionally incremented: %#v", name, update)
		}
	}
	if put := input.TransactItems[2].Put; put == nil || stringValue(t, put.Item["pk"]) != "VOICE#SESSION#voice_01K1ABCDE2FGHIJK3LMNOPQRST" || stringValue(t, put.Item["GSI2PK"]) != "VOICE#LEASE" {
		t.Fatalf("session put contract is wrong: %#v", put)
	}
	if put := input.TransactItems[3].Put; put == nil || stringValue(t, put.Item["request_hash"]) != record.RequestHash || stringValue(t, put.Item["session_id"]) != record.Session.ID {
		t.Fatalf("idempotency put contract is wrong: %#v", put)
	}
}

func TestDynamoDBCreateMapsAtomicCapacityFailures(t *testing.T) {
	cases := []struct {
		name    string
		index   int
		wantErr error
	}{
		{name: "global", index: 0, wantErr: ErrGlobalCapacity},
		{name: "user", index: 1, wantErr: ErrUserCapacity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reasons := make([]types.CancellationReason, 4)
			reasons[tc.index].Code = aws.String("ConditionalCheckFailed")
			client := &fakeDynamoDB{transactError: &types.TransactionCanceledException{CancellationReasons: reasons}}
			store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
			if _, _, err := store.Create(context.Background(), testCreateRecord()); !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestDynamoDBCreateRecoversIdempotencyRaceAndDetectsConflict(t *testing.T) {
	record := testCreateRecord()
	idem := idempotencyItem(record)
	sessionItem := marshalSessionForTest(t, record.Session)
	reasons := make([]types.CancellationReason, 4)
	reasons[3].Code = aws.String("ConditionalCheckFailed")
	client := &fakeDynamoDB{
		getOutputs:    []*dynamodb.GetItemOutput{{}, {Item: idem}, {Item: sessionItem}},
		transactError: &types.TransactionCanceledException{CancellationReasons: reasons},
	}
	store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
	got, created, err := store.Create(context.Background(), record)
	if err != nil || created || got.ID != record.Session.ID {
		t.Fatalf("race recovery failed created=%v session=%#v err=%v", created, got, err)
	}

	conflictRecord := record
	conflictRecord.RequestHash = "different"
	conflictClient := &fakeDynamoDB{getOutputs: []*dynamodb.GetItemOutput{{Item: idem}}}
	conflictStore := NewDynamoDBStore(conflictClient, DynamoDBStoreConfig{TableName: "voice-table"})
	if _, _, err := conflictStore.Create(context.Background(), conflictRecord); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestDynamoDBResumeExtendsLeaseWithoutAdmissionWrites(t *testing.T) {
	record := testCreateRecord()
	resumed := cloneSession(record.Session)
	resumed.LeaseExpiresAt = resumed.LeaseExpiresAt.Add(time.Minute)
	client := &fakeDynamoDB{updateOutputs: []*dynamodb.UpdateItemOutput{{Attributes: marshalSessionForTest(t, resumed)}}}
	store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
	got, err := store.Resume(context.Background(), ResumeRecord{
		Scope: Scope{UserID: record.Session.UserID, BusinessID: record.Session.BusinessID, AllBranches: true}, SessionID: record.Session.ID,
		ExpectedBranchID:    record.Session.BranchID,
		OldRuntimeSessionID: record.Session.RuntimeSessionID, ExpectedRuntimeState: RuntimeStateRunning,
		ExpectedLeaseExpiresAt: record.Session.LeaseExpiresAt,
		LeaseExpiresAt:         resumed.LeaseExpiresAt, UpdatedAt: resumed.UpdatedAt,
	})
	if err != nil || !got.LeaseExpiresAt.Equal(resumed.LeaseExpiresAt) {
		t.Fatalf("resume failed: %#v %v", got, err)
	}
	if len(client.transactInputs) != 0 || len(client.updateInputs) != 1 {
		t.Fatalf("resume touched admission: transactions=%d updates=%d", len(client.transactInputs), len(client.updateInputs))
	}
	input := client.updateInputs[0]
	if strings.Contains(aws.ToString(input.UpdateExpression), "active_count") || !strings.Contains(aws.ToString(input.ConditionExpression), "#runtime_session_id = :old_runtime_session_id") || !strings.Contains(aws.ToString(input.ConditionExpression), "#runtime_state = :expected_runtime_state") {
		t.Fatalf("unsafe resume expression: %#v", input)
	}
}

func TestDynamoDBReleaseIsOneGuardedTransaction(t *testing.T) {
	record := testCreateRecord()
	client := &fakeDynamoDB{getOutputs: []*dynamodb.GetItemOutput{{Item: marshalSessionForTest(t, record.Session)}}}
	store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
	got, state, err := store.Release(context.Background(), Scope{UserID: record.Session.UserID, BusinessID: record.Session.BusinessID, AllBranches: true}, record.Session.ID, record.Session.UpdatedAt.Add(time.Minute))
	if err != nil || !state.Released || !state.ShouldStop || got.Status != StatusClosing {
		t.Fatalf("release failed state=%#v session=%#v err=%v", state, got, err)
	}
	if len(client.transactInputs) != 1 || len(client.transactInputs[0].TransactItems) != 3 {
		t.Fatalf("release must be one three-item transaction: %#v", client.transactInputs)
	}
	update := client.transactInputs[0].TransactItems[0].Update
	if !strings.Contains(aws.ToString(update.ConditionExpression), "#capacity_released = :false") || !strings.Contains(aws.ToString(update.UpdateExpression), "REMOVE #gsi1pk") {
		t.Fatalf("release is missing one-time guard/index removal: %#v", update)
	}
	for _, item := range client.transactInputs[0].TransactItems[1:] {
		if item.Update == nil || !strings.Contains(aws.ToString(item.Update.ConditionExpression), "#active_count >= :one") {
			t.Fatalf("counter decrement can go negative: %#v", item.Update)
		}
	}
}

func TestDynamoDBExpiredLeasesUsesGSI2BoundedQuery(t *testing.T) {
	client := &fakeDynamoDB{}
	store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table", LeaseIndexName: "gsi2"})
	before := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	if _, err := store.ExpiredLeases(context.Background(), before, 25); err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(client.queryInputs) != 1 {
		t.Fatalf("expected one query, got %d", len(client.queryInputs))
	}
	input := client.queryInputs[0]
	if aws.ToString(input.IndexName) != "gsi2" || aws.ToInt32(input.Limit) != 25 || aws.ToBool(input.ConsistentRead) {
		t.Fatalf("wrong lease query bounds: %#v", input)
	}
	if aws.ToString(input.KeyConditionExpression) != "#gsi2pk = :lease AND #gsi2sk <= :cutoff" {
		t.Fatalf("wrong lease key condition: %q", aws.ToString(input.KeyConditionExpression))
	}
}

func TestDynamoDBGetHidesTenantAndOwnerMismatch(t *testing.T) {
	record := testCreateRecord()
	client := &fakeDynamoDB{getOutputs: []*dynamodb.GetItemOutput{{Item: marshalSessionForTest(t, record.Session)}}}
	store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
	if _, err := store.Get(context.Background(), Scope{UserID: "foreign", BusinessID: record.Session.BusinessID, AllBranches: true}, record.Session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign owner leaked: %v", err)
	}
}

func testCreateRecord() CreateRecord {
	now := time.Date(2026, 8, 6, 7, 0, 0, 0, time.UTC)
	return CreateRecord{
		Session: &Session{
			ID: "voice_01K1ABCDE2FGHIJK3LMNOPQRST", RuntimeSessionID: "voice-session-01K1ABCDE2FGHIJK3LMNOPQRST",
			UserID: "user-1", BusinessID: "business-1", Status: StatusActive, RuntimeState: RuntimeStateRunning,
			ProtocolVersion: 1, KVSChannelIndex: 7, PreferredLanguage: "en-IN", FallbackLanguage: "en-IN", CurrentLanguage: "en-IN",
			ConsentTranscriptStorage: true, ConsentPolicyVersion: "2026-08-01", ClientPlatform: "ios", ClientAppVersion: "1.0.0",
			CreatedAt: now, UpdatedAt: now, LeaseExpiresAt: now.Add(2 * time.Minute), ExpiresAt: now.Add(55 * time.Minute), RotateAt: now.Add(52 * time.Minute),
		},
		IdempotencyKey: "35e046c7-23f4-4c8d-b79d-581229de44ad", RequestHash: "canonical-hash", IdempotencyExpiresAt: now.Add(24 * time.Hour),
	}
}

func stringValue(t *testing.T, value types.AttributeValue) string {
	t.Helper()
	stringValue, ok := value.(*types.AttributeValueMemberS)
	if !ok {
		t.Fatalf("attribute is not a string: %#v", value)
	}
	return stringValue.Value
}

func marshalSessionForTest(t *testing.T, value *Session) map[string]types.AttributeValue {
	t.Helper()
	item, err := marshalSession(value)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	return item
}
