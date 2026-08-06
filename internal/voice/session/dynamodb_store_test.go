package session

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type fakeDynamoDB struct {
	getInputs      []*dynamodb.GetItemInput
	getOutputs     []*dynamodb.GetItemOutput
	getErrors      []error
	transactInputs []*dynamodb.TransactWriteItemsInput
	transactError  error
	transactErrors []error
	updateInputs   []*dynamodb.UpdateItemInput
	updateOutputs  []*dynamodb.UpdateItemOutput
	updateErrors   []error
	queryInputs    []*dynamodb.QueryInput
	queryOutput    *dynamodb.QueryOutput
	queryOutputs   []*dynamodb.QueryOutput
	queryError     error
	queryErrors    []error
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
	index := len(f.transactInputs) - 1
	if index < len(f.transactErrors) {
		return &dynamodb.TransactWriteItemsOutput{}, f.transactErrors[index]
	}
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
	index := len(f.queryInputs) - 1
	if index < len(f.queryOutputs) {
		if index < len(f.queryErrors) {
			return f.queryOutputs[index], f.queryErrors[index]
		}
		return f.queryOutputs[index], nil
	}
	if f.queryOutput == nil {
		f.queryOutput = &dynamodb.QueryOutput{}
	}
	if index < len(f.queryErrors) {
		return f.queryOutput, f.queryErrors[index]
	}
	return f.queryOutput, f.queryError
}

func TestDynamoDBPersistFinalTurnUsesConditionalSequenceConsentAndIdempotentItem(t *testing.T) {
	client := &fakeDynamoDB{}
	store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
	turn := testFinalTurn()

	if err := store.PersistFinalTurn(context.Background(), turn); err != nil {
		t.Fatalf("persist final turn: %v", err)
	}
	if len(client.transactInputs) != 1 {
		t.Fatalf("expected one transaction, got %d", len(client.transactInputs))
	}
	input := client.transactInputs[0]
	if len(input.TransactItems) != 2 {
		t.Fatalf("expected metadata update and immutable turn put: %#v", input.TransactItems)
	}
	update := input.TransactItems[0].Update
	if update == nil {
		t.Fatal("missing metadata update")
	}
	condition := aws.ToString(update.ConditionExpression)
	for _, fragment := range []string{
		"#status = :active",
		"#consent_transcript_storage = :true",
		"#turn_sequence = :previous_sequence",
		"#generation_id < :generation_id",
		"#expires_at >= :turn_expires_at_epoch",
	} {
		if !strings.Contains(condition, fragment) {
			t.Fatalf("turn condition missing %q: %s", fragment, condition)
		}
	}
	if got := stringValue(t, update.Key["pk"]); got != "VOICE#SESSION#"+turn.SessionID {
		t.Fatalf("wrong session key %q", got)
	}
	if got := numberValue(t, update.ExpressionAttributeValues[":previous_sequence"]); got != turn.Sequence-1 {
		t.Fatalf("wrong previous sequence %d", got)
	}
	if got := numberValue(t, update.ExpressionAttributeValues[":turn_expires_at_epoch"]); got != turn.ExpiresAt.Unix() {
		t.Fatalf("turn TTL fence changed: %d", got)
	}
	put := input.TransactItems[1].Put
	if put == nil || aws.ToString(put.ConditionExpression) != "attribute_not_exists(#pk) AND attribute_not_exists(#sk)" {
		t.Fatalf("turn item is not immutable: %#v", put)
	}
	if got := stringValue(t, put.Item["sk"]); got != "TURN#00000000000000000001" {
		t.Fatalf("wrong ordered turn key %q", got)
	}
	if got := stringValue(t, put.Item["transcript"]); got != turn.Transcript {
		t.Fatalf("transcript changed: %q", got)
	}
	if got := stringValue(t, put.Item["response"]); got != turn.Response {
		t.Fatalf("response changed: %q", got)
	}
	if got := stringValue(t, put.Item["detected_language"]); got != turn.ProviderLanguage {
		t.Fatalf("detected language changed: %q", got)
	}
	if got := stringValue(t, put.Item["selected_language"]); got != turn.SelectedLanguage {
		t.Fatalf("selected language changed: %q", got)
	}
	if got := boolValue(t, put.Item["cancelled"]); got {
		t.Fatal("completed turn was marked cancelled")
	}
	if _, exists := put.Item["tool_arguments"]; exists {
		t.Fatal("tool arguments must never be persisted")
	}
	if _, exists := put.Item["tool_results"]; exists {
		t.Fatal("tool results must never be persisted")
	}
	if got := stringValue(t, put.Item["speech_ended_at"]); got != formatTime(turn.Timings.SpeechEndedAt) {
		t.Fatalf("speech boundary changed: %q", got)
	}
	if got := stringValue(t, put.Item["client_first_audio_at"]); got != formatTime(turn.Timings.ClientFirstAudioAt) {
		t.Fatalf("client audio boundary changed: %q", got)
	}
	if got := numberValue(t, put.Item["input_tokens"]); got != int64Value(turn.Usage.InputTokens) {
		t.Fatalf("input usage changed: %d", got)
	}
	if got := numberValue(t, put.Item["tts_characters"]); got != int64Value(turn.Usage.TTSCharacters) {
		t.Fatalf("TTS usage changed: %d", got)
	}
	var tools []FinalTurnToolOutcome
	if err := attributevalue.Unmarshal(put.Item["tools"], &tools); err != nil {
		t.Fatalf("decode retained tool metadata: %v", err)
	}
	if !reflect.DeepEqual(tools, turn.Tools) {
		t.Fatalf("tool metadata changed: got %#v want %#v", tools, turn.Tools)
	}
	if got := numberValue(t, put.Item["expires_at"]); got != turn.ExpiresAt.Unix() {
		t.Fatalf("turn TTL changed: %d", got)
	}
	if token := aws.ToString(input.ClientRequestToken); token == "" || len(token) > 36 {
		t.Fatalf("invalid turn transaction token %q", token)
	}
}

