package models

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestVendorAddressColumnsMatchMigration(t *testing.T) {
	parsed, err := schema.Parse(&Vendor{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	for field, column := range map[string]string{"BillingJSON": "billing_address_json", "ShippingJSON": "shipping_address_json"} {
		if got := parsed.LookUpField(field).DBName; got != column {
			t.Errorf("%s column = %s, want %s", field, got, column)
		}
	}
}

func TestDocumentGSTColumnsMatchMigration(t *testing.T) {
	parsed, err := schema.Parse(&Document{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	for field, column := range map[string]string{"GenerateEInvoice": "generate_einvoice", "GenerateEWayBill": "generate_ewaybill", "CurrentEInvoiceID": "current_einvoice_id", "CurrentEWayBillID": "current_ewaybill_id"} {
		if got := parsed.LookUpField(field).DBName; got != column {
			t.Errorf("%s column = %s, want %s", field, got, column)
		}
	}
}

func TestDocumentLineHSNColumnMatchesMigration(t *testing.T) {
	parsed, err := schema.Parse(&DocumentLine{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.LookUpField("HSNSACCode").DBName; got != "hsn_sac_code" {
		t.Errorf("HSNSACCode column = %s, want hsn_sac_code", got)
	}
}
