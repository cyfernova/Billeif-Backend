package session

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	sessionSortKey     = "META"
	globalCapacityPK   = "VOICE#CAPACITY"
	globalCapacitySK   = "GLOBAL"
	userCapacitySK     = "ACTIVE"
	idempotencySortKey = "REQUEST"
	leasePartitionKey  = "VOICE#LEASE"
	timeKeyFormat      = "2006-01-02T15:04:05.000000000Z07:00"
)

var finalTurnMetadataPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type DynamoDBAPI interface {
	GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	TransactWriteItems(context.Context, *dynamodb.TransactWriteItemsInput, ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
	UpdateItem(context.Context, *dynamodb.UpdateItemInput, ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	Query(context.Context, *dynamodb.QueryInput, ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
}

type DynamoDBStoreConfig struct {
	TableName            string
	LeaseIndexName       string
	GlobalCapacityLimit  int64
	PerUserCapacityLimit int64
}

type DynamoDBStore struct {
	db                   DynamoDBAPI
	tableName            string
	leaseIndexName       string
	globalCapacityLimit  int64
	perUserCapacityLimit int64
}

func NewDynamoDBStore(db DynamoDBAPI, cfg DynamoDBStoreConfig) *DynamoDBStore {
	if cfg.LeaseIndexName == "" {
		cfg.LeaseIndexName = "gsi2"
	}
	if cfg.GlobalCapacityLimit == 0 {
		cfg.GlobalCapacityLimit = 100
	}
	if cfg.PerUserCapacityLimit == 0 {
		cfg.PerUserCapacityLimit = 1
	}
	return &DynamoDBStore{
		db: db, tableName: cfg.TableName, leaseIndexName: cfg.LeaseIndexName,
		globalCapacityLimit: cfg.GlobalCapacityLimit, perUserCapacityLimit: cfg.PerUserCapacityLimit,
	}
}

func (s *DynamoDBStore) PersistFinalTurn(ctx context.Context, turn FinalTurn) error {
	if err := s.validate(); err != nil {
		return err
	}
	if err := validateFinalTurn(turn); err != nil {
		return err
	}
	item, err := marshalFinalTurn(turn)
	if err != nil {
		return fmt.Errorf("marshal final voice turn: %w", ErrInvalidFinalTurn)
	}
	hash := attributeString(item, "content_hash")
	input := &dynamodb.TransactWriteItemsInput{
		ClientRequestToken: aws.String(transactionToken("turn", turn.SessionID+"|"+strconv.FormatInt(turn.Sequence, 10)+"|"+hash)),
		TransactItems: []types.TransactWriteItem{
			{Update: &types.Update{
				TableName:           aws.String(s.tableName),
				Key:                 sessionKey(turn.SessionID),
				ConditionExpression: aws.String("#status = :active AND #consent_transcript_storage = :true AND #turn_sequence = :previous_sequence AND #generation_id < :generation_id AND #expires_at > :completed_at_epoch AND #expires_at >= :turn_expires_at_epoch"),
				UpdateExpression:    aws.String("SET #turn_sequence = :turn_sequence, #generation_id = :generation_id, #updated_at = :updated_at"),
				ExpressionAttributeNames: map[string]string{
					"#status": "status", "#consent_transcript_storage": "consent_transcript_storage",
					"#turn_sequence": "turn_sequence", "#generation_id": "generation_id",
					"#expires_at": "expires_at", "#updated_at": "updated_at",
				},
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":active": stringAttribute(string(StatusActive)), ":true": boolAttribute(true),
					":previous_sequence": numberAttribute(turn.Sequence - 1), ":turn_sequence": numberAttribute(turn.Sequence),
					":generation_id": numberAttribute(turn.GenerationID), ":completed_at_epoch": numberAttribute(turn.CompletedAt.Unix()),
					":turn_expires_at_epoch": numberAttribute(turn.ExpiresAt.Unix()),
					":updated_at":            stringAttribute(formatTime(turn.CompletedAt)),
				},
			}},
			{Put: &types.Put{
				TableName: aws.String(s.tableName), Item: item,
				ConditionExpression:      aws.String("attribute_not_exists(#pk) AND attribute_not_exists(#sk)"),
				ExpressionAttributeNames: map[string]string{"#pk": "pk", "#sk": "sk"},
			}},
		},
	}
	if _, err := s.db.TransactWriteItems(ctx, input); err == nil {
		return nil
	} else {
		durableHash, found, lookupErr := s.lookupFinalTurnHash(ctx, turn.SessionID, turn.Sequence)
		if lookupErr != nil {
			return lookupErr
		}
		if found {
			if durableHash == hash {
				return nil
			}
			return ErrFinalTurnConflict
		}
		if cancellationReasonIs(err, 0, "ConditionalCheckFailed") {
			return ErrFinalTurnSequence
		}
		if cancellationReasonIs(err, 1, "ConditionalCheckFailed") {
			return ErrFinalTurnConflict
		}
		return fmt.Errorf("persist final voice turn: %w", err)
	}
}

func (s *DynamoDBStore) Create(ctx context.Context, record CreateRecord) (*Session, bool, error) {
	if err := s.validate(); err != nil {
		return nil, false, err
	}
	if record.Session == nil {
		return nil, false, fmt.Errorf("create voice session: %w", ErrInvalidRequest)
	}
	if existing, found, err := s.lookupIdempotency(ctx, record); err != nil {
		return nil, false, err
	} else if found {
		return existing, false, nil
	}

	sessionItem, err := marshalSession(record.Session)
	if err != nil {
		return nil, false, fmt.Errorf("marshal voice session: %w", err)
	}
	nowValue := stringAttribute(formatTime(record.Session.UpdatedAt))
	transaction := &dynamodb.TransactWriteItemsInput{
		ClientRequestToken: aws.String(transactionToken("create", record.Session.UserID+"|"+record.IdempotencyKey+"|"+record.RequestHash)),
		TransactItems: []types.TransactWriteItem{
			{Update: capacityIncrement(s.tableName, globalCapacityPK, globalCapacitySK, s.globalCapacityLimit, nowValue)},
			{Update: capacityIncrement(s.tableName, userCapacityPK(record.Session.UserID), userCapacitySK, s.perUserCapacityLimit, nowValue)},
			{Put: &types.Put{TableName: aws.String(s.tableName), Item: sessionItem, ConditionExpression: aws.String("attribute_not_exists(#pk)"), ExpressionAttributeNames: map[string]string{"#pk": "pk"}}},
			{Put: &types.Put{TableName: aws.String(s.tableName), Item: idempotencyItem(record), ConditionExpression: aws.String("attribute_not_exists(#pk)"), ExpressionAttributeNames: map[string]string{"#pk": "pk"}}},
		},
	}
	if _, err := s.db.TransactWriteItems(ctx, transaction); err != nil {
		// A concurrent identical request may win between the consistent pre-read and
		// transaction. Read the durable idempotency record before interpreting which
		// condition lost, so retries still return the original session at capacity.
		if existing, found, lookupErr := s.lookupIdempotency(ctx, record); lookupErr != nil {
			return nil, false, lookupErr
		} else if found {
			return existing, false, nil
		}
		if cancellationReasonIs(err, 0, "ConditionalCheckFailed") {
			return nil, false, ErrGlobalCapacity
		}
		if cancellationReasonIs(err, 1, "ConditionalCheckFailed") {
			return nil, false, ErrUserCapacity
		}
		if cancellationReasonIs(err, 3, "ConditionalCheckFailed") {
			return nil, false, ErrIdempotencyConflict
		}
		return nil, false, fmt.Errorf("create voice session transaction: %w", err)
	}
	return cloneSession(record.Session), true, nil
}

func (s *DynamoDBStore) Get(ctx context.Context, scope Scope, sessionID string) (*Session, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	output, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName), ConsistentRead: aws.Bool(true), Key: sessionKey(sessionID),
	})
	if err != nil {
		return nil, fmt.Errorf("get voice session: %w", err)
	}
	if len(output.Item) == 0 {
		return nil, ErrNotFound
	}
	value, err := unmarshalSession(output.Item)
	if err != nil {
		return nil, fmt.Errorf("decode voice session: %w", err)
	}
	if value.UserID != scope.UserID || value.BusinessID != scope.BusinessID {
		return nil, ErrNotFound
	}
	if !scopeAllowsBranch(scope, value.BranchID) {
		return nil, ErrNotFound
	}
	return value, nil
}

