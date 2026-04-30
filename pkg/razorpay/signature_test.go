package razorpay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func hmacHex(message, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyRazorpayPaymentSignature(t *testing.T) {
	secret := "test_secret"
	orderID := "order_123"
	paymentID := "pay_123"
	valid := hmacHex(orderID+"|"+paymentID, secret)

	if !VerifyRazorpayPaymentSignature(orderID, paymentID, valid, secret) {
		t.Fatalf("expected valid payment signature")
	}
	if VerifyRazorpayPaymentSignature(orderID, paymentID, hmacHex(orderID+"|pay_other", secret), secret) {
		t.Fatalf("expected invalid payment signature to fail")
	}
	if VerifyRazorpayPaymentSignature(orderID, paymentID, "not-hex", secret) {
		t.Fatalf("expected invalid hex payment signature to fail")
	}
}

func TestVerifyRazorpayWebhook(t *testing.T) {
	secret := "webhook_secret"
	body := []byte(`{"event":"payment.captured","payload":{"payment":{"entity":{"id":"pay_123"}}}}`)
	valid := hmacHex(string(body), secret)

	if !VerifyRazorpayWebhook(body, valid, secret) {
		t.Fatalf("expected valid webhook signature")
	}
	if VerifyRazorpayWebhook([]byte(`{"event":"payment.failed"}`), valid, secret) {
		t.Fatalf("expected tampered body to fail")
	}
	if VerifyRazorpayWebhook(body, "1234", secret) {
		t.Fatalf("expected wrong signature length to fail")
	}
}
