package tools

import (
	"encoding/json"
	"math"
	"strconv"
)

func projectToolResponse(plan toolPlan, document []byte, businessID string) (json.RawMessage, error) {
	value, err := decodeUniqueJSON(document)
	if err != nil || !allBusinessIDsMatch(value, businessID) {
		return nil, ErrInvalidToolResponse
	}

	var projected any
	switch plan.kind {
	case toolListInvoices:
		projected, err = projectInvoiceList(value, businessID, plan.limit)
	case toolGetInvoice:
		projected, err = projectInvoice(value, businessID, plan.expectedID)
	case toolListCustomers:
		projected, err = projectCustomerList(value, businessID, plan.page, plan.limit)
	case toolGetCustomer:
		projected, err = projectCustomer(value, businessID, plan.expectedID)
	default:
		err = ErrInvalidToolResponse
	}
	if err != nil {
		return nil, ErrInvalidToolResponse
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		return nil, ErrInvalidToolResponse
	}
	return json.RawMessage(encoded), nil
}

func projectInvoiceList(value any, businessID string, requestedLimit int) (map[string]any, error) {
	envelope, ok := exactObject(value, "items", "next_cursor")
	if !ok {
		return nil, ErrInvalidToolResponse
	}
	items, ok := envelope["items"].([]any)
	if !ok || len(items) > requestedLimit || len(items) > maximumToolLimit {
		return nil, ErrInvalidToolResponse
	}
	projectedItems := make([]any, 0, len(items))
	seenIDs := make(map[string]struct{}, len(items))
	for _, item := range items {
		invoice, err := projectInvoice(item, businessID, "")
		if err != nil {
			return nil, err
		}
		id := invoice["id"].(string)
		if _, duplicate := seenIDs[id]; duplicate {
			return nil, ErrInvalidToolResponse
		}
		seenIDs[id] = struct{}{}
		projectedItems = append(projectedItems, invoice)
	}

	var nextCursor any
	switch cursor := envelope["next_cursor"].(type) {
	case nil:
		nextCursor = nil
	case string:
		if !validCursorToken(cursor, businessID) {
			return nil, ErrInvalidToolResponse
		}
		nextCursor = cursor
	default:
		return nil, ErrInvalidToolResponse
	}
	return map[string]any{"items": projectedItems, "next_cursor": nextCursor}, nil
}

func projectInvoice(value any, businessID, expectedID string) (map[string]any, error) {
	record, ok := value.(map[string]any)
	if !ok || !recordHasTenantAndCanonicalID(record, businessID, expectedID) {
		return nil, ErrInvalidToolResponse
	}
	projected := map[string]any{"id": record["id"].(string)}

	if err := copyOptionalText(record, projected, "invoice_no", "invoice_number", 128); err != nil {
		return nil, err
	}
	for _, field := range []struct {
		name     string
		maximum  int
		required bool
	}{
		{name: "status", maximum: 64, required: true},
		{name: "invoice_date", maximum: 64, required: true},
		{name: "issued_at", maximum: 64},
		{name: "due_date", maximum: 64},
		{name: "currency", maximum: 8, required: true},
	} {
		if err := copyText(record, projected, field.name, field.name, field.maximum, field.required); err != nil {
			return nil, err
		}
	}
	for _, field := range []string{
		"subtotal", "tax", "discount", "total", "paid_amount", "balance_due",
	} {
		if err := copyRequiredNonnegativeNumber(record, projected, field); err != nil {
			return nil, err
		}
	}
	if buyer, present := record["buyer_snapshot"]; present && buyer != nil {
		buyerObject, ok := buyer.(map[string]any)
		if !ok {
			return nil, ErrInvalidToolResponse
		}
		if err := copyOptionalText(buyerObject, projected, "name", "customer_name", 255); err != nil {
			return nil, err
		}
	}
	return projected, nil
}

