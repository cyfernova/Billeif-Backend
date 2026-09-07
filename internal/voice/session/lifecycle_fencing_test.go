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

const (
	testBranchA = "cbd6e793-62e6-4c32-a106-065709caf460"
	testBranchB = "de6c25e7-a096-4fef-980c-10c49d7691dc"
)

func TestServiceRestrictedScopeRequiresAllowedBranch(t *testing.T) {
	now := time.Date(2026, 8, 6, 7, 0, 0, 0, time.UTC)
	svc := NewService(newMemoryStore(), &stopRecorder{}, testConfig(), allowServiceOptions(ServiceOptions{
		Now:     func() time.Time { return now },
		NewULID: func() string { return "01K1ABCDE2FGHIJK3LMNOPQRST" },
	}))
	restricted := Scope{UserID: "user-1", BusinessID: "business-1", AllowedBranchIDs: []string{testBranchA}}

	omitted := validCreateInput()
	if _, err := svc.Create(context.Background(), restricted, omitted); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("restricted create without branch must fail validation, got %v", err)
	}

	foreign := validCreateInput()
	foreign.BranchID = testBranchB
	if _, err := svc.Create(context.Background(), restricted, foreign); !errors.Is(err, ErrBranchForbidden) {
		t.Fatalf("restricted create for foreign branch must be forbidden, got %v", err)
	}

	allowed := validCreateInput()
	allowed.BranchID = testBranchA
	if _, err := svc.Create(context.Background(), restricted, allowed); err != nil {
		t.Fatalf("restricted create for allowlisted branch: %v", err)
	}
}

func TestServiceHidesSessionAfterBranchAccessIsRevoked(t *testing.T) {
	now := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	store := newMemoryStore()
	store.sessions["voice_private"] = &Session{
		ID: "voice_private", RuntimeSessionID: "voice-session-private-123456789012345",
		UserID: "user-1", BusinessID: "business-1", BranchID: testBranchA,
		Status: StatusActive, RuntimeState: RuntimeStateRunning,
		LeaseExpiresAt: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	svc := NewService(store, &stopRecorder{}, testConfig(), allowServiceOptions(ServiceOptions{Now: func() time.Time { return now }}))
	revoked := Scope{UserID: "user-1", BusinessID: "business-1", AllowedBranchIDs: []string{testBranchB}}

	if _, err := svc.Get(context.Background(), revoked, "voice_private"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked branch should hide get, got %v", err)
	}
	if _, err := svc.Resume(context.Background(), revoked, "voice_private"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked branch should hide resume, got %v", err)
	}
	if err := svc.Close(context.Background(), revoked, "voice_private"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked branch should hide close, got %v", err)
	}
}

func TestServiceResumableRequiresSessionAndLeaseStrictlyAfterNow(t *testing.T) {
	now := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		leaseEnd   time.Time
		sessionEnd time.Time
	}{
		{name: "lease boundary", leaseEnd: now, sessionEnd: now.Add(time.Hour)},
		{name: "session boundary", leaseEnd: now.Add(time.Minute), sessionEnd: now},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryStore()
			stored := &Session{
				ID: "voice_boundary", RuntimeSessionID: "voice-session-boundary-12345678901234",
				UserID: "u", BusinessID: "b", Status: StatusActive, RuntimeState: RuntimeStateRunning,
				LeaseExpiresAt: tc.leaseEnd, ExpiresAt: tc.sessionEnd,
			}
			store.sessions[stored.ID] = stored
			svc := NewService(store, &stopRecorder{}, testConfig(), allowServiceOptions(ServiceOptions{Now: func() time.Time { return now }}))
			scope := Scope{UserID: "u", BusinessID: "b", AllBranches: true}

			got, err := svc.Get(context.Background(), scope, stored.ID)
			if err != nil {
				t.Fatalf("get boundary session: %v", err)
			}
			if got.Resumable {
				t.Fatal("boundary session must not be resumable")
			}
			if _, err := svc.Resume(context.Background(), scope, stored.ID); !errors.Is(err, ErrNotResumable) {
				t.Fatalf("boundary session must reject resume, got %v", err)
			}
		})
	}
}

