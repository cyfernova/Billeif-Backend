package handlers

import (
	"errors"
	"net/http"
	"testing"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/invoiceresolution"
)

func TestInvoiceCreateErrorStatusMapsIdempotencyErrors(t *testing.T) {
	fixtures := []struct {
		name string
		err  error
		want int
	}{
		{name: "missing or malformed key", err: &idempotency.InvalidKeyError{}, want: http.StatusBadRequest},
		{name: "invalid canonical payload", err: &idempotency.InvalidPayloadError{}, want: http.StatusBadRequest},
		{name: "changed payload conflict", err: &idempotency.ConflictError{}, want: http.StatusConflict},
		{name: "identical request still in progress", err: &idempotency.InProgressError{}, want: http.StatusConflict},
		{name: "line resolver unavailable", err: &invoiceresolution.UnavailableError{}, want: http.StatusServiceUnavailable},
		{name: "wrapped conflict", err: errors.New("not idempotency"), want: http.StatusInternalServerError},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			if got := invoiceCreateErrorStatus(fixture.err); got != fixture.want {
				t.Fatalf("status = %d, want %d", got, fixture.want)
			}
		})
	}
}

func TestInvoiceResolverUnavailableErrorHasStableSanitizedResponse(t *testing.T) {
	err := &invoiceresolution.UnavailableError{}
	if err.Error() != "invoice line resolution is temporarily unavailable" {
		t.Fatalf("error = %q, want stable sanitized response", err.Error())
	}
}