func TestDynamoDBPersistFinalTurnFencesLateTurnTTLToOwningSessionExpiry(t *testing.T) {
	sessionCreatedAt := time.Date(2026, 8, 6, 7, 0, 0, 0, time.UTC)
	sessionExpiresAt := sessionCreatedAt.Add(MaxFinalTurnRetention)
	turn := testFinalTurn()
	turn.CompletedAt = sessionExpiresAt.Add(-time.Minute)
	turn.ExpiresAt = sessionExpiresAt
	turn.Timings = FinalTurnTimings{
		SpeechEndedAt:      turn.CompletedAt.Add(-1500 * time.Millisecond),
		STTFinalAt:         turn.CompletedAt.Add(-1200 * time.Millisecond),
		LLMFirstTokenAt:    turn.CompletedAt.Add(-900 * time.Millisecond),
		TTSFirstAudioAt:    turn.CompletedAt.Add(-600 * time.Millisecond),
		ClientFirstAudioAt: turn.CompletedAt.Add(-300 * time.Millisecond),
	}
	client := &fakeDynamoDB{}
	store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})

	if err := store.PersistFinalTurn(context.Background(), turn); err != nil {
		t.Fatalf("persist late-session turn: %v", err)
	}
	update := client.transactInputs[0].TransactItems[0].Update
	condition := aws.ToString(update.ConditionExpression)
	if !strings.Contains(condition, "#expires_at >= :turn_expires_at_epoch") {
		t.Fatalf("turn TTL is not fenced to the durable session expiry: %s", condition)
	}
	if got := numberValue(t, update.ExpressionAttributeValues[":turn_expires_at_epoch"]); got != sessionExpiresAt.Unix() {
		t.Fatalf("late-session turn TTL fence = %d, want %d", got, sessionExpiresAt.Unix())
	}
	if got := numberValue(t, client.transactInputs[0].TransactItems[1].Put.Item["expires_at"]); got != sessionExpiresAt.Unix() {
		t.Fatalf("late-session turn outlives its owning session: %d", got)
	}
}

func TestDynamoDBPersistFinalTurnTreatsIdenticalDurableItemAsIdempotent(t *testing.T) {
	turn := testFinalTurn()
	conditional := &types.TransactionCanceledException{CancellationReasons: []types.CancellationReason{
		{Code: aws.String("ConditionalCheckFailed")},
		{},
	}}
	client := &fakeDynamoDB{
		transactError: conditional,
		getOutputs:    []*dynamodb.GetItemOutput{{Item: finalTurnItemForTest(t, turn)}},
	}
	store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})

	if err := store.PersistFinalTurn(context.Background(), turn); err != nil {
		t.Fatalf("identical retry must succeed: %v", err)
	}
	if len(client.getInputs) != 1 || !aws.ToBool(client.getInputs[0].ConsistentRead) {
		t.Fatalf("idempotency recovery must consistently read the durable turn: %#v", client.getInputs)
	}
}

