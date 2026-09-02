package services

import (
	"context"
	"errors"
	"testing"
)

func TestUngovernedAutonomousSurfacesFailClosed(t *testing.T) {
	if _, err := (&A2ABargainingService{}).DisableUngovernedExecution().StartAutonomousNegotiation(context.Background(), A2ANegotiationScope{UserID: "user", BusinessID: "business"}, &AutonomousNegotiationRequest{}); !errors.Is(err, ErrA2AGovernanceRequired) {
		t.Fatalf("autonomous bargaining error = %v", err)
	}
	if _, err := (&ProcurementService{}).DisableUngovernedExecution().StartProcurement(context.Background(), &CreateProcurementRunRequest{}); !errors.Is(err, ErrA2AGovernanceRequired) {
		t.Fatalf("procurement start error = %v", err)
	}
}
