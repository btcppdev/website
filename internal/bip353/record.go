// Package bip353 creates and manages DNS TXT records containing BIP 353
// Bitcoin payment instructions.
package bip353

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var (
	// ErrNotFound means no BIP 353 TXT record exists for the requested user.
	ErrNotFound = errors.New("bip353 record not found")
	// ErrConflict means more than one bitcoin: TXT record exists at a BIP 353
	// name. BIP 353 requires clients to reject this situation.
	ErrConflict = errors.New("multiple bip353 records exist at the same name")
)

// Entry describes the desired payment instruction for one human-readable name.
type Entry struct {
	User string
	URI  string
	// TTL is expressed in seconds. A zero value uses the Manager default.
	TTL uint32
}

// Record is a TXT record returned by a Provider.
type Record struct {
	ID      string
	Name    string
	Content string
	TTL     uint32
}

// Result describes the outcome of Put.
type Result struct {
	Record  Record
	Created bool
	Changed bool
}

// RecordName returns the DNS name specified by BIP 353 for user@domain.
// This package intentionally accepts printable ASCII DNS labels only. Callers
// that need internationalized names should convert them to Punycode first.
func RecordName(user, domain string) (string, error) {
	user = strings.ToLower(strings.TrimSpace(user))
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))

	if err := validateLabels("user", user); err != nil {
		return "", err
	}
	if err := validateLabels("domain", domain); err != nil {
		return "", err
	}

	name := user + ".user._bitcoin-payment." + domain
	if len(name) > 253 {
		return "", fmt.Errorf("bip353 record name is %d bytes; maximum is 253", len(name))
	}
	return name, nil
}

func validateLabels(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 {
			return fmt.Errorf("%s contains an empty or oversized DNS label", field)
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("%s DNS labels cannot begin or end with a hyphen", field)
		}
		for _, c := range label {
			if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
				continue
			}
			return fmt.Errorf("%s must use lowercase ASCII letters, digits, dots, or hyphens", field)
		}
	}
	return nil
}

func validateURI(value string) error {
	if value == "" {
		return errors.New("bitcoin URI is required")
	}
	for _, c := range value {
		if c <= 0x20 || c == 0x7f {
			return errors.New("bitcoin URI cannot contain spaces or control characters")
		}
	}
	u, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("invalid bitcoin URI: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "bitcoin") {
		return errors.New("payment instruction must use the bitcoin: URI scheme")
	}
	if u.Fragment != "" {
		return errors.New("bitcoin URI cannot contain a fragment")
	}
	return nil
}

func isBitcoinURI(value string) bool {
	return len(value) >= len("bitcoin:") && strings.EqualFold(value[:len("bitcoin:")], "bitcoin:")
}