func TestDynamoDBPersistFinalTurnRejectsConflictingOrOutOfSequenceWrite(t *testing.T) {
	turn := testFinalTurn()
	conditional := &types.TransactionCanceledException{CancellationReasons: []types.CancellationReason{
		{Code: aws.String("ConditionalCheckFailed")},
		{},
	}}

	t.Run("same sequence with different content", func(t *testing.T) {
		other := turn
		other.Response = "A different response"
		client := &fakeDynamoDB{
			transactError: conditional,
			getOutputs:    []*dynamodb.GetItemOutput{{Item: finalTurnItemForTest(t, other)}},
		}
		store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
		if err := store.PersistFinalTurn(context.Background(), turn); !errors.Is(err, ErrFinalTurnConflict) {
			t.Fatalf("expected conflict, got %v", err)
		}
	})

	t.Run("missing previous sequence", func(t *testing.T) {
		client := &fakeDynamoDB{transactError: conditional}
		store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
		if err := store.PersistFinalTurn(context.Background(), turn); !errors.Is(err, ErrFinalTurnSequence) {
			t.Fatalf("expected sequence rejection, got %v", err)
		}
	})
}

func TestDynamoDBPersistFinalTurnValidatesBoundedFinalOnlyPayload(t *testing.T) {
	valid := testFinalTurn()
	cases := []struct {
		name   string
		mutate func(*FinalTurn)
	}{
		{name: "missing session", mutate: func(turn *FinalTurn) { turn.SessionID = "" }},
		{name: "zero sequence", mutate: func(turn *FinalTurn) { turn.Sequence = 0 }},
		{name: "zero generation", mutate: func(turn *FinalTurn) { turn.GenerationID = 0 }},
		{name: "empty transcript", mutate: func(turn *FinalTurn) { turn.Transcript = "  " }},
		{name: "empty response", mutate: func(turn *FinalTurn) { turn.Response = "" }},
		{name: "unsafe provider language", mutate: func(turn *FinalTurn) { turn.ProviderLanguage = "fr-FR\nsecret" }},
		{name: "unsupported selected language", mutate: func(turn *FinalTurn) { turn.SelectedLanguage = "fr-FR" }},
		{name: "too many tools", mutate: func(turn *FinalTurn) {
			turn.Tools = append(turn.Tools,
				FinalTurnToolOutcome{Name: "one", Success: true},
				FinalTurnToolOutcome{Name: "two", Success: true},
				FinalTurnToolOutcome{Name: "three", Success: true},
			)
		}},
		{name: "unsafe tool failure code", mutate: func(turn *FinalTurn) { turn.Tools[1].ErrorCode = "credential=secret" }},
		{name: "non-monotonic timings", mutate: func(turn *FinalTurn) {
			turn.Timings.STTFinalAt = turn.Timings.SpeechEndedAt.Add(-time.Millisecond)
		}},
		{name: "inconsistent token usage", mutate: func(turn *FinalTurn) {
			value := int64(99)
			turn.Usage.TotalTokens = &value
		}},
		{name: "oversized transcript", mutate: func(turn *FinalTurn) { turn.Transcript = strings.Repeat("x", MaxPersistedTranscriptBytes+1) }},
		{name: "oversized response", mutate: func(turn *FinalTurn) { turn.Response = strings.Repeat("x", MaxPersistedResponseBytes+1) }},
		{name: "expired ttl", mutate: func(turn *FinalTurn) { turn.ExpiresAt = turn.CompletedAt }},
		{name: "retention beyond product cap", mutate: func(turn *FinalTurn) {
			turn.ExpiresAt = turn.CompletedAt.Add(MaxFinalTurnRetention + time.Second)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			turn := valid
			tc.mutate(&turn)
			client := &fakeDynamoDB{}
			store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
			if err := store.PersistFinalTurn(context.Background(), turn); !errors.Is(err, ErrInvalidFinalTurn) {
				t.Fatalf("expected invalid final turn, got %v", err)
			}
			if len(client.transactInputs) != 0 {
				t.Fatal("invalid turn reached DynamoDB")
			}
		})
	}
}

