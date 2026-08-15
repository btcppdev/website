package getters

import "testing"

func TestNormalizeNostrPubkey(t *testing.T) {
	const npub = "npub10elfcs4fr0l0r8af98jlmgdh9c8tcxjvz9qkw038js35mp4dma8qzvjptg"
	const hexKey = "7e7e9c42a91bfef19fa929e5fda1b72e0ebc1a4c1141673e2794234d86addf4e"
	for _, input := range []string{npub, "nostr:" + npub, "https://njump.me/" + npub, hexKey} {
		got, err := NormalizeNostrPubkey(input)
		if err != nil || got != hexKey {
			t.Fatalf("NormalizeNostrPubkey(%q) = %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"", "npub1bad", "nsec1bad", "abcd"} {
		if _, err := NormalizeNostrPubkey(input); err == nil {
			t.Fatalf("NormalizeNostrPubkey(%q) accepted invalid key", input)
		}
	}
}
