package tests

import (
	"os"
	"strings"
	"testing"
)

func TestInvoiceServiceHasNoCopiedTestableImplementation(t *testing.T) {
	body, err := os.ReadFile("../internal/services/invoice_service.go")
	if err != nil {
		t.Fatalf("read production invoice service: %v", err)
	}
	for _, forbidden := range []string{
		"type InvoiceServiceTestable",
		"NewInvoiceServiceForTesting",
		"type InvoiceRepositoryTestable",
		"type CustomerRepositoryTestable",
	} {
		if strings.Contains(string(body), forbidden) {
			t.Errorf("production invoice service retains copied test-only surface %q", forbidden)
		}
	}
}
