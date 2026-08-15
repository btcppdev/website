package handlers

import (
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

func TestValidateNostrAuthEvent(t *testing.T) {
	secret := nostr.GeneratePrivateKey()
	now := time.Now().UTC().Truncate(time.Second)
	event := nostr.Event{
		CreatedAt: nostr.Timestamp(now.Unix()),
		Kind:      nostrAuthKind,
		Tags:      nostr.Tags{{"u", "https://example.test/auth/nostr/verify"}, {"method", "POST"}, {"challenge", "one-time-challenge"}},
		Content:   "",
	}
	if err := event.Sign(secret); err != nil {
		t.Fatal(err)
	}
	if err := validateNostrAuthEvent(event, "one-time-challenge", "https://example.test/auth/nostr/verify", now); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}

	tampered := event
	tampered.Tags = nostr.Tags{{"u", "https://attacker.test/auth/nostr/verify"}, {"method", "POST"}, {"challenge", "one-time-challenge"}}
	if err := validateNostrAuthEvent(tampered, "one-time-challenge", "https://example.test/auth/nostr/verify", now); err == nil {
		t.Fatal("event bound to another URL was accepted")
	}
	if err := validateNostrAuthEvent(event, "different-challenge", "https://example.test/auth/nostr/verify", now); err == nil {
		t.Fatal("replayed challenge was accepted")
	}
}

func TestSingleNostrTagRejectsDuplicates(t *testing.T) {
	tags := nostr.Tags{{"challenge", "one"}, {"challenge", "two"}}
	if got := singleNostrTag(tags, "challenge"); got != "" {
		t.Fatalf("duplicate challenge returned %q", got)
	}
}
