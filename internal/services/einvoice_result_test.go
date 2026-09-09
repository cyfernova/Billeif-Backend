package services

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateGSTEInvoiceResult(t *testing.T) {
	now := time.Now()
	valid := func() *GSTEInvoiceResult {
		return &GSTEInvoiceResult{IRN: strings.Repeat("a", 64), AckNumber: "123456789012345", AckDate: &now, SignedQRCodePayload: "provider-signed-qr"}
	}
	require.NoError(t, validateGSTEInvoiceResult(valid()))
	require.Error(t, validateGSTEInvoiceResult(nil))
	require.Error(t, validateGSTEInvoiceResult(parseEInvoiceResult(map[string]interface{}{"Status": 0})))
	for _, test := range []struct {
		name   string
		change func(*GSTEInvoiceResult)
	}{
		{"invalid IRN", func(r *GSTEInvoiceResult) { r.IRN = strings.Repeat("z", 64) }},
		{"short IRN", func(r *GSTEInvoiceResult) { r.IRN = "abc" }},
		{"no acknowledgement", func(r *GSTEInvoiceResult) { r.AckNumber = "" }},
		{"no date", func(r *GSTEInvoiceResult) { r.AckDate = nil }},
		{"no QR", func(r *GSTEInvoiceResult) { r.SignedQRCodePayload = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := valid()
			test.change(result)
			require.Error(t, validateGSTEInvoiceResult(result))
		})
	}
}
