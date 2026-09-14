package prizepool

import (
	"bytes"
	"context"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

func testNodeConfig() NodeConfig {
	c := DefaultNodeConfig()
	c.Host = "127.0.0.1:9735"
	c.NodeID = "02" + strings.Repeat("ab", 32)
	c.Rune = "private-monitor-rune"
	c.ProvisionRune = "private-provision-rune"
	c.CFToken = "private-cloudflare-token"
	c.Active = true
	return c
}
func TestNodeConfigEncryption(t *testing.T) {
	c := testNodeConfig()
	one, err := encryptSettings("test root secret", c.Settings)
	if err != nil {
		t.Fatal(err)
	}
	two, err := encryptSettings("test root secret", c.Settings)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(one, two) {
		t.Fatal("encryption reused nonce")
	}
	for _, secret := range []string{c.Rune, c.ProvisionRune, c.CFToken} {
		if bytes.Contains(one, []byte(secret)) {
			t.Fatal("plaintext credential stored")
		}
	}
	got, err := decodeSettings("test root secret", one)
	if err != nil || got != c.Settings {
		t.Fatal("roundtrip failed")
	}
	if _, err = decodeSettings("wrong secret", one); err == nil {
		t.Fatal("wrong key accepted")
	}
	one[len(one)-1] ^= 1
	if _, err = decodeSettings("test root secret", one); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	if _, err = encryptSettings("", c.Settings); err == nil {
		t.Fatal("empty encryption secret accepted")
	}
}
func TestNodeConfigStorage(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	secret := "test root secret"
	empty, err := LoadNodeConfig(ctx, db, secret)
	if err != nil || empty.Active || empty.Enabled() || empty.Revision != 0 {
		t.Fatalf("default configuration: %v", err)
	}
	t.Setenv("PRIZE_CLN_HOST", "ignored.invalid:9735")
	unchanged, err := LoadNodeConfig(ctx, db, secret)
	if err != nil || unchanged != empty {
		t.Fatal("environment affected settings")
	}
	c := testNodeConfig()
	if err = SaveNodeConfig(ctx, db, secret, "accounts-admin", c); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadNodeConfig(ctx, db, secret)
	if err != nil || saved.Settings != c.Settings || saved.Revision != 1 {
		t.Fatal("save/load failed")
	}
	if err = SaveNodeConfig(ctx, db, secret, "other-admin", c); err == nil {
		t.Fatal("stale update accepted")
	}
	// Destination-changing operations hold a shared lock, so settings cannot race them.
	release, err := LockNodeConfig(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 40*time.Millisecond)
	err = SaveNodeConfig(short, db, secret, "accounts-admin", saved)
	cancel()
	release()
	if err == nil {
		t.Fatal("save bypassed in-flight operation lock")
	}
	saved.Host = "replacement.example:9735"
	saved.Rune = "rotated-monitor"
	if err = SaveNodeConfig(ctx, db, secret, "accounts-admin", saved); err != nil {
		t.Fatal(err)
	}
	saved, err = LoadNodeConfig(ctx, db, secret)
	if err != nil {
		t.Fatal(err)
	}
	id := seedPool(t, db, "bound", "bound-offer")
	if _, err = db.Exec(ctx, `UPDATE community_prize_pools SET node_id=$1 WHERE id=$2`, saved.NodeID, id); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*NodeConfig){func(c *NodeConfig) { c.NodeID = "03" + strings.Repeat("cd", 32) }, func(c *NodeConfig) { c.Domain = "elsewhere.example" }, func(c *NodeConfig) { c.Network = "signet" }} {
		changed := saved
		change(&changed)
		if err = SaveNodeConfig(ctx, db, secret, "accounts-admin", changed); err == nil {
			t.Fatal("bound destination changed")
		}
	}
	saved.Active = false
	saved.Rune = ""
	saved.ProvisionRune = ""
	saved.CFToken = ""
	if err = SaveNodeConfig(ctx, db, secret, "accounts-admin", saved); err != nil {
		t.Fatal(err)
	}
	saved, err = LoadNodeConfig(ctx, db, secret)
	if err != nil || saved.Active || saved.Rune != "" {
		t.Fatal("disable/remove failed")
	}
	var audits int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM community_node_config_audit`).Scan(&audits); err != nil || audits != 3 {
		t.Fatalf("audit count=%d err=%v", audits, err)
	}
}
func TestNodeConfigMonitorReload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	current := DefaultNodeConfig()
	var loadErr error
	events := make(chan string, 20)
	done := make(chan struct{})
	go func() {
		defer close(done)
		followNodeConfig(ctx, time.Millisecond, func(context.Context) (NodeConfig, error) { mu.Lock(); defer mu.Unlock(); return current, loadErr }, func(ctx context.Context, c NodeConfig) {
			events <- "start:" + c.Rune
			<-ctx.Done()
			events <- "stop:" + c.Rune
		}, log.New(io.Discard, "", 0))
	}()
	expect := func(want string) {
		t.Helper()
		select {
		case got := <-events:
			if got != want {
				t.Fatalf("got %q want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("no event %q", want)
		}
	}
	select {
	case <-events:
		t.Fatal("disabled configuration started monitor")
	case <-time.After(10 * time.Millisecond):
	}
	mu.Lock()
	current = testNodeConfig()
	mu.Unlock()
	expect("start:private-monitor-rune")
	mu.Lock()
	current.Rune = "rotated"
	current.Revision++
	mu.Unlock()
	expect("stop:private-monitor-rune")
	expect("start:rotated")
	mu.Lock()
	current.Active = false
	mu.Unlock()
	expect("stop:rotated")
	mu.Lock()
	current.Active = true
	mu.Unlock()
	expect("start:rotated")
	mu.Lock()
	loadErr = io.ErrUnexpectedEOF
	mu.Unlock()
	expect("stop:rotated")
	mu.Lock()
	loadErr = nil
	mu.Unlock()
	expect("start:rotated")
	cancel()
	expect("stop:rotated")
	<-done
}
