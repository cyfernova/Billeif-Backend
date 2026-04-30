package razorpay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func VerifyRazorpayPaymentSignature(orderID, paymentID, receivedSignature, keySecret string) bool {
	message := strings.TrimSpace(orderID) + "|" + strings.TrimSpace(paymentID)
	return verifyHMACSHA256([]byte(message), strings.TrimSpace(receivedSignature), keySecret)
}

func VerifyRazorpayWebhook(rawBody []byte, receivedSignature, webhookSecret string) bool {
	return verifyHMACSHA256(rawBody, strings.TrimSpace(receivedSignature), webhookSecret)
}

func verifyHMACSHA256(message []byte, receivedSignature, secret string) bool {
	if len(message) == 0 || strings.TrimSpace(receivedSignature) == "" || secret == "" {
		return false
	}

	received, err := hex.DecodeString(receivedSignature)
	if err != nil || len(received) != sha256.Size {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(message)
	expected := mac.Sum(nil)

	return hmac.Equal(expected, received)
}
