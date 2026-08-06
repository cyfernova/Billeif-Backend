package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maximumToolArgumentBytes  = 4 << 10
	maximumToolResponseBytes  = 512 << 10
	maximumAuthorizationBytes = 8 << 10
	maximumCustomerPage       = 1000
	maximumToolLimit          = 20
	defaultToolLimit          = 10
	maximumJSONDepth          = 64
)

type toolKind uint8

const (
	toolListInvoices toolKind = iota + 1
	toolGetInvoice
	toolListCustomers
	toolGetCustomer
)

type toolPlan struct {
	kind       toolKind
	path       string
	query      url.Values
	expectedID string
	limit      int
	page       int
}

// Execute validates one frozen tool call, obtains the latest session
// Authorization value, and returns a least-privilege JSON projection.
func (registry *Registry) Execute(ctx context.Context, name string, arguments json.RawMessage) (json.RawMessage, error) {
	if registry == nil || registry.closed.Load() {
		return nil, ErrRegistryClosed
	}
	if ctx == nil {
		return nil, ErrToolUnavailable
	}
	if !registry.enableCustomerTools && isCustomerTool(name) {
		return nil, ErrUnknownTool
	}
	parsedArguments, err := parseToolArguments(name, arguments)
	if err != nil {
		return nil, err
	}

	authorizationSource, businessID, branchID, err := registry.binding.snapshot()
	if err != nil {
		return nil, err
	}
	if branchID != "" {
		return nil, ErrBranchScopeUnsupported
	}
	if cursor, present := parsedArguments["cursor"].(string); present && !validCursorToken(cursor, businessID) {
		return nil, ErrInvalidToolArguments
	}
	plan := buildToolPlan(name, parsedArguments, businessID)

	callContext, cancel := context.WithTimeout(ctx, registry.timeout)
	stopRegistryCancellation := context.AfterFunc(registry.rootContext, cancel)
	defer func() {
		stopRegistryCancellation()
		cancel()
	}()
	select {
	case <-registry.rootContext.Done():
		return nil, ErrRegistryClosed
	default:
	}

	body, err := registry.request(callContext, authorizationSource, plan)
	if err != nil {
		return nil, err
	}
	projected, err := projectToolResponse(plan, body, businessID)
	if err != nil {
		return nil, err
	}
	return projected, nil
}

func (registry *Registry) request(ctx context.Context, source AuthorizationSource, plan toolPlan) ([]byte, error) {
	requestURL := registry.origin + plan.path
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, ErrToolUnavailable
	}
	request.URL.RawQuery = plan.query.Encode()
	request.Header.Set("Accept", "application/json")

	authorization, err := source.Authorization(ctx)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		if errors.Is(err, context.Canceled) {
			return nil, context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		var refreshRequired interface {
			AuthorizationRefreshRequired() bool
		}
		if errors.As(err, &refreshRequired) && refreshRequired.AuthorizationRefreshRequired() {
			return nil, &AuthorizationRefreshRequiredError{}
		}
		return nil, ErrAuthorizationUnavailable
	}
	if !validAuthorization(authorization) {
		return nil, ErrAuthorizationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", authorization)
	response, err := registry.client.Do(request)
	request.Header.Del("Authorization")
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, ErrToolUnavailable
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, &AuthorizationRefreshRequiredError{}
	case http.StatusForbidden:
		return nil, ErrAuthorizationUnavailable
	case http.StatusNotFound:
		return nil, ErrToolNotFound
	default:
		return nil, ErrToolUnavailable
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, ErrInvalidToolResponse
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumToolResponseBytes+1))
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, ErrToolUnavailable
	}
	if len(body) > maximumToolResponseBytes {
		return nil, ErrToolResponseTooLarge
	}
	return body, nil
}