func (s *DynamoDBStore) Resume(ctx context.Context, input ResumeRecord) (*Session, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if !scopeAllowsBranch(input.Scope, input.ExpectedBranchID) {
		return nil, ErrNotFound
	}
	names := map[string]string{
		"#status": "status", "#user_id": "user_id", "#business_id": "business_id",
		"#branch_id":          "branch_id",
		"#runtime_session_id": "runtime_session_id", "#runtime_state": "runtime_state",
		"#expires_at": "expires_at", "#lease_expires_at": "lease_expires_at",
		"#updated_at": "updated_at", "#gsi2pk": "GSI2PK", "#gsi2sk": "GSI2SK",
	}
	values := map[string]types.AttributeValue{
		":active": stringAttribute(string(StatusActive)), ":user_id": stringAttribute(input.Scope.UserID),
		":business_id": stringAttribute(input.Scope.BusinessID), ":old_runtime_session_id": stringAttribute(input.OldRuntimeSessionID),
		":expected_runtime_state":    stringAttribute(string(input.ExpectedRuntimeState)),
		":expected_lease_expires_at": stringAttribute(formatTime(input.ExpectedLeaseExpiresAt)),
		":now":                       stringAttribute(formatTime(input.UpdatedAt)), ":now_epoch": numberAttribute(input.UpdatedAt.Unix()),
		":lease_expires_at": stringAttribute(formatTime(input.LeaseExpiresAt)),
		":updated_at":       stringAttribute(formatTime(input.UpdatedAt)), ":lease_pk": stringAttribute(leasePartitionKey),
		":lease_sk": stringAttribute(leaseSortKey(input.LeaseExpiresAt, input.SessionID)),
	}
	conditionExpression := "#status = :active AND #user_id = :user_id AND #business_id = :business_id AND #runtime_session_id = :old_runtime_session_id AND #runtime_state = :expected_runtime_state AND #expires_at > :now_epoch AND #lease_expires_at = :expected_lease_expires_at AND #lease_expires_at > :now"
	conditionExpression = addExpectedBranchCondition(conditionExpression, input.ExpectedBranchID, values)
	updateExpression := "SET #lease_expires_at = :lease_expires_at, #updated_at = :updated_at, #gsi2pk = :lease_pk, #gsi2sk = :lease_sk"
	if input.NewRuntimeSessionID != "" {
		updateExpression += ", #runtime_session_id = :new_runtime_session_id, #runtime_state = :running"
		values[":new_runtime_session_id"] = stringAttribute(input.NewRuntimeSessionID)
		values[":running"] = stringAttribute(string(RuntimeStateRunning))
	}
	output, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName), Key: sessionKey(input.SessionID),
		ConditionExpression: aws.String(conditionExpression),
		UpdateExpression:    aws.String(updateExpression), ExpressionAttributeNames: names, ExpressionAttributeValues: values,
		ReturnValues: types.ReturnValueAllNew,
	})
	if err != nil {
		var conditional *types.ConditionalCheckFailedException
		if errors.As(err, &conditional) {
			if _, getErr := s.Get(ctx, input.Scope, input.SessionID); errors.Is(getErr, ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, ErrNotResumable
		}
		return nil, fmt.Errorf("resume voice session: %w", err)
	}
	if len(output.Attributes) == 0 {
		return s.Get(ctx, input.Scope, input.SessionID)
	}
	resumed, err := unmarshalSession(output.Attributes)
	if err != nil {
		return nil, fmt.Errorf("decode resumed voice session: %w", err)
	}
	if resumed.UserID != input.Scope.UserID || resumed.BusinessID != input.Scope.BusinessID || !scopeAllowsBranch(input.Scope, resumed.BranchID) {
		return nil, ErrNotFound
	}
	return resumed, nil
}

