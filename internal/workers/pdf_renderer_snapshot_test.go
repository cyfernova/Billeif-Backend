package workers

import (
	"testing"

	"invoice-backend/internal/models"
)

func TestFrozenRenderPartiesPreferIssuedSourceLinkageSnapshots(t *testing.T) {
	document := &models.Document{
		ID:         "invoice-1",
		BusinessID: "business-1",
		PartyID:    models.StringPointer("live-customer-must-not-load"),
		SourceLinkage: `{
			"seller_snapshot":{
				"name":"Frozen Seller",
				"email":"seller@example.com",
				"phone":"111",
				"address":"Seller Street",
				"city":"Mumbai",
				"state":"Maharashtra",
				"postal_code":"400001",
				"country":"India",
				"gstin":"27AAAAA0000A1Z5"
			},
			"buyer_snapshot":{
				"name":"Frozen Buyer",
				"email":"buyer@example.com",
				"phone":"222",
				"address":"Buyer Street",
				"city":"Pune",
				"state":"Maharashtra",
				"postal_code":"411001",
				"country":"India",
				"gstin":"27BBBBB0000B1Z5"
			}
		}`,
	}

	business, party, ok := frozenRenderParties(document, labelsForLocale("en-IN"))

	if !ok || business == nil || business.Name != "Frozen Seller" ||
		business.GSTIN != "27AAAAA0000A1Z5" {
		t.Fatalf("frozen seller = %#v, ok=%v", business, ok)
	}
	if party.name != "Frozen Buyer" || party.email != "buyer@example.com" ||
		party.taxID != "27BBBBB0000B1Z5" {
		t.Fatalf("frozen buyer = %#v", party)
	}
	if len(party.address) != 3 ||
		party.address[0] != "Buyer Street" ||
		party.address[1] != "Pune, Maharashtra, 411001" ||
		party.address[2] != "India" {
		t.Fatalf("frozen buyer address = %#v", party.address)
	}
}
