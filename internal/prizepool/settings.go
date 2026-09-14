package prizepool

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type NodeConfig struct {
	Settings
	Active   bool
	Revision int64
}

func DefaultNodeConfig() NodeConfig {
	return NodeConfig{Settings: Settings{Domain: "zap.btcplusplus.dev", Network: "bitcoin"}}
}

// Domain-separated encryption uses the existing application root secret. Keep
// that secret with database backups; rotate it only with a coordinated re-encryption of these settings.
func settingsCipher(secret string) (cipher.AEAD, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("application encryption secret is unavailable")
	}
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte("btcpp/community-node-config/v1"))
	block, err := aes.NewCipher(h.Sum(nil))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func encryptSettings(secret string, s Settings) ([]byte, error) {
	aead, err := settingsCipher(secret)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, data, []byte("community_node_config:v1")), nil
}
func decodeSettings(secret string, data []byte) (Settings, error) {
	var s Settings
	aead, err := settingsCipher(secret)
	if err != nil {
		return s, err
	}
	if len(data) < aead.NonceSize() {
		return s, errors.New("invalid node configuration")
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte("community_node_config:v1"))
	if err != nil {
		return s, errors.New("unable to decrypt node configuration")
	}
	err = json.Unmarshal(plain, &s)
	return s, err
}
func LoadNodeConfig(ctx context.Context, db *pgxpool.Pool, secret string) (NodeConfig, error) {
	c := DefaultNodeConfig()
	var raw []byte
	err := db.QueryRow(ctx, `SELECT revision,enabled,encrypted_settings FROM community_node_config WHERE singleton`).Scan(&c.Revision, &c.Active, &raw)
	if err != nil {
		return c, err
	}
	if len(raw) > 0 {
		c.Settings, err = decodeSettings(secret, raw)
	}
	return c, err
}

// Hold the configuration stable while an event creates or changes a payment
// destination. SaveNodeConfig takes the conflicting row lock before validation.
func LockNodeConfig(ctx context.Context, db *pgxpool.Pool) (func(), error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	release := func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}
	var singleton bool
	if err = tx.QueryRow(ctx, `SELECT singleton FROM community_node_config WHERE singleton FOR SHARE`).Scan(&singleton); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func ValidateNodeConfig(c NodeConfig) error {
	s := c.Settings
	if s.Network != "bitcoin" && s.Network != "testnet" && s.Network != "regtest" && s.Network != "signet" {
		return errors.New("choose a supported Bitcoin network")
	}
	if len(s.Domain) > 253 || !strings.Contains(s.Domain, ".") {
		return errors.New("enter a valid address domain")
	}
	for _, label := range strings.Split(s.Domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("enter a valid address domain")
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return errors.New("enter a valid address domain")
			}
		}
	}
	if s.Host != "" {
		host, port, err := net.SplitHostPort(s.Host)
		n, _ := strconv.Atoi(port)
		if err != nil || host == "" || n < 1 || n > 65535 || strings.ContainsAny(host, " /\\\t\r\n") {
			return errors.New("enter the node hostname or IP with its port, such as node.example:9735")
		}
	}
	if s.NodeID != "" {
		key, err := hex.DecodeString(s.NodeID)
		if err != nil || len(key) != 33 || (key[0] != 2 && key[0] != 3) {
			return errors.New("enter a compressed CLN node public key")
		}
	}
	if c.Active && !s.Enabled() {
		return errors.New("host, node public key, and monitoring rune are required to enable monitoring")
	}
	return nil
}
func SaveNodeConfig(ctx context.Context, db *pgxpool.Pool, secret, actor string, c NodeConfig) error {
	if err := ValidateNodeConfig(c); err != nil {
		return err
	}
	raw, err := encryptSettings(secret, c.Settings)
	if err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT revision FROM community_node_config WHERE singleton FOR UPDATE`).Scan(&revision); err != nil {
		return err
	}
	if revision != c.Revision {
		return errors.New("configuration changed in another session; reload before saving")
	}
	// Bound payment destinations cannot be silently moved to another node/domain.
	var conflict bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM community_prize_pools WHERE domain<>$1 OR (node_id<>'' AND node_id<>$2))`, c.Domain, c.NodeID).Scan(&conflict)
	if err != nil {
		return err
	}
	if conflict {
		return errors.New("existing prize pools are bound to another node or address domain; destination migration requires a separate review")
	}
	var previous []byte
	if err = tx.QueryRow(ctx, `SELECT encrypted_settings FROM community_node_config WHERE singleton`).Scan(&previous); err != nil {
		return err
	}
	if len(previous) > 0 {
		old, e := decodeSettings(secret, previous)
		if e != nil {
			return e
		}
		if old.Network != c.Network {
			var bound bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM community_prize_pools WHERE node_id<>'')`).Scan(&bound); err != nil {
				return err
			}
			if bound {
				return errors.New("cannot change the network of existing prize pools")
			}
		}
	}
	_, err = tx.Exec(ctx, `UPDATE community_node_config SET revision=revision+1,encrypted_settings=$1,enabled=$2,updated_at=now(),updated_by=$3 WHERE singleton`, raw, c.Active, actor)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO community_node_config_audit(revision,actor,enabled) VALUES($1,$2,$3)`, revision+1, actor, c.Active)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