// RenewLease advances one exact active runtime lease. The old lease timestamp
// is the optimistic-lock version shared with ReleaseExpired: DynamoDB can
// commit the renewal or the reconciler release, never both.
func (s *DynamoDBStore) RenewLease(ctx context.Context, input LeaseRenewal) (*Session, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if ctx == nil || validateScope(input.Scope) != nil || !validSessionID(input.SessionID) ||
		!validRuntimeLeaseID(input.RuntimeSessionID) || !scopeAllowsBranch(input.Scope, input.ExpectedBranchID) ||
		input.ExpectedLeaseExpiresAt.IsZero() || input.ExpectedExpiresAt.IsZero() || input.RenewedAt.IsZero() ||
		input.LeaseDuration <= 0 || input.LeaseDuration > 5*time.Minute ||
		!input.ExpectedLeaseExpiresAt.After(input.RenewedAt) || input.ExpectedExpiresAt.Unix() <= input.RenewedAt.Unix() {
		return nil, ErrInvalidRequest
	}

	productExpiresAt := time.Unix(input.ExpectedExpiresAt.Unix(), 0).UTC()
	if input.ExpectedLeaseExpiresAt.After(productExpiresAt) {
		return nil, ErrInvalidRequest
	}
	leaseExpiresAt := input.RenewedAt.Add(input.LeaseDuration)
	if leaseExpiresAt.After(productExpiresAt) {
		leaseExpiresAt = productExpiresAt
	}
	if !leaseExpiresAt.After(input.ExpectedLeaseExpiresAt) {
		return nil, ErrLeaseNotRenewable
	}

	names := map[string]string{
		"#session_id": "session_id", "#user_id": "user_id", "#business_id": "business_id", "#branch_id": "branch_id",
		"#runtime_session_id": "runtime_session_id", "#status": "status", "#runtime_state": "runtime_state",
		"#capacity_released": "capacity_released", "#lease_expires_at": "lease_expires_at", "#expires_at": "expires_at",
		"#updated_at": "updated_at", "#gsi2pk": "GSI2PK", "#gsi2sk": "GSI2SK",
	}
	values := map[string]types.AttributeValue{
		":session_id": stringAttribute(input.SessionID), ":user_id": stringAttribute(input.Scope.UserID),
		":business_id": stringAttribute(input.Scope.BusinessID), ":runtime_session_id": stringAttribute(input.RuntimeSessionID),
		":active": stringAttribute(string(StatusActive)), ":running": stringAttribute(string(RuntimeStateRunning)),
		":false": boolAttribute(false), ":expected_lease_expires_at": stringAttribute(formatTime(input.ExpectedLeaseExpiresAt)),
		":renewed_at": stringAttribute(formatTime(input.RenewedAt)), ":expected_expires_at": numberAttribute(input.ExpectedExpiresAt.Unix()),
		":renewed_at_epoch": numberAttribute(input.RenewedAt.Unix()), ":lease_expires_at": stringAttribute(formatTime(leaseExpiresAt)),
		":lease_expires_at_epoch": numberAttribute(leaseExpiresAt.Unix()),
		":updated_at":             stringAttribute(formatTime(input.RenewedAt)), ":lease_pk": stringAttribute(leasePartitionKey),
		":lease_sk": stringAttribute(leaseSortKey(leaseExpiresAt, input.SessionID)),
	}
	condition := "#session_id = :session_id AND #user_id = :user_id AND #business_id = :business_id AND " +
		"#runtime_session_id = :runtime_session_id AND #status = :active AND #runtime_state = :running AND " +
		"#capacity_released = :false AND #lease_expires_at = :expected_lease_expires_at AND " +
		"#lease_expires_at > :renewed_at AND #expires_at = :expected_expires_at AND #expires_at > :renewed_at_epoch AND " +
		"#expires_at >= :lease_expires_at_epoch"
	condition = addExpectedBranchCondition(condition, input.ExpectedBranchID, values)
	output, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName), Key: sessionKey(input.SessionID),
		ConditionExpression:      aws.String(condition),
		UpdateExpression:         aws.String("SET #lease_expires_at = :lease_expires_at, #updated_at = :updated_at, #gsi2pk = :lease_pk, #gsi2sk = :lease_sk"),
		ExpressionAttributeNames: names, ExpressionAttributeValues: values, ReturnValues: types.ReturnValueAllNew,
	})
	if err != nil {
		var conditional *types.ConditionalCheckFailedException
		if errors.As(err, &conditional) {
			return nil, ErrLeaseNotRenewable
		}
		return nil, fmt.Errorf("renew voice session lease: %w", ErrUnavailable)
	}
	if output == nil || len(output.Attributes) == 0 {
		return nil, fmt.Errorf("renew voice session lease: %w", ErrUnavailable)
	}
	renewed, err := unmarshalSession(output.Attributes)
	if err != nil || !validRenewedLease(renewed, input, leaseExpiresAt) {
		return nil, fmt.Errorf("renew voice session lease: %w", ErrUnavailable)
	}
	return renewed, nil
}

func (s *DynamoDBStore) Release(ctx context.Context, scope Scope, sessionID string, now time.Time) (*Session, ReleaseState, error) {
	stored, err := s.Get(ctx, scope, sessionID)
	if err != nil {
		return nil, ReleaseState{}, err
	}
	switch stored.Status {
	case StatusClosed:
		return stored, ReleaseState{AlreadyClosed: true}, nil
	case StatusClosing:
		return stored, ReleaseState{ShouldStop: true}, nil
	case StatusActive:
		// Continue into the guarded release transaction.
	default:
		return nil, ReleaseState{}, ErrNotResumable
	}

	transaction := s.releaseTransaction(scope, stored, now, "close", nil, nil)
	if _, err := s.db.TransactWriteItems(ctx, transaction); err != nil {
		current, getErr := s.Get(ctx, scope, sessionID)
		if getErr != nil {
			return nil, ReleaseState{}, getErr
		}
		if current.Status == StatusClosed {
			return current, ReleaseState{AlreadyClosed: true}, nil
		}
		if current.Status == StatusClosing && current.CapacityReleased {
			return current, ReleaseState{ShouldStop: true}, nil
		}
		return nil, ReleaseState{}, fmt.Errorf("release voice session capacity: %w", err)
	}
	released := cloneSession(stored)
	released.Status = StatusClosing
	released.CapacityReleased = true
	released.UpdatedAt = now
	return released, ReleaseState{Released: true, ShouldStop: true}, nil
}

