package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ValidateSignature checks the X-Hub-Signature-256 header against the payload.
// signatureHeader must be in the format "sha256=<hex>".
func ValidateSignature(payload []byte, signatureHeader string, secret string) bool {
	if signatureHeader == "" || !strings.HasPrefix(signatureHeader, "sha256=") {
		return false
	}

	gotHex := strings.TrimPrefix(signatureHeader, "sha256=")
	gotSig, err := hex.DecodeString(gotHex)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedSig := mac.Sum(nil)

	return hmac.Equal(gotSig, expectedSig)
}

// ComputeSignature returns the HMAC-SHA256 signature of payload in "sha256=<hex>" format.
func ComputeSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