func TestDynamoDBPersistFinalTurnRetainsCancellationWithoutCommittingAssistantText(t *testing.T) {
	turn := testFinalTurn()
	turn.Cancelled = true
	turn.Response = ""
	turn.Timings.LLMFirstTokenAt = time.Time{}
	turn.Timings.TTSFirstAudioAt = time.Time{}
	turn.Timings.ClientFirstAudioAt = time.Time{}
	client := &fakeDynamoDB{}
	store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
	if err := store.PersistFinalTurn(context.Background(), turn); err != nil {
		t.Fatalf("persist cancellation metadata: %v", err)
	}
	item := client.transactInputs[0].TransactItems[1].Put.Item
	if !boolValue(t, item["cancelled"]) || stringValue(t, item["response"]) != "" {
		t.Fatalf("cancelled assistant text was committed: %#v", item)
	}
	for _, absent := range []string{"llm_first_token_at", "tts_first_audio_at", "client_first_audio_at"} {
		if _, exists := item[absent]; exists {
			t.Fatalf("absent cancellation boundary %q was retained: %#v", absent, item[absent])
		}
	}

	unsafe := turn
	unsafe.Response = "stale cancelled text"
	client = &fakeDynamoDB{}
	store = NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
	if err := store.PersistFinalTurn(context.Background(), unsafe); !errors.Is(err, ErrInvalidFinalTurn) {
		t.Fatalf("cancelled response must be rejected, got %v", err)
	}
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

func TestDynamoDBReleaseExpiredUsesObservedLeaseAndOneTimeCapacityTransaction(t *testing.T) {
	record := testCreateRecord()
	now := record.Session.LeaseExpiresAt.Add(time.Second)
	candidate := cloneSession(record.Session)
	client := &fakeDynamoDB{getOutputs: []*dynamodb.GetItemOutput{{Item: marshalSessionForTest(t, candidate)}}}
	store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})

	got, state, err := store.ReleaseExpired(context.Background(), candidate, now)
	if err != nil || !state.Released || !state.ShouldStop || got.Status != StatusClosing {
		t.Fatalf("release expired failed state=%#v session=%#v err=%v", state, got, err)
	}
	if len(client.transactInputs) != 1 || len(client.transactInputs[0].TransactItems) != 3 {
		t.Fatalf("expired release must share the one-time three-item transaction: %#v", client.transactInputs)
	}
	update := client.transactInputs[0].TransactItems[0].Update
	condition := aws.ToString(update.ConditionExpression)
	for _, fragment := range []string{
		"#status = :active",
		"#capacity_released = :false",
		"#lease_expires_at = :expected_lease_expires_at",
		"#lease_expires_at <= :cutoff",
	} {
		if !strings.Contains(condition, fragment) {
			t.Fatalf("expired lease condition missing %q: %s", fragment, condition)
		}
	}
	if got := stringValue(t, update.ExpressionAttributeValues[":expected_lease_expires_at"]); got != formatTime(candidate.LeaseExpiresAt) {
		t.Fatalf("transaction did not fence observed lease: %q", got)
	}
	for _, item := range client.transactInputs[0].TransactItems[1:] {
		if item.Update == nil || !strings.Contains(aws.ToString(item.Update.ConditionExpression), "#active_count >= :one") {
			t.Fatalf("capacity decrement does not share close invariant: %#v", item.Update)
		}
	}
}

func TestDynamoDBReleaseTransactionsUseAttemptSpecificIdempotencyTokens(t *testing.T) {
	record := testCreateRecord()
	store := NewDynamoDBStore(&fakeDynamoDB{}, DynamoDBStoreConfig{TableName: "voice-table"})
	scope := sessionScope(record.Session)
	firstCutoff := record.Session.LeaseExpiresAt.Add(time.Second)
	secondCutoff := firstCutoff.Add(time.Minute)
	lease := record.Session.LeaseExpiresAt

	first := store.releaseTransaction(scope, record.Session, firstCutoff, "reconcile", &lease, &firstCutoff)
	retry := store.releaseTransaction(scope, record.Session, firstCutoff, "reconcile", &lease, &firstCutoff)
	laterAttempt := store.releaseTransaction(scope, record.Session, secondCutoff, "reconcile", &lease, &secondCutoff)
	if aws.ToString(first.ClientRequestToken) != aws.ToString(retry.ClientRequestToken) {
		t.Fatal("an SDK retry of the same transaction lost its idempotency token")
	}
	if aws.ToString(first.ClientRequestToken) == aws.ToString(laterAttempt.ClientRequestToken) {
		t.Fatal("a later reconciliation reused a token for different transaction parameters")
	}
}