// ReleaseExpired transitions only the exact expired lease observed through the
// lease index. A concurrent renewal changes lease_expires_at and wins the race.
func (s *DynamoDBStore) ReleaseExpired(ctx context.Context, candidate *Session, cutoff time.Time) (*Session, ReleaseState, error) {
	if err := s.validate(); err != nil {
		return nil, ReleaseState{}, err
	}
	if candidate == nil || candidate.UserID == "" || candidate.BusinessID == "" || !validSessionID(candidate.ID) ||
		candidate.LeaseExpiresAt.IsZero() || candidate.LeaseExpiresAt.After(cutoff) {
		return nil, ReleaseState{}, ErrInvalidRequest
	}
	scope := sessionScope(candidate)
	current, err := s.Get(ctx, scope, candidate.ID)
	if err != nil {
		return nil, ReleaseState{}, err
	}
	if result, state, handled := classifyExpiredRelease(current, candidate.LeaseExpiresAt, cutoff); handled {
		return result, state, nil
	}

	transaction := s.releaseTransaction(scope, current, cutoff, "reconcile", &candidate.LeaseExpiresAt, &cutoff)
	if _, err := s.db.TransactWriteItems(ctx, transaction); err != nil {
		latest, getErr := s.Get(ctx, scope, candidate.ID)
		if getErr != nil {
			return nil, ReleaseState{}, getErr
		}
		if result, state, handled := classifyExpiredRelease(latest, candidate.LeaseExpiresAt, cutoff); handled {
			return result, state, nil
		}
		return nil, ReleaseState{}, fmt.Errorf("release expired voice session capacity: %w", err)
	}
	released := cloneSession(current)
	released.Status = StatusClosing
	released.CapacityReleased = true
	released.UpdatedAt = cutoff.UTC()
	return released, ReleaseState{Released: true, ShouldStop: true}, nil
}

func (s *DynamoDBStore) releaseTransaction(scope Scope, stored *Session, now time.Time, operation string, expectedLease, cutoff *time.Time) *dynamodb.TransactWriteItemsInput {
	now = now.UTC()
	nowAttribute := stringAttribute(formatTime(now))
	conditionExpression := "#status = :active AND #user_id = :user_id AND #business_id = :business_id AND #capacity_released = :false"
	releaseValues := map[string]types.AttributeValue{
		":active": stringAttribute(string(StatusActive)), ":closing": stringAttribute(string(StatusClosing)),
		":user_id": stringAttribute(scope.UserID), ":business_id": stringAttribute(scope.BusinessID),
		":false": boolAttribute(false), ":true": boolAttribute(true), ":updated_at": nowAttribute,
		":lease_pk": stringAttribute(leasePartitionKey), ":stop_pending_sk": stringAttribute(leaseSortKey(now, stored.ID)),
	}
	names := map[string]string{
		"#status": "status", "#user_id": "user_id", "#business_id": "business_id", "#capacity_released": "capacity_released",
		"#branch_id": "branch_id", "#updated_at": "updated_at", "#gsi1pk": "GSI1PK", "#gsi1sk": "GSI1SK", "#gsi2pk": "GSI2PK", "#gsi2sk": "GSI2SK",
	}
	conditionExpression = addExpectedBranchCondition(conditionExpression, stored.BranchID, releaseValues)
	// A DynamoDB client token may be reused only for byte-for-byte equivalent
	// transactions. Include the attempt timestamp because updated_at and the
	// closing lease-index key change between later close/reconcile attempts.
	tokenScope := stored.ID + "|" + formatTime(now)
	if expectedLease != nil && cutoff != nil {
		names["#lease_expires_at"] = "lease_expires_at"
		releaseValues[":expected_lease_expires_at"] = stringAttribute(formatTime(*expectedLease))
		releaseValues[":cutoff"] = stringAttribute(formatTime(*cutoff))
		conditionExpression += " AND #lease_expires_at = :expected_lease_expires_at AND #lease_expires_at <= :cutoff"
		tokenScope += "|" + formatTime(*expectedLease)
	}
	return &dynamodb.TransactWriteItemsInput{
		ClientRequestToken: aws.String(transactionToken(operation, tokenScope)),
		TransactItems: []types.TransactWriteItem{
			{Update: &types.Update{
				TableName: aws.String(s.tableName), Key: sessionKey(stored.ID),
				ConditionExpression:      aws.String(conditionExpression),
				UpdateExpression:         aws.String("SET #status = :closing, #capacity_released = :true, #updated_at = :updated_at, #gsi2pk = :lease_pk, #gsi2sk = :stop_pending_sk REMOVE #gsi1pk, #gsi1sk"),
				ExpressionAttributeNames: names, ExpressionAttributeValues: releaseValues,
			}},
			{Update: capacityDecrement(s.tableName, globalCapacityPK, globalCapacitySK, nowAttribute)},
			{Update: capacityDecrement(s.tableName, userCapacityPK(scope.UserID), userCapacitySK, nowAttribute)},
		},
	}
}

func classifyExpiredRelease(current *Session, expectedLease, cutoff time.Time) (*Session, ReleaseState, bool) {
	if current == nil {
		return nil, ReleaseState{}, false
	}
	switch current.Status {
	case StatusClosed:
		return current, ReleaseState{AlreadyClosed: true}, true
	case StatusClosing:
		if current.CapacityReleased {
			return current, ReleaseState{ShouldStop: true}, true
		}
		return nil, ReleaseState{}, false
	case StatusActive:
		if !current.LeaseExpiresAt.Equal(expectedLease) || current.LeaseExpiresAt.After(cutoff) {
			return current, ReleaseState{LeaseRenewed: true}, true
		}
	}
	return nil, ReleaseState{}, false
}

func sessionScope(value *Session) Scope {
	scope := Scope{UserID: value.UserID, BusinessID: value.BusinessID}
	if value.BranchID == "" {
		scope.AllBranches = true
	} else {
		scope.AllowedBranchIDs = []string{value.BranchID}
	}
	return scope
}

func (s *DynamoDBStore) MarkClosed(ctx context.Context, scope Scope, sessionID, expectedBranchID string, now time.Time) error {
	if !scopeAllowsBranch(scope, expectedBranchID) {
		return ErrNotFound
	}
	values := map[string]types.AttributeValue{
		":closing": stringAttribute(string(StatusClosing)), ":closed": stringAttribute(string(StatusClosed)),
		":stopped": stringAttribute(string(RuntimeStateStopped)), ":closed_at": stringAttribute(formatTime(now)),
		":updated_at": stringAttribute(formatTime(now)), ":user_id": stringAttribute(scope.UserID),
		":business_id": stringAttribute(scope.BusinessID), ":true": boolAttribute(true),
	}
	conditionExpression := "#status = :closing AND #user_id = :user_id AND #business_id = :business_id AND #capacity_released = :true"
	conditionExpression = addExpectedBranchCondition(conditionExpression, expectedBranchID, values)
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName), Key: sessionKey(sessionID),
		ConditionExpression: aws.String(conditionExpression),
		UpdateExpression:    aws.String("SET #status = :closed, #runtime_state = :stopped, #closed_at = :closed_at, #updated_at = :updated_at REMOVE #gsi2pk, #gsi2sk"),
		ExpressionAttributeNames: map[string]string{
			"#status": "status", "#runtime_state": "runtime_state", "#closed_at": "closed_at", "#updated_at": "updated_at",
			"#user_id": "user_id", "#business_id": "business_id", "#branch_id": "branch_id", "#capacity_released": "capacity_released",
			"#gsi2pk": "GSI2PK", "#gsi2sk": "GSI2SK",
		},
		ExpressionAttributeValues: values,
	})
	if err == nil {
		return nil
	}
	var conditional *types.ConditionalCheckFailedException
	if errors.As(err, &conditional) {
		current, getErr := s.Get(ctx, scope, sessionID)
		if getErr == nil && current.Status == StatusClosed {
			return nil
		}
		if getErr != nil {
			return getErr
		}
	}
	return fmt.Errorf("mark voice session closed: %w", err)
}

