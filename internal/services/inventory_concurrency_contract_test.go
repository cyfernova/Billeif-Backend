package services

import (
	"os"
	"strings"
	"testing"
)

func TestInventoryBalanceMutationLocksCurrentRow(t *testing.T) {
	source, err := os.ReadFile("inventory_domain_service.go")
	if err != nil {
		t.Fatalf("read inventory service: %v", err)
	}
	serviceSource := string(source)
	start := strings.Index(serviceSource, "func (s *InventoryService) applySimpleBalanceTx")
	if start < 0 {
		t.Fatal("inventory balance mutation function not found")
	}
	end := strings.Index(serviceSource[start:], "func (s *InventoryService) resolveBatchTx")
	if end < 0 {
		t.Fatal("inventory balance mutation function end not found")
	}
	mutation := serviceSource[start : start+end]
	if !strings.Contains(mutation, `Clauses(clause.Locking{Strength: "UPDATE"})`) {
		t.Fatal("inventory balance must be locked before checking and changing on-hand stock")
	}
}