func TestDynamoDBReleaseExpiredSkipsRenewedLeaseBeforeAndDuringTransition(t *testing.T) {
	record := testCreateRecord()
	candidate := cloneSession(record.Session)
	now := candidate.LeaseExpiresAt.Add(time.Second)
	renewed := cloneSession(candidate)
	renewed.LeaseExpiresAt = now.Add(time.Minute)

	t.Run("renewed before consistent read", func(t *testing.T) {
		client := &fakeDynamoDB{getOutputs: []*dynamodb.GetItemOutput{{Item: marshalSessionForTest(t, renewed)}}}
		store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
		got, state, err := store.ReleaseExpired(context.Background(), candidate, now)
		if err != nil || !state.LeaseRenewed || state.ShouldStop || !got.LeaseExpiresAt.Equal(renewed.LeaseExpiresAt) {
			t.Fatalf("renewed lease was not skipped state=%#v session=%#v err=%v", state, got, err)
		}
		if len(client.transactInputs) != 0 {
			t.Fatal("renewed lease decremented capacity")
		}
	})

	t.Run("renewed after consistent read", func(t *testing.T) {
		conditional := &types.TransactionCanceledException{CancellationReasons: []types.CancellationReason{
			{Code: aws.String("ConditionalCheckFailed")}, {}, {},
		}}
		client := &fakeDynamoDB{
			getOutputs: []*dynamodb.GetItemOutput{
				{Item: marshalSessionForTest(t, candidate)},
				{Item: marshalSessionForTest(t, renewed)},
			},
			transactError: conditional,
		}
		store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
		got, state, err := store.ReleaseExpired(context.Background(), candidate, now)
		if err != nil || !state.LeaseRenewed || state.ShouldStop || !got.LeaseExpiresAt.Equal(renewed.LeaseExpiresAt) {
			t.Fatalf("renewal race was not fenced state=%#v session=%#v err=%v", state, got, err)
		}
	})
}

