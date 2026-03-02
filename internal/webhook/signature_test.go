package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestValidateSignature_Valid(t *testing.T) {
	secret := "test-secret"
	payload := []byte(`{"action":"opened"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !ValidateSignature(payload, sig, secret) {
		t.Fatal("expected valid signature to pass validation")
	}
}

func TestValidateSignature_WrongSecret(t *testing.T) {
	payload := []byte(`{"action":"opened"}`)
	sig := ComputeSignature(payload, "correct-secret")

	if ValidateSignature(payload, sig, "wrong-secret") {
		t.Fatal("expected validation to fail with wrong secret")
	}
}

func TestValidateSignature_EmptyHeader(t *testing.T) {
	if ValidateSignature([]byte(`{}`), "", "secret") {
		t.Fatal("expected empty signature header to fail")
	}
}

func TestValidateSignature_MalformedHeader(t *testing.T) {
	cases := []string{
		"md5=abc123",
		"sha256=notvalidhex!!!",
		"sha256=",
		"garbage",
	}
	for _, header := range cases {
		if ValidateSignature([]byte(`{}`), header, "secret") {
			t.Fatalf("expected malformed header %q to fail", header)
		}
	}
}

func TestComputeSignature(t *testing.T) {
	secret := "my-secret"
	payload := []byte(`{"hello":"world"}`)

	result := ComputeSignature(payload, secret)

	// Verify prefix
	if result[:7] != "sha256=" {
		t.Fatalf("expected sha256= prefix, got %s", result[:7])
	}

	// Verify the hex portion matches an independent computation.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if result != expected {
		t.Fatalf("signature mismatch:\n  got:  %s\n  want: %s", result, expected)
	}
}

func TestComputeAndValidateRoundTrip(t *testing.T) {
	secret := "round-trip-secret"
	payload := []byte(`{"event":"push","ref":"refs/heads/main"}`)

	sig := ComputeSignature(payload, secret)
	if !ValidateSignature(payload, sig, secret) {
		t.Fatal("round-trip: ComputeSignature output should validate successfully")
	}
}
