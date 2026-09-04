package getters

import (
	"strings"
	"testing"

	"github.com/nbd-wtf/go-nostr/nip19"
)

func TestNormalizeNostrPubkey(t *testing.T) {
	const npub = "npub10elfcs4fr0l0r8af98jlmgdh9c8tcxjvz9qkw038js35mp4dma8qzvjptg"
	const hexKey = "7e7e9c42a91bfef19fa929e5fda1b72e0ebc1a4c1141673e2794234d86addf4e"
	nprofile, err := nip19.EncodeProfile(hexKey, []string{"wss://relay.example"})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{npub, "nostr:" + npub, "https://njump.me/" + npub, hexKey, nprofile, "nostr:" + nprofile} {
		got, err := NormalizeNostrPubkey(input)
		if err != nil || got != hexKey {
			t.Fatalf("NormalizeNostrPubkey(%q) = %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"", "npub1bad", "nprofile1bad", "nsec1bad", "abcd"} {
		if _, err := NormalizeNostrPubkey(input); err == nil {
			t.Fatalf("NormalizeNostrPubkey(%q) accepted invalid key", input)
		}
	}
}

func TestNostrPubkeyDisplay(t *testing.T) {
	const npub = "npub10elfcs4fr0l0r8af98jlmgdh9c8tcxjvz9qkw038js35mp4dma8qzvjptg"
	const hexKey = "7e7e9c42a91bfef19fa929e5fda1b72e0ebc1a4c1141673e2794234d86addf4e"
	if got := NostrPubkeyDisplay(hexKey); got != npub {
		t.Fatalf("NostrPubkeyDisplay() = %q, want %q", got, npub)
	}
}

func TestCanonicalNostrProfileValueConvertsNprofileToNpub(t *testing.T) {
	const npub = "npub10elfcs4fr0l0r8af98jlmgdh9c8tcxjvz9qkw038js35mp4dma8qzvjptg"
	const hexKey = "7e7e9c42a91bfef19fa929e5fda1b72e0ebc1a4c1141673e2794234d86addf4e"
	nprofile, err := nip19.EncodeProfile(hexKey, []string{"wss://relay.example"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := CanonicalNostrProfileValue(nprofile); err != nil || got != npub {
		t.Fatalf("CanonicalNostrProfileValue(%q) = %q, %v; want %q", nprofile, got, err, npub)
	}
	if got, err := CanonicalNostrProfileValue(""); err != nil || got != "" {
		t.Fatalf("CanonicalNostrProfileValue(empty) = %q, %v", got, err)
	}
	const submitted = "nprofile1qqsrmnq40gpsfmpxagf35r6w2ahzmfnl7hrxnqy5n32m6lctkx67lgghg096x"
	if got, err := CanonicalNostrProfileValue(submitted); err != nil || !strings.HasPrefix(got, "npub1") {
		t.Fatalf("CanonicalNostrProfileValue(submitted nprofile) = %q, %v", got, err)
	}
}