func (s *DynamoDBStore) ExpiredLeases(ctx context.Context, before time.Time, limit int32) ([]*Session, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	values := make([]*Session, 0, limit)
	var cursor map[string]types.AttributeValue
	for page := int32(0); int32(len(values)) < limit && page < limit; page++ {
		remaining := limit - int32(len(values))
		output, err := s.db.Query(ctx, &dynamodb.QueryInput{
			TableName: aws.String(s.tableName), IndexName: aws.String(s.leaseIndexName), Limit: aws.Int32(remaining),
			ConsistentRead: aws.Bool(false), ScanIndexForward: aws.Bool(true), ExclusiveStartKey: cursor,
			KeyConditionExpression:   aws.String("#gsi2pk = :lease AND #gsi2sk <= :cutoff"),
			ExpressionAttributeNames: map[string]string{"#gsi2pk": "GSI2PK", "#gsi2sk": "GSI2SK"},
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":lease": stringAttribute(leasePartitionKey), ":cutoff": stringAttribute(leaseSortKey(before, "\uffff")),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("query expired voice leases: %w", err)
		}
		for _, item := range output.Items {
			if int32(len(values)) == limit {
				break
			}
			value, decodeErr := unmarshalSession(item)
			if decodeErr != nil {
				return nil, fmt.Errorf("decode expired voice lease: %w", decodeErr)
			}
			values = append(values, value)
		}
		if len(output.LastEvaluatedKey) == 0 {
			return values, nil
		}
		cursor = output.LastEvaluatedKey
	}
	if int32(len(values)) < limit && len(cursor) != 0 {
		return nil, fmt.Errorf("query expired voice leases exceeded pagination bound: %w", ErrUnavailable)
	}
	return values, nil
}

func (s *DynamoDBStore) validate() error {
	if s == nil || nilDynamoDBAPI(s.db) || s.tableName == "" {
		return ErrUnavailable
	}
	return nil
}

func nilDynamoDBAPI(value DynamoDBAPI) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func validRuntimeLeaseID(value string) bool {
	if value == "" || len(value) > 256 || strings.TrimSpace(value) != value || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func validRenewedLease(value *Session, input LeaseRenewal, expectedLease time.Time) bool {
	return value != nil && value.ID == input.SessionID && value.RuntimeSessionID == input.RuntimeSessionID &&
		value.UserID == input.Scope.UserID && value.BusinessID == input.Scope.BusinessID && value.BranchID == input.ExpectedBranchID &&
		value.Status == StatusActive && value.RuntimeState == RuntimeStateRunning && !value.CapacityReleased &&
		value.LeaseExpiresAt.Equal(expectedLease) && value.ExpiresAt.Unix() == input.ExpectedExpiresAt.Unix() &&
		value.UpdatedAt.Equal(input.RenewedAt) && !value.LeaseExpiresAt.After(value.ExpiresAt)
}

func (s *DynamoDBStore) lookupIdempotency(ctx context.Context, record CreateRecord) (*Session, bool, error) {
	output, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName), ConsistentRead: aws.Bool(true),
		Key: map[string]types.AttributeValue{"pk": stringAttribute(idempotencyPK(record.Session.UserID, record.IdempotencyKey)), "sk": stringAttribute(idempotencySortKey)},
	})
	if err != nil {
		return nil, false, fmt.Errorf("get voice idempotency record: %w", err)
	}
	if len(output.Item) == 0 {
		return nil, false, nil
	}
	hash := attributeString(output.Item, "request_hash")
	businessID := attributeString(output.Item, "business_id")
	if hash != record.RequestHash || businessID != record.Session.BusinessID {
		return nil, false, ErrIdempotencyConflict
	}
	sessionID := attributeString(output.Item, "session_id")
	if sessionID == "" {
		return nil, false, fmt.Errorf("voice idempotency record has no session id: %w", ErrUnavailable)
	}
	lookupScope := Scope{UserID: record.Session.UserID, BusinessID: record.Session.BusinessID}
	if record.Session.BranchID == "" {
		lookupScope.AllBranches = true
	} else {
		lookupScope.AllowedBranchIDs = []string{record.Session.BranchID}
	}
	value, err := s.Get(ctx, lookupScope, sessionID)
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

type sessionDynamoItem struct {
	PK                       string `dynamodbav:"pk"`
	SK                       string `dynamodbav:"sk"`
	SessionID                string `dynamodbav:"session_id"`
	RuntimeSessionID         string `dynamodbav:"runtime_session_id"`
	UserID                   string `dynamodbav:"user_id"`
	BusinessID               string `dynamodbav:"business_id"`
	BranchID                 string `dynamodbav:"branch_id,omitempty"`
	Status                   string `dynamodbav:"status"`
	RuntimeState             string `dynamodbav:"runtime_state"`
	ProtocolVersion          int    `dynamodbav:"protocol_version"`
	KVSChannelIndex          int    `dynamodbav:"kvs_channel_index"`
	PreferredLanguage        string `dynamodbav:"preferred_language"`
	FallbackLanguage         string `dynamodbav:"fallback_language"`
	CurrentLanguage          string `dynamodbav:"current_language"`
	TurnSequence             int64  `dynamodbav:"turn_sequence"`
	GenerationID             int64  `dynamodbav:"generation_id"`
	ConsentTranscriptStorage bool   `dynamodbav:"consent_transcript_storage"`
	ConsentAudioRecording    bool   `dynamodbav:"consent_audio_recording"`
	ConsentPolicyVersion     string `dynamodbav:"consent_policy_version"`
	ClientPlatform           string `dynamodbav:"client_platform"`
	ClientAppVersion         string `dynamodbav:"client_app_version"`
	CreatedAt                string `dynamodbav:"created_at"`
	UpdatedAt                string `dynamodbav:"updated_at"`
	LeaseExpiresAt           string `dynamodbav:"lease_expires_at"`
	ExpiresAt                int64  `dynamodbav:"expires_at"`
	RotateAt                 string `dynamodbav:"rotate_at"`
	ClosedAt                 string `dynamodbav:"closed_at,omitempty"`
	CapacityReleased         bool   `dynamodbav:"capacity_released"`
	TTL                      int64  `dynamodbav:"ttl"`
	GSI1PK                   string `dynamodbav:"GSI1PK,omitempty"`
	GSI1SK                   string `dynamodbav:"GSI1SK,omitempty"`
	GSI2PK                   string `dynamodbav:"GSI2PK,omitempty"`
	GSI2SK                   string `dynamodbav:"GSI2SK,omitempty"`
}