func TestDynamoDBResumeFencesBranchAndExactLiveLease(t *testing.T) {
	record := testCreateRecord()
	record.Session.BranchID = testBranchA
	resumed := cloneSession(record.Session)
	resumed.LeaseExpiresAt = resumed.LeaseExpiresAt.Add(time.Minute)
	client := &fakeDynamoDB{updateOutputs: []*dynamodb.UpdateItemOutput{{Attributes: marshalSessionForTest(t, resumed)}}}
	store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
	_, err := store.Resume(context.Background(), ResumeRecord{
		Scope:     Scope{UserID: record.Session.UserID, BusinessID: record.Session.BusinessID, AllowedBranchIDs: []string{testBranchA}},
		SessionID: record.Session.ID, ExpectedBranchID: testBranchA,
		OldRuntimeSessionID: record.Session.RuntimeSessionID, ExpectedRuntimeState: RuntimeStateRunning,
		ExpectedLeaseExpiresAt: record.Session.LeaseExpiresAt,
		LeaseExpiresAt:         resumed.LeaseExpiresAt, UpdatedAt: record.Session.UpdatedAt.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	input := client.updateInputs[0]
	condition := aws.ToString(input.ConditionExpression)
	for _, fragment := range []string{
		"#branch_id = :expected_branch_id",
		"#lease_expires_at = :expected_lease_expires_at",
		"#lease_expires_at > :now",
	} {
		if !strings.Contains(condition, fragment) {
			t.Fatalf("resume condition missing %q: %s", fragment, condition)
		}
	}
}

func TestDynamoDBResumeRejectsBranchChangeAndRenewedLeaseRace(t *testing.T) {
	record := testCreateRecord()
	record.Session.BranchID = testBranchA
	conditional := &types.ConditionalCheckFailedException{}

	t.Run("branch changed after read", func(t *testing.T) {
		changed := cloneSession(record.Session)
		changed.BranchID = testBranchB
		client := &fakeDynamoDB{
			updateErrors: []error{conditional},
			getOutputs:   []*dynamodb.GetItemOutput{{Item: marshalSessionForTest(t, changed)}},
		}
		store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
		_, err := store.Resume(context.Background(), ResumeRecord{
			Scope:     Scope{UserID: record.Session.UserID, BusinessID: record.Session.BusinessID, AllowedBranchIDs: []string{testBranchA}},
			SessionID: record.Session.ID, ExpectedBranchID: testBranchA,
			OldRuntimeSessionID: record.Session.RuntimeSessionID, ExpectedRuntimeState: RuntimeStateRunning,
			ExpectedLeaseExpiresAt: record.Session.LeaseExpiresAt,
			LeaseExpiresAt:         record.Session.LeaseExpiresAt.Add(time.Minute), UpdatedAt: record.Session.UpdatedAt.Add(time.Minute),
		})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("changed branch must be hidden, got %v", err)
		}
	})

	t.Run("lease changed by reconciler", func(t *testing.T) {
		client := &fakeDynamoDB{
			updateErrors: []error{conditional},
			getOutputs:   []*dynamodb.GetItemOutput{{Item: marshalSessionForTest(t, record.Session)}},
		}
		store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
		_, err := store.Resume(context.Background(), ResumeRecord{
			Scope:     Scope{UserID: record.Session.UserID, BusinessID: record.Session.BusinessID, AllowedBranchIDs: []string{testBranchA}},
			SessionID: record.Session.ID, ExpectedBranchID: testBranchA,
			OldRuntimeSessionID: record.Session.RuntimeSessionID, ExpectedRuntimeState: RuntimeStateRunning,
			ExpectedLeaseExpiresAt: record.Session.LeaseExpiresAt,
			LeaseExpiresAt:         record.Session.LeaseExpiresAt.Add(time.Minute), UpdatedAt: record.Session.UpdatedAt.Add(time.Minute),
		})
		if !errors.Is(err, ErrNotResumable) {
			t.Fatalf("conditional lease race must reject resume, got %v", err)
		}
	})
}

func TestDynamoDBClosingRemainsDiscoverableUntilTerminalClose(t *testing.T) {
	record := testCreateRecord()
	record.Session.BranchID = testBranchA
	now := record.Session.UpdatedAt.Add(time.Minute)
	scope := Scope{UserID: record.Session.UserID, BusinessID: record.Session.BusinessID, AllowedBranchIDs: []string{testBranchA}}
	client := &fakeDynamoDB{getOutputs: []*dynamodb.GetItemOutput{{Item: marshalSessionForTest(t, record.Session)}}}
	store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})

	if _, _, err := store.Release(context.Background(), scope, record.Session.ID, now); err != nil {
		t.Fatalf("release: %v", err)
	}
	release := client.transactInputs[0].TransactItems[0].Update
	update := aws.ToString(release.UpdateExpression)
	if strings.Contains(update, "#gsi2pk") && strings.Contains(update, "REMOVE #gsi2") {
		t.Fatalf("closing release removed lease discovery keys: %s", update)
	}
	if !strings.Contains(update, "#gsi2pk = :lease_pk") || !strings.Contains(update, "#gsi2sk = :stop_pending_sk") {
		t.Fatalf("closing release must re-key immediate stop work: %s", update)
	}
	if got := stringValue(t, release.ExpressionAttributeValues[":stop_pending_sk"]); got != leaseSortKey(now, record.Session.ID) {
		t.Fatalf("closing session not immediately eligible: %q", got)
	}

	if err := store.MarkClosed(context.Background(), scope, record.Session.ID, testBranchA, now); err != nil {
		t.Fatalf("mark closed: %v", err)
	}
	closed := client.updateInputs[0]
	if !strings.Contains(aws.ToString(closed.UpdateExpression), "REMOVE #gsi2pk, #gsi2sk") {
		t.Fatalf("terminal close must remove lease discovery keys: %#v", closed)
	}
	if !strings.Contains(aws.ToString(closed.ConditionExpression), "#branch_id = :expected_branch_id") {
		t.Fatalf("terminal close is not branch-fenced: %#v", closed)
	}
}

func TestDynamoDBReleaseHidesBranchChangedAfterRead(t *testing.T) {
	record := testCreateRecord()
	record.Session.BranchID = testBranchA
	changed := cloneSession(record.Session)
	changed.BranchID = testBranchB
	client := &fakeDynamoDB{
		getOutputs: []*dynamodb.GetItemOutput{
			{Item: marshalSessionForTest(t, record.Session)},
			{Item: marshalSessionForTest(t, changed)},
		},
		transactError: &types.TransactionCanceledException{},
	}
	store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
	scope := Scope{UserID: record.Session.UserID, BusinessID: record.Session.BusinessID, AllowedBranchIDs: []string{testBranchA}}

	_, _, err := store.Release(context.Background(), scope, record.Session.ID, record.Session.UpdatedAt.Add(time.Minute))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("branch changed after release read must be hidden, got %v", err)
	}
	condition := aws.ToString(client.transactInputs[0].TransactItems[0].Update.ConditionExpression)
	if !strings.Contains(condition, "#branch_id = :expected_branch_id") {
		t.Fatalf("release write is not branch-fenced: %s", condition)
	}
}
