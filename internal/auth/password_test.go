package auth

import "testing"

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("a long test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "a long test passphrase") {
		t.Fatal("correct password was rejected")
	}
	if CheckPassword(hash, "a different test passphrase") {
		t.Fatal("incorrect password was accepted")
	}
	if CheckPassword("not-a-hash", "a long test passphrase") {
		t.Fatal("malformed hash was accepted")
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("too short"); err == nil {
		t.Fatal("short password was accepted")
	}
	if err := ValidatePassword("correct horse battery staple"); err != nil {
		t.Fatalf("passphrase rejected: %v", err)
	}
}
