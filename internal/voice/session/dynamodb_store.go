package session

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"time"

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

	nowAttribute := stringAttribute(formatTime(now))
	conditionExpression := "#status = :active AND #user_id = :user_id AND #business_id = :business_id AND #capacity_released = :false"
	releaseValues := map[string]types.AttributeValue{
		":active": stringAttribute(string(StatusActive)), ":closing": stringAttribute(string(StatusClosing)),
		":user_id": stringAttribute(scope.UserID), ":business_id": stringAttribute(scope.BusinessID),
		":false": boolAttribute(false), ":true": boolAttribute(true), ":updated_at": nowAttribute,
		":lease_pk": stringAttribute(leasePartitionKey), ":stop_pending_sk": stringAttribute(leaseSortKey(now, sessionID)),
	}
	conditionExpression = addExpectedBranchCondition(conditionExpression, stored.BranchID, releaseValues)
	transaction := &dynamodb.TransactWriteItemsInput{
		ClientRequestToken: aws.String(transactionToken("close", sessionID)),
		TransactItems: []types.TransactWriteItem{
			{Update: &types.Update{
				TableName: aws.String(s.tableName), Key: sessionKey(sessionID),
				ConditionExpression: aws.String(conditionExpression),
				UpdateExpression:    aws.String("SET #status = :closing, #capacity_released = :true, #updated_at = :updated_at, #gsi2pk = :lease_pk, #gsi2sk = :stop_pending_sk REMOVE #gsi1pk, #gsi1sk"),
				ExpressionAttributeNames: map[string]string{
					"#status": "status", "#user_id": "user_id", "#business_id": "business_id", "#capacity_released": "capacity_released",
					"#branch_id": "branch_id", "#updated_at": "updated_at", "#gsi1pk": "GSI1PK", "#gsi1sk": "GSI1SK", "#gsi2pk": "GSI2PK", "#gsi2sk": "GSI2SK",
				},
				ExpressionAttributeValues: releaseValues,
			}},
			{Update: capacityDecrement(s.tableName, globalCapacityPK, globalCapacitySK, nowAttribute)},
			{Update: capacityDecrement(s.tableName, userCapacityPK(scope.UserID), userCapacitySK, nowAttribute)},
		},
	}
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
	output, err := s.db.Query(ctx, &dynamodb.QueryInput{
		TableName: aws.String(s.tableName), IndexName: aws.String(s.leaseIndexName), Limit: aws.Int32(limit),
		ConsistentRead: aws.Bool(false), ScanIndexForward: aws.Bool(true),
		KeyConditionExpression:   aws.String("#gsi2pk = :lease AND #gsi2sk <= :cutoff"),
		ExpressionAttributeNames: map[string]string{"#gsi2pk": "GSI2PK", "#gsi2sk": "GSI2SK"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":lease": stringAttribute(leasePartitionKey), ":cutoff": stringAttribute(leaseSortKey(before, "\uffff")),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("query expired voice leases: %w", err)
	}
	values := make([]*Session, 0, len(output.Items))
	for _, item := range output.Items {
		value, decodeErr := unmarshalSession(item)
		if decodeErr != nil {
			return nil, fmt.Errorf("decode expired voice lease: %w", decodeErr)
		}
		values = append(values, value)
	}
	return values, nil
}

func (s *DynamoDBStore) validate() error {
	if s == nil || s.db == nil || s.tableName == "" {
		return ErrUnavailable
	}
	return nil
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