func validAuthorization(value string) bool {
	if len(value) < len("Bearer ")+1 || len(value) > maximumAuthorizationBytes {
		return false
	}
	separator := strings.IndexByte(value, ' ')
	if separator <= 0 || !strings.EqualFold(value[:separator], "Bearer") {
		return false
	}
	token := value[separator+1:]
	if token == "" {
		return false
	}
	for index := 0; index < len(token); index++ {
		character := token[index]
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func parseToolArguments(name string, raw json.RawMessage) (map[string]any, error) {
	if !knownTool(name) {
		return nil, ErrUnknownTool
	}
	if len(raw) == 0 || len(raw) > maximumToolArgumentBytes || !utf8.Valid(raw) {
		return nil, ErrInvalidToolArguments
	}
	value, err := decodeUniqueJSON(raw)
	if err != nil {
		return nil, ErrInvalidToolArguments
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, ErrInvalidToolArguments
	}

	allowed := map[string]struct{}{}
	switch name {
	case "list_invoices":
		allowed["limit"] = struct{}{}
		allowed["cursor"] = struct{}{}
	case "get_invoice", "get_customer":
		allowed["id"] = struct{}{}
	case "list_customers":
		allowed["page"] = struct{}{}
		allowed["limit"] = struct{}{}
	}
	for key := range object {
		if _, ok := allowed[key]; !ok {
			return nil, ErrInvalidToolArguments
		}
	}

	switch name {
	case "list_invoices":
		if value, present := object["limit"]; present {
			if _, ok := boundedInteger(value, 1, maximumToolLimit); !ok {
				return nil, ErrInvalidToolArguments
			}
		}
		if value, present := object["cursor"]; present {
			cursor, ok := value.(string)
			if !ok || !validCursorToken(cursor, "") {
				return nil, ErrInvalidToolArguments
			}
		}
	case "get_invoice", "get_customer":
		id, ok := object["id"].(string)
		if !ok || !canonicalUUID(id) {
			return nil, ErrInvalidToolArguments
		}
	case "list_customers":
		page := 1
		limit := defaultToolLimit
		if value, present := object["page"]; present {
			var ok bool
			page, ok = boundedInteger(value, 1, maximumCustomerPage)
			if !ok {
				return nil, ErrInvalidToolArguments
			}
		}
		if value, present := object["limit"]; present {
			var ok bool
			limit, ok = boundedInteger(value, 1, maximumToolLimit)
			if !ok {
				return nil, ErrInvalidToolArguments
			}
		}
		maximumInt := int(^uint(0) >> 1)
		if page > maximumInt/limit {
			return nil, ErrInvalidToolArguments
		}
	}
	return object, nil
}

func knownTool(name string) bool {
	switch name {
	case "list_invoices", "get_invoice", "list_customers", "get_customer":
		return true
	default:
		return false
	}
}

func buildToolPlan(name string, arguments map[string]any, businessID string) toolPlan {
	query := url.Values{"business_id": {businessID}}
	switch name {
	case "list_invoices":
		limit := defaultToolLimit
		if raw, present := arguments["limit"]; present {
			limit, _ = boundedInteger(raw, 1, maximumToolLimit)
		}
		query.Set("limit", strconv.Itoa(limit))
		if cursor, present := arguments["cursor"].(string); present {
			query.Set("cursor", cursor)
		}
		return toolPlan{kind: toolListInvoices, path: "/api/v1/invoices", query: query, limit: limit}
	case "get_invoice":
		id := arguments["id"].(string)
		return toolPlan{kind: toolGetInvoice, path: "/api/v1/invoices/" + id, query: query, expectedID: id}
	case "list_customers":
		page, limit := 1, defaultToolLimit
		if raw, present := arguments["page"]; present {
			page, _ = boundedInteger(raw, 1, maximumCustomerPage)
		}
		if raw, present := arguments["limit"]; present {
			limit, _ = boundedInteger(raw, 1, maximumToolLimit)
		}
		query.Set("page", strconv.Itoa(page))
		query.Set("limit", strconv.Itoa(limit))
		return toolPlan{kind: toolListCustomers, path: "/api/v1/customers", query: query, page: page, limit: limit}
	case "get_customer":
		id := arguments["id"].(string)
		return toolPlan{kind: toolGetCustomer, path: "/api/v1/customers/" + id, query: query, expectedID: id}
	default:
		panic("validated tool name became unknown")
	}
}

func boundedInteger(value any, minimum, maximum int) (int, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	integer, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil || integer < int64(minimum) || integer > int64(maximum) {
		return 0, false
	}
	return int(integer), true
}

func decodeUniqueJSON(document []byte) (any, error) {
	if !utf8.Valid(document) {
		return nil, ErrInvalidToolResponse
	}
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()
	value, err := decodeJSONValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidToolResponse
	}
	return value, nil
}

func decodeJSONValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > maximumJSONDepth {
		return nil, ErrInvalidToolResponse
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, ErrInvalidToolResponse
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, ErrInvalidToolResponse
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, ErrInvalidToolResponse
			}
			if _, duplicate := object[key]; duplicate {
				return nil, ErrInvalidToolResponse
			}
			value, err := decodeJSONValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return nil, ErrInvalidToolResponse
		}
		return object, nil
	case '[':
		array := make([]any, 0)
		for decoder.More() {
			value, err := decodeJSONValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return nil, ErrInvalidToolResponse
		}
		return array, nil
	default:
		return nil, ErrInvalidToolResponse
	}
}

func safeText(value string, minimum, maximum int) bool {
	if !utf8.ValidString(value) || len(value) < minimum || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validCursorToken(value, expectedBusinessID string) bool {
	if len(value) == 0 || len(value) > 1024 {
		return false
	}
	segments := strings.Split(value, ".")
	if len(segments) != 3 || segments[0] != "v1" || segments[1] == "" || segments[2] == "" {
		return false
	}
	payloadDocument, err := base64.RawURLEncoding.Strict().DecodeString(segments[1])
	if err != nil {
		return false
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(segments[2])
	if err != nil || len(signature) != 32 {
		return false
	}
	payloadValue, err := decodeUniqueJSON(payloadDocument)
	if err != nil {
		return false
	}
	payload, ok := exactObject(payloadValue, "business_id", "created_at", "id")
	if !ok {
		return false
	}
	businessID, ok := payload["business_id"].(string)
	if !ok || !canonicalUUID(businessID) || (expectedBusinessID != "" && businessID != expectedBusinessID) {
		return false
	}
	id, ok := payload["id"].(string)
	if !ok || !canonicalUUID(id) {
		return false
	}
	createdAt, ok := payload["created_at"].(string)
	if !ok {
		return false
	}
	_, err = time.Parse(time.RFC3339Nano, createdAt)
	return err == nil
}

func (*Registry) String() string { return "Registry{redacted}" }

func (registry *Registry) GoString() string { return registry.String() }

func (*Registry) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }

var _ fmt.Stringer = (*Registry)(nil)
