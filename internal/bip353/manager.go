package bip353

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const DefaultTTL uint32 = 300

// Provider is the DNS-provider boundary needed by Manager. Implementations
// should scope themselves to a single DNS zone.
type Provider interface {
	CheckDNSSEC(context.Context) error
	ListTXT(context.Context, string) ([]Record, error)
	CreateTXT(context.Context, Record) (Record, error)
	UpdateTXT(context.Context, Record) (Record, error)
	DeleteTXT(context.Context, string) error
}

// Manager manages BIP 353 records below one identity domain, such as
// zap.example.com. It is safe for concurrent use within a process.
type Manager struct {
	provider   Provider
	domain     string
	defaultTTL uint32
	mu         sync.Mutex
}

// NewManager constructs a Manager. provider must be configured for the DNS
// zone containing domain.
func NewManager(provider Provider, domain string) (*Manager, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if _, err := RecordName("validation", domain); err != nil {
		return nil, err
	}
	return &Manager{provider: provider, domain: domain, defaultTTL: DefaultTTL}, nil
}

// Put creates or updates a user's BIP 353 TXT record. Unrelated TXT records at
// the same name are preserved. Put refuses to modify an already-invalid name
// containing multiple bitcoin: TXT records.
func (m *Manager) Put(ctx context.Context, entry Entry) (Result, error) {
	name, err := RecordName(entry.User, m.domain)
	if err != nil {
		return Result{}, err
	}
	if err := validateURI(entry.URI); err != nil {
		return Result{}, err
	}
	if entry.TTL == 0 {
		entry.TTL = m.defaultTTL
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.provider.CheckDNSSEC(ctx); err != nil {
		return Result{}, fmt.Errorf("DNSSEC precondition failed: %w", err)
	}
	existing, err := m.paymentRecord(ctx, name)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Result{}, err
	}
	desired := Record{Name: name, Content: entry.URI, TTL: entry.TTL}
	if existing == nil {
		created, err := m.provider.CreateTXT(ctx, desired)
		if err != nil {
			return Result{}, fmt.Errorf("create %s: %w", name, err)
		}
		return Result{Record: created, Created: true, Changed: true}, nil
	}
	if existing.Content == desired.Content && existing.TTL == desired.TTL {
		return Result{Record: *existing}, nil
	}
	desired.ID = existing.ID
	updated, err := m.provider.UpdateTXT(ctx, desired)
	if err != nil {
		return Result{}, fmt.Errorf("update %s: %w", name, err)
	}
	return Result{Record: updated, Changed: true}, nil
}

// Get returns the one BIP 353 record for user. It returns ErrConflict when the
// name has multiple bitcoin: TXT records.
func (m *Manager) Get(ctx context.Context, user string) (Record, error) {
	name, err := RecordName(user, m.domain)
	if err != nil {
		return Record{}, err
	}
	record, err := m.paymentRecord(ctx, name)
	if err != nil {
		return Record{}, err
	}
	return *record, nil
}

// Delete removes the one BIP 353 record for user while preserving unrelated
// TXT records. Deletion remains available if DNSSEC is temporarily unhealthy.
func (m *Manager) Delete(ctx context.Context, user string) error {
	name, err := RecordName(user, m.domain)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.paymentRecord(ctx, name)
	if err != nil {
		return err
	}
	if err := m.provider.DeleteTXT(ctx, record.ID); err != nil {
		return fmt.Errorf("delete %s: %w", name, err)
	}
	return nil
}

func (m *Manager) paymentRecord(ctx context.Context, name string) (*Record, error) {
	records, err := m.provider.ListTXT(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("list TXT records for %s: %w", name, err)
	}
	var found *Record
	for i := range records {
		if !isBitcoinURI(records[i].Content) {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("%w: %s", ErrConflict, name)
		}
		copy := records[i]
		found = &copy
	}
	if found == nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return found, nil
}
