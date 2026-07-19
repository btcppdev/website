package payoutdocs

import "testing"

func TestRoundTrip(t *testing.T) {
	plain := []byte("private tax form")
	encrypted, err := Encrypt("test key", plain)
	if err != nil {
		t.Fatal(err)
	}
	if string(encrypted) == string(plain) {
		t.Fatal("encrypted document equals plaintext")
	}
	got, err := Decrypt("test key", encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plain) {
		t.Fatalf("got %q, want %q", got, plain)
	}
	if _, err := Decrypt("wrong key", encrypted); err == nil {
		t.Fatal("wrong key unexpectedly decrypted document")
	}
}