func projectCustomerList(value any, businessID string, requestedPage, requestedLimit int) (map[string]any, error) {
	envelope, ok := exactObject(value, "data", "total", "page", "limit")
	if !ok {
		return nil, ErrInvalidToolResponse
	}
	data, ok := envelope["data"].([]any)
	if !ok || len(data) > requestedLimit || len(data) > maximumToolLimit {
		return nil, ErrInvalidToolResponse
	}
	total, ok := nonnegativeInteger(envelope["total"])
	if !ok || total < int64(len(data)) {
		return nil, ErrInvalidToolResponse
	}
	page, ok := boundedInteger(envelope["page"], 1, maximumCustomerPage)
	if !ok || page != requestedPage {
		return nil, ErrInvalidToolResponse
	}
	limit, ok := boundedInteger(envelope["limit"], 1, maximumToolLimit)
	if !ok || limit != requestedLimit {
		return nil, ErrInvalidToolResponse
	}

	projectedData := make([]any, 0, len(data))
	seenIDs := make(map[string]struct{}, len(data))
	for _, item := range data {
		customer, err := projectCustomer(item, businessID, "")
		if err != nil {
			return nil, err
		}
		id := customer["id"].(string)
		if _, duplicate := seenIDs[id]; duplicate {
			return nil, ErrInvalidToolResponse
		}
		seenIDs[id] = struct{}{}
		projectedData = append(projectedData, customer)
	}
	return map[string]any{
		"data": projectedData, "total": json.Number(strconv.FormatInt(total, 10)), "page": page, "limit": limit,
	}, nil
}

func projectCustomer(value any, businessID, expectedID string) (map[string]any, error) {
	record, ok := value.(map[string]any)
	if !ok || !recordHasTenantAndCanonicalID(record, businessID, expectedID) {
		return nil, ErrInvalidToolResponse
	}
	name, ok := record["name"].(string)
	if !ok || !safeText(name, 1, 255) {
		return nil, ErrInvalidToolResponse
	}
	projected := map[string]any{"id": record["id"].(string), "name": name}
	for _, field := range []struct {
		name    string
		maximum int
	}{
		{name: "display_name", maximum: 255},
		{name: "company_name", maximum: 255},
		{name: "email", maximum: 320},
		{name: "phone", maximum: 64},
	} {
		if err := copyOptionalText(record, projected, field.name, field.name, field.maximum); err != nil {
			return nil, err
		}
	}
	return projected, nil
}

func exactObject(value any, keys ...string) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	if !ok || len(object) != len(keys) {
		return nil, false
	}
	for _, key := range keys {
		if _, present := object[key]; !present {
			return nil, false
		}
	}
	return object, true
}

func recordHasTenantAndCanonicalID(record map[string]any, businessID, expectedID string) bool {
	recordBusinessID, ok := record["business_id"].(string)
	if !ok || recordBusinessID != businessID {
		return false
	}
	id, ok := record["id"].(string)
	if !ok || !canonicalUUID(id) || (expectedID != "" && id != expectedID) {
		return false
	}
	return true
}

func allBusinessIDsMatch(value any, businessID string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if key == "business_id" {
				returned, ok := nested.(string)
				if !ok || returned != businessID {
					return false
				}
			}
			if !allBusinessIDsMatch(nested, businessID) {
				return false
			}
		}
	case []any:
		for _, nested := range typed {
			if !allBusinessIDsMatch(nested, businessID) {
				return false
			}
		}
	}
	return true
}

func copyOptionalText(source, target map[string]any, sourceName, targetName string, maximum int) error {
	return copyText(source, target, sourceName, targetName, maximum, false)
}

func copyText(source, target map[string]any, sourceName, targetName string, maximum int, required bool) error {
	value, present := source[sourceName]
	if !present || value == nil {
		if required {
			return ErrInvalidToolResponse
		}
		return nil
	}
	text, ok := value.(string)
	minimum := 0
	if required {
		minimum = 1
	}
	if !ok || !safeText(text, minimum, maximum) {
		return ErrInvalidToolResponse
	}
	target[targetName] = text
	return nil
}

func copyRequiredNonnegativeNumber(source, target map[string]any, field string) error {
	value, present := source[field]
	if !present {
		return ErrInvalidToolResponse
	}
	number, ok := value.(json.Number)
	if !ok {
		return ErrInvalidToolResponse
	}
	parsed, err := strconv.ParseFloat(number.String(), 64)
	if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) || parsed < 0 || parsed > 1e18 {
		return ErrInvalidToolResponse
	}
	target[field] = number
	return nil
}

func nonnegativeInteger(value any) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	integer, err := strconv.ParseInt(number.String(), 10, 64)
	return integer, err == nil && integer >= 0
}
