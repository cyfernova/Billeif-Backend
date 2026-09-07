package services

import (
	"context"
	"testing"

	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/require"
)

func TestCredentialProviderAddPaymentMethodRejectsUnsupportedCapabilityBeforeEncryptionOrRepository(t *testing.T) {
	guard := &recordingCapabilityGuard{err: &CapabilityUnavailableError{
		Code: "capability_unavailable", Capability: CapabilitySavedPayments,
		State: CapabilityStateUnsupported, ReasonCode: ReasonSavedPaymentsUnsupported,
	}}
	service := NewCredentialProviderServiceWithResolver(nil, nil, nil, logger.New()).WithCapabilityGuard(guard)

	_, err := service.AddPaymentMethod(context.Background(), &AddPaymentMethodRequest{
		BusinessID: "biz-1", UserID: "user-1", CardToken: "must-not-be-encrypted",
	})

	var unavailable *CapabilityUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, CapabilitySavedPayments, guard.request.Capability)
	require.Equal(t, "biz-1", guard.request.BusinessID)
	require.Equal(t, "user-1", guard.request.UserID)
}

func TestCredentialProviderOtherPaymentMethodMutationsRejectUnsupportedCapabilityBeforeRepository(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		invoke func(*CredentialProviderService) error
	}{
		{name: "set default", invoke: func(service *CredentialProviderService) error {
			return service.SetDefaultPaymentMethod(context.Background(), "biz-1", "user-1", "credential-1")
		}},
		{name: "delete", invoke: func(service *CredentialProviderService) error {
			return service.DeletePaymentMethod(context.Background(), "biz-1", "user-1", "credential-1")
		}},
		{name: "generate token", invoke: func(service *CredentialProviderService) error {
			_, err := service.GenerateCredentialToken(context.Background(), &GenerateTokenRequest{
				BusinessID: "biz-1", UserID: "user-1", CredentialID: "credential-1",
			})
			return err
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			guard := &recordingCapabilityGuard{err: &CapabilityUnavailableError{
				Code: "capability_unavailable", Capability: CapabilitySavedPayments,
				State: CapabilityStateUnsupported, ReasonCode: ReasonSavedPaymentsUnsupported,
			}}
			service := NewCredentialProviderServiceWithResolver(nil, nil, nil, logger.New()).WithCapabilityGuard(guard)

			err := fixture.invoke(service)

			var unavailable *CapabilityUnavailableError
			require.ErrorAs(t, err, &unavailable)
			require.Equal(t, "biz-1", guard.request.BusinessID)
			require.Equal(t, "user-1", guard.request.UserID)
		})
	}
}