type finalTurnDynamoItem struct {
	PK                 string                 `dynamodbav:"pk"`
	SK                 string                 `dynamodbav:"sk"`
	ItemType           string                 `dynamodbav:"item_type"`
	SessionID          string                 `dynamodbav:"session_id"`
	Sequence           int64                  `dynamodbav:"sequence"`
	GenerationID       int64                  `dynamodbav:"generation_id"`
	Transcript         string                 `dynamodbav:"transcript"`
	DetectedLanguage   string                 `dynamodbav:"detected_language"`
	SelectedLanguage   string                 `dynamodbav:"selected_language"`
	Response           string                 `dynamodbav:"response"`
	Tools              []FinalTurnToolOutcome `dynamodbav:"tools"`
	SpeechEndedAt      string                 `dynamodbav:"speech_ended_at"`
	STTFinalAt         string                 `dynamodbav:"stt_final_at"`
	LLMFirstTokenAt    string                 `dynamodbav:"llm_first_token_at,omitempty"`
	TTSFirstAudioAt    string                 `dynamodbav:"tts_first_audio_at,omitempty"`
	ClientFirstAudioAt string                 `dynamodbav:"client_first_audio_at,omitempty"`
	Cancelled          bool                   `dynamodbav:"cancelled"`
	InputTokens        *int64                 `dynamodbav:"input_tokens,omitempty"`
	OutputTokens       *int64                 `dynamodbav:"output_tokens,omitempty"`
	TotalTokens        *int64                 `dynamodbav:"total_tokens,omitempty"`
	TTSCharacters      *int64                 `dynamodbav:"tts_characters,omitempty"`
	CompletedAt        string                 `dynamodbav:"completed_at"`
	ExpiresAt          int64                  `dynamodbav:"expires_at"`
	ContentHash        string                 `dynamodbav:"content_hash"`
}

func validateFinalTurn(turn FinalTurn) error {
	if !validSessionID(turn.SessionID) || turn.Sequence <= 0 || turn.GenerationID <= 0 ||
		turn.CompletedAt.IsZero() || !turn.ExpiresAt.After(turn.CompletedAt) || turn.ExpiresAt.Sub(turn.CompletedAt) > MaxFinalTurnRetention ||
		strings.TrimSpace(turn.Transcript) == "" ||
		!utf8.ValidString(turn.Transcript) || !utf8.ValidString(turn.Response) ||
		len(turn.Transcript) > MaxPersistedTranscriptBytes || len(turn.Response) > MaxPersistedResponseBytes {
		return ErrInvalidFinalTurn
	}
	if (!turn.Cancelled && strings.TrimSpace(turn.Response) == "") || (turn.Cancelled && turn.Response != "") {
		return ErrInvalidFinalTurn
	}
	if !safeFinalTurnProviderLanguage(turn.ProviderLanguage) || !supportedLanguage(turn.SelectedLanguage) {
		return ErrInvalidFinalTurn
	}
	if len(turn.Tools) > MaxPersistedToolOutcomes {
		return ErrInvalidFinalTurn
	}
	for _, tool := range turn.Tools {
		if !finalTurnMetadataPattern.MatchString(tool.Name) ||
			(tool.Success && tool.ErrorCode != "") ||
			(!tool.Success && !finalTurnMetadataPattern.MatchString(tool.ErrorCode)) {
			return ErrInvalidFinalTurn
		}
	}
	if !validFinalTurnTimings(turn.Timings, turn.CompletedAt, turn.Cancelled) || !validFinalTurnUsage(turn.Usage) {
		return ErrInvalidFinalTurn
	}
	return nil
}

func safeFinalTurnProviderLanguage(value string) bool {
	if len(value) > 16 {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character > 0x7e {
			return false
		}
	}
	return true
}

func validFinalTurnTimings(timings FinalTurnTimings, completedAt time.Time, cancelled bool) bool {
	if timings.SpeechEndedAt.IsZero() || timings.STTFinalAt.IsZero() || timings.STTFinalAt.Before(timings.SpeechEndedAt) {
		return false
	}
	last := timings.STTFinalAt
	missing := false
	for _, boundary := range []time.Time{timings.LLMFirstTokenAt, timings.TTSFirstAudioAt, timings.ClientFirstAudioAt} {
		if boundary.IsZero() {
			missing = true
			continue
		}
		if missing || boundary.Before(last) {
			return false
		}
		last = boundary
	}
	if (!cancelled && missing) || completedAt.Before(last) {
		return false
	}
	return true
}

func validFinalTurnUsage(usage FinalTurnUsage) bool {
	for _, value := range []*int64{usage.InputTokens, usage.OutputTokens, usage.TotalTokens, usage.TTSCharacters} {
		if value != nil && (*value < 0 || *value > 1_000_000) {
			return false
		}
	}
	if usage.InputTokens != nil && usage.OutputTokens != nil && usage.TotalTokens != nil &&
		*usage.TotalTokens != *usage.InputTokens+*usage.OutputTokens {
		return false
	}
	return true
}