func TestDynamoDBReleaseExpiredResumesStopWithoutDoubleDecrementAndSkipsClosed(t *testing.T) {
	record := testCreateRecord()
	candidate := cloneSession(record.Session)
	now := candidate.LeaseExpiresAt.Add(time.Second)

	t.Run("capacity already released", func(t *testing.T) {
		closing := cloneSession(candidate)
		closing.Status = StatusClosing
		closing.CapacityReleased = true
		client := &fakeDynamoDB{getOutputs: []*dynamodb.GetItemOutput{{Item: marshalSessionForTest(t, closing)}}}
		store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
		got, state, err := store.ReleaseExpired(context.Background(), candidate, now)
		if err != nil || state.Released || !state.ShouldStop || got.Status != StatusClosing {
			t.Fatalf("closing retry contract failed state=%#v session=%#v err=%v", state, got, err)
		}
		if len(client.transactInputs) != 0 {
			t.Fatal("already-released capacity was decremented twice")
		}
	})

	t.Run("closed", func(t *testing.T) {
		closed := cloneSession(candidate)
		closed.Status = StatusClosed
		closed.CapacityReleased = true
		client := &fakeDynamoDB{getOutputs: []*dynamodb.GetItemOutput{{Item: marshalSessionForTest(t, closed)}}}
		store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table"})
		_, state, err := store.ReleaseExpired(context.Background(), candidate, now)
		if err != nil || !state.AlreadyClosed || state.ShouldStop {
			t.Fatalf("closed session was not skipped state=%#v err=%v", state, err)
		}
		if len(client.transactInputs) != 0 {
			t.Fatal("closed session touched capacity")
		}
	})
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

func TestDynamoDBExpiredLeasesPaginatesWithinCallerBound(t *testing.T) {
	first := testCreateRecord().Session
	second := cloneSession(first)
	second.ID = "voice_01K1ABCDE2FGHIJK3LMNOPQRSU"
	second.RuntimeSessionID = "voice-session-01K1ABCDE2FGHIJK3LMNOPQRSU"
	cursor := map[string]types.AttributeValue{
		"pk": stringAttribute(sessionPK(first.ID)), "sk": stringAttribute(sessionSortKey),
		"GSI2PK": stringAttribute(leasePartitionKey), "GSI2SK": stringAttribute(leaseSortKey(first.LeaseExpiresAt, first.ID)),
	}
	client := &fakeDynamoDB{queryOutputs: []*dynamodb.QueryOutput{
		{Items: []map[string]types.AttributeValue{marshalSessionForTest(t, first)}, LastEvaluatedKey: cursor},
		{Items: []map[string]types.AttributeValue{marshalSessionForTest(t, second)}},
	}}
	store := NewDynamoDBStore(client, DynamoDBStoreConfig{TableName: "voice-table", LeaseIndexName: "gsi2"})

	got, err := store.ExpiredLeases(context.Background(), first.LeaseExpiresAt.Add(time.Minute), 2)
	if err != nil {
		t.Fatalf("query pages: %v", err)
	}
	if len(got) != 2 || got[0].ID != first.ID || got[1].ID != second.ID {
		t.Fatalf("wrong paginated candidates: %#v", got)
	}
	if len(client.queryInputs) != 2 {
		t.Fatalf("expected two bounded pages, got %d", len(client.queryInputs))
	}
	if !reflect.DeepEqual(client.queryInputs[1].ExclusiveStartKey, cursor) || aws.ToInt32(client.queryInputs[1].Limit) != 1 {
		t.Fatalf("second page did not continue within remaining limit: %#v", client.queryInputs[1])
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

func numberValue(t *testing.T, value types.AttributeValue) int64 {
	t.Helper()
	number, ok := value.(*types.AttributeValueMemberN)
	if !ok {
		t.Fatalf("attribute is not a number: %#v", value)
	}
	parsed, err := strconv.ParseInt(number.Value, 10, 64)
	if err != nil {
		t.Fatalf("parse number %q: %v", number.Value, err)
	}
	return parsed
}

func boolValue(t *testing.T, value types.AttributeValue) bool {
	t.Helper()
	boolean, ok := value.(*types.AttributeValueMemberBOOL)
	if !ok {
		t.Fatalf("attribute is not a bool: %#v", value)
	}
	return boolean.Value
}

func testFinalTurn() FinalTurn {
	completedAt := time.Date(2026, 8, 6, 7, 1, 0, 0, time.UTC)
	inputTokens := int64(42)
	outputTokens := int64(18)
	totalTokens := int64(60)
	ttsCharacters := int64(29)
	return FinalTurn{
		SessionID: "voice_01K1ABCDE2FGHIJK3LMNOPQRST",
		Sequence:  1, GenerationID: 7,
		Transcript: "Show my latest invoice.", ProviderLanguage: "fr-FR", SelectedLanguage: "ta-IN",
		Response: "Your latest invoice is ready.",
		Tools: []FinalTurnToolOutcome{
			{Name: "get_invoice", Success: true},
			{Name: "list_invoices", Success: false, ErrorCode: "execution_failed"},
		},
		Timings: FinalTurnTimings{
			SpeechEndedAt:      completedAt.Add(-1500 * time.Millisecond),
			STTFinalAt:         completedAt.Add(-1200 * time.Millisecond),
			LLMFirstTokenAt:    completedAt.Add(-900 * time.Millisecond),
			TTSFirstAudioAt:    completedAt.Add(-600 * time.Millisecond),
			ClientFirstAudioAt: completedAt.Add(-300 * time.Millisecond),
		},
		Usage: FinalTurnUsage{
			InputTokens: &inputTokens, OutputTokens: &outputTokens,
			TotalTokens: &totalTokens, TTSCharacters: &ttsCharacters,
		},
		CompletedAt: completedAt,
		ExpiresAt:   completedAt.Add(55 * time.Minute),
	}
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func finalTurnItemForTest(t *testing.T, turn FinalTurn) map[string]types.AttributeValue {
	t.Helper()
	item, err := marshalFinalTurn(turn)
	if err != nil {
		t.Fatalf("marshal final turn: %v", err)
	}
	return item
}

func marshalSessionForTest(t *testing.T, value *Session) map[string]types.AttributeValue {
	t.Helper()
	item, err := marshalSession(value)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	return item
}
