package main

import "testing"

func TestMatchesSHA256(t *testing.T) {
	const secret = "admin-secret"
	const digest = "16175223c8ddce5ace0493c948569c211b03c4c6bb3d3e484434999448cffe01"

	if err := matchesSHA256(secret, digest); err != nil {
		t.Fatalf("matchesSHA256 returned error: %v", err)
	}
}

func TestMatchesSHA256Mismatch(t *testing.T) {
	const digest = "16175223c8ddce5ace0493c948569c211b03c4c6bb3d3e484434999448cffe01"

	if err := matchesSHA256("wrong-secret", digest); err == nil {
		t.Fatal("matchesSHA256 returned nil error for mismatched secret")
	}
}

func TestVerifySecretRequiresValue(t *testing.T) {
	if err := verifySecret(""); err == nil {
		t.Fatal("verifySecret returned nil error for empty secret")
	}
}