func marshalFinalTurn(turn FinalTurn) (map[string]types.AttributeValue, error) {
	canonical := struct {
		SessionID          string                 `json:"session_id"`
		Sequence           int64                  `json:"sequence"`
		GenerationID       int64                  `json:"generation_id"`
		Transcript         string                 `json:"transcript"`
		DetectedLanguage   string                 `json:"detected_language"`
		SelectedLanguage   string                 `json:"selected_language"`
		Response           string                 `json:"response"`
		Tools              []FinalTurnToolOutcome `json:"tools"`
		SpeechEndedAt      string                 `json:"speech_ended_at"`
		STTFinalAt         string                 `json:"stt_final_at"`
		LLMFirstTokenAt    string                 `json:"llm_first_token_at"`
		TTSFirstAudioAt    string                 `json:"tts_first_audio_at"`
		ClientFirstAudioAt string                 `json:"client_first_audio_at"`
		Cancelled          bool                   `json:"cancelled"`
		InputTokens        *int64                 `json:"input_tokens,omitempty"`
		OutputTokens       *int64                 `json:"output_tokens,omitempty"`
		TotalTokens        *int64                 `json:"total_tokens,omitempty"`
		TTSCharacters      *int64                 `json:"tts_characters,omitempty"`
		CompletedAt        string                 `json:"completed_at"`
		ExpiresAt          int64                  `json:"expires_at"`
	}{
		SessionID: turn.SessionID, Sequence: turn.Sequence, GenerationID: turn.GenerationID,
		Transcript: turn.Transcript, DetectedLanguage: turn.ProviderLanguage, SelectedLanguage: turn.SelectedLanguage,
		Response: turn.Response, Tools: turn.Tools,
		SpeechEndedAt: formatTime(turn.Timings.SpeechEndedAt), STTFinalAt: formatTime(turn.Timings.STTFinalAt),
		LLMFirstTokenAt: optionalTime(turn.Timings.LLMFirstTokenAt), TTSFirstAudioAt: optionalTime(turn.Timings.TTSFirstAudioAt),
		ClientFirstAudioAt: optionalTime(turn.Timings.ClientFirstAudioAt), Cancelled: turn.Cancelled,
		InputTokens: turn.Usage.InputTokens, OutputTokens: turn.Usage.OutputTokens,
		TotalTokens: turn.Usage.TotalTokens, TTSCharacters: turn.Usage.TTSCharacters,
		CompletedAt: formatTime(turn.CompletedAt), ExpiresAt: turn.ExpiresAt.Unix(),
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(encoded))
	return attributevalue.MarshalMap(finalTurnDynamoItem{
		PK: sessionPK(turn.SessionID), SK: turnSortKey(turn.Sequence), ItemType: "FINAL_TURN",
		SessionID: turn.SessionID, Sequence: turn.Sequence, GenerationID: turn.GenerationID,
		Transcript: turn.Transcript, DetectedLanguage: turn.ProviderLanguage, SelectedLanguage: turn.SelectedLanguage,
		Response: turn.Response, Tools: append([]FinalTurnToolOutcome(nil), turn.Tools...),
		SpeechEndedAt: canonical.SpeechEndedAt, STTFinalAt: canonical.STTFinalAt,
		LLMFirstTokenAt: canonical.LLMFirstTokenAt, TTSFirstAudioAt: canonical.TTSFirstAudioAt,
		ClientFirstAudioAt: canonical.ClientFirstAudioAt, Cancelled: turn.Cancelled,
		InputTokens: cloneInt64(turn.Usage.InputTokens), OutputTokens: cloneInt64(turn.Usage.OutputTokens),
		TotalTokens: cloneInt64(turn.Usage.TotalTokens), TTSCharacters: cloneInt64(turn.Usage.TTSCharacters),
		CompletedAt: canonical.CompletedAt,
		ExpiresAt:   canonical.ExpiresAt, ContentHash: hash,
	})
}

func optionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return formatTime(value)
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func (s *DynamoDBStore) lookupFinalTurnHash(ctx context.Context, sessionID string, sequence int64) (string, bool, error) {
	output, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName), ConsistentRead: aws.Bool(true), Key: turnKey(sessionID, sequence),
	})
	if err != nil {
		return "", false, fmt.Errorf("read retained final voice turn: %w", err)
	}
	if len(output.Item) == 0 {
		return "", false, nil
	}
	hash := attributeString(output.Item, "content_hash")
	if len(hash) != sha256.Size*2 {
		return "", false, fmt.Errorf("read retained final voice turn: %w", ErrFinalTurnConflict)
	}
	return hash, true, nil
}

func marshalSession(value *Session) (map[string]types.AttributeValue, error) {
	closedAt := ""
	if value.ClosedAt != nil {
		closedAt = formatTime(*value.ClosedAt)
	}
	item := sessionDynamoItem{
		PK: sessionPK(value.ID), SK: sessionSortKey, SessionID: value.ID, RuntimeSessionID: value.RuntimeSessionID,
		UserID: value.UserID, BusinessID: value.BusinessID, BranchID: value.BranchID,
		Status: string(value.Status), RuntimeState: string(value.RuntimeState), ProtocolVersion: value.ProtocolVersion,
		KVSChannelIndex: value.KVSChannelIndex, PreferredLanguage: value.PreferredLanguage, FallbackLanguage: value.FallbackLanguage,
		CurrentLanguage: value.CurrentLanguage, TurnSequence: value.TurnSequence, GenerationID: value.GenerationID,
		ConsentTranscriptStorage: value.ConsentTranscriptStorage, ConsentAudioRecording: value.ConsentAudioRecording,
		ConsentPolicyVersion: value.ConsentPolicyVersion, ClientPlatform: value.ClientPlatform, ClientAppVersion: value.ClientAppVersion,
		CreatedAt: formatTime(value.CreatedAt), UpdatedAt: formatTime(value.UpdatedAt), LeaseExpiresAt: formatTime(value.LeaseExpiresAt),
		ExpiresAt: value.ExpiresAt.Unix(), RotateAt: formatTime(value.RotateAt), ClosedAt: closedAt,
		CapacityReleased: value.CapacityReleased, TTL: value.ExpiresAt.Unix(),
	}
	if value.Status == StatusActive {
		item.GSI1PK = "VOICE#USER#" + value.UserID
		item.GSI1SK = "ACTIVE#" + formatTime(value.CreatedAt) + "#" + value.ID
		item.GSI2PK = leasePartitionKey
		item.GSI2SK = leaseSortKey(value.LeaseExpiresAt, value.ID)
	}
	return attributevalue.MarshalMap(item)
}

func unmarshalSession(item map[string]types.AttributeValue) (*Session, error) {
	var stored sessionDynamoItem
	if err := attributevalue.UnmarshalMap(item, &stored); err != nil {
		return nil, err
	}
	createdAt, err := parseTime(stored.CreatedAt)
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseTime(stored.UpdatedAt)
	if err != nil {
		return nil, err
	}
	leaseExpiresAt, err := parseTime(stored.LeaseExpiresAt)
	if err != nil {
		return nil, err
	}
	rotateAt, err := parseTime(stored.RotateAt)
	if err != nil {
		return nil, err
	}
	var closedAt *time.Time
	if stored.ClosedAt != "" {
		parsed, parseErr := parseTime(stored.ClosedAt)
		if parseErr != nil {
			return nil, parseErr
		}
		closedAt = &parsed
	}
	runtimeState := RuntimeState(stored.RuntimeState)
	if runtimeState == "" {
		runtimeState = RuntimeStateRunning
	}
	return &Session{
		ID: stored.SessionID, RuntimeSessionID: stored.RuntimeSessionID, UserID: stored.UserID, BusinessID: stored.BusinessID,
		BranchID: stored.BranchID, Status: Status(stored.Status), RuntimeState: runtimeState,
		ProtocolVersion: stored.ProtocolVersion, KVSChannelIndex: stored.KVSChannelIndex,
		PreferredLanguage: stored.PreferredLanguage, FallbackLanguage: stored.FallbackLanguage, CurrentLanguage: stored.CurrentLanguage,
		TurnSequence: stored.TurnSequence, GenerationID: stored.GenerationID,
		ConsentTranscriptStorage: stored.ConsentTranscriptStorage, ConsentAudioRecording: stored.ConsentAudioRecording,
		ConsentPolicyVersion: stored.ConsentPolicyVersion, ClientPlatform: stored.ClientPlatform, ClientAppVersion: stored.ClientAppVersion,
		CreatedAt: createdAt, UpdatedAt: updatedAt, LeaseExpiresAt: leaseExpiresAt,
		ExpiresAt: time.Unix(stored.ExpiresAt, 0).UTC(), RotateAt: rotateAt, ClosedAt: closedAt,
		CapacityReleased: stored.CapacityReleased,
	}, nil
}

func idempotencyItem(record CreateRecord) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"pk": stringAttribute(idempotencyPK(record.Session.UserID, record.IdempotencyKey)),
		"sk": stringAttribute(idempotencySortKey), "user_id": stringAttribute(record.Session.UserID),
		"business_id": stringAttribute(record.Session.BusinessID), "idempotency_key": stringAttribute(record.IdempotencyKey),
		"request_hash": stringAttribute(record.RequestHash), "session_id": stringAttribute(record.Session.ID),
		"created_at": stringAttribute(formatTime(record.Session.CreatedAt)), "expires_at": numberAttribute(record.IdempotencyExpiresAt.Unix()),
	}
}

func capacityIncrement(tableName, pk, sk string, limit int64, now types.AttributeValue) *types.Update {
	return &types.Update{
		TableName: aws.String(tableName), Key: map[string]types.AttributeValue{"pk": stringAttribute(pk), "sk": stringAttribute(sk)},
		ConditionExpression:      aws.String("attribute_not_exists(#active_count) OR #active_count < :capacity_limit"),
		UpdateExpression:         aws.String("SET #active_count = if_not_exists(#active_count, :zero) + :one, #capacity_limit = :capacity_limit, #updated_at = :updated_at"),
		ExpressionAttributeNames: map[string]string{"#active_count": "active_count", "#capacity_limit": "limit", "#updated_at": "updated_at"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":zero": numberAttribute(0), ":one": numberAttribute(1), ":capacity_limit": numberAttribute(limit), ":updated_at": now,
		},
	}
}

func capacityDecrement(tableName, pk, sk string, now types.AttributeValue) *types.Update {
	return &types.Update{
		TableName: aws.String(tableName), Key: map[string]types.AttributeValue{"pk": stringAttribute(pk), "sk": stringAttribute(sk)},
		ConditionExpression:       aws.String("attribute_exists(#active_count) AND #active_count >= :one"),
		UpdateExpression:          aws.String("SET #active_count = #active_count - :one, #updated_at = :updated_at"),
		ExpressionAttributeNames:  map[string]string{"#active_count": "active_count", "#updated_at": "updated_at"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":one": numberAttribute(1), ":updated_at": now},
	}
}

func sessionPK(sessionID string) string       { return "VOICE#SESSION#" + sessionID }
func userCapacityPK(userID string) string     { return "VOICE#CAPACITY#USER#" + userID }
func idempotencyPK(userID, key string) string { return "VOICE#IDEMPOTENCY#" + userID + "#" + key }

func sessionKey(sessionID string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"pk": stringAttribute(sessionPK(sessionID)), "sk": stringAttribute(sessionSortKey)}
}

func turnKey(sessionID string, sequence int64) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"pk": stringAttribute(sessionPK(sessionID)), "sk": stringAttribute(turnSortKey(sequence))}
}

func turnSortKey(sequence int64) string { return fmt.Sprintf("TURN#%020d", sequence) }

func leaseSortKey(at time.Time, sessionID string) string { return formatTime(at) + "#" + sessionID }

func addExpectedBranchCondition(condition, expectedBranchID string, values map[string]types.AttributeValue) string {
	if expectedBranchID == "" {
		return condition + " AND attribute_not_exists(#branch_id)"
	}
	values[":expected_branch_id"] = stringAttribute(expectedBranchID)
	return condition + " AND #branch_id = :expected_branch_id"
}
func formatTime(value time.Time) string         { return value.UTC().Format(timeKeyFormat) }
func parseTime(value string) (time.Time, error) { return time.Parse(timeKeyFormat, value) }
func stringAttribute(value string) types.AttributeValue {
	return &types.AttributeValueMemberS{Value: value}
}
func numberAttribute(value int64) types.AttributeValue {
	return &types.AttributeValueMemberN{Value: strconv.FormatInt(value, 10)}
}
func boolAttribute(value bool) types.AttributeValue {
	return &types.AttributeValueMemberBOOL{Value: value}
}

func attributeString(item map[string]types.AttributeValue, key string) string {
	value, ok := item[key].(*types.AttributeValueMemberS)
	if !ok {
		return ""
	}
	return value.Value
}

func cancellationReasonIs(err error, index int, code string) bool {
	var cancellation *types.TransactionCanceledException
	if !errors.As(err, &cancellation) || index < 0 || index >= len(cancellation.CancellationReasons) {
		return false
	}
	return aws.ToString(cancellation.CancellationReasons[index].Code) == code
}

func transactionToken(operation, sessionID string) string {
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte(operation+"|"+sessionID)))
	digestLength := 36 - len(operation) - 1
	if digestLength > len(sum) {
		digestLength = len(sum)
	}
	return operation + "-" + sum[:digestLength]
}
