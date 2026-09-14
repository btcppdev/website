package bip353

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type memoryProvider struct {
	dnssecErr error
	nextID    int
	records   []Record
}

func (p *memoryProvider) CheckDNSSEC(context.Context) error { return p.dnssecErr }
func (p *memoryProvider) ListTXT(_ context.Context, name string) ([]Record, error) {
	var result []Record
	for _, record := range p.records {
		if record.Name == name {
			result = append(result, record)
		}
	}
	return result, nil
}
func (p *memoryProvider) CreateTXT(_ context.Context, record Record) (Record, error) {
	p.nextID++
	record.ID = fmt.Sprintf("record-%d", p.nextID)
	p.records = append(p.records, record)
	return record, nil
}
func (p *memoryProvider) UpdateTXT(_ context.Context, record Record) (Record, error) {
	for i := range p.records {
		if p.records[i].ID == record.ID {
			p.records[i] = record
			return record, nil
		}
	}
	return Record{}, ErrNotFound
}
func (p *memoryProvider) DeleteTXT(_ context.Context, id string) error {
	for i := range p.records {
		if p.records[i].ID == id {
			p.records = append(p.records[:i], p.records[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func TestManagerLifecycle(t *testing.T) {
	t.Parallel()
	provider := &memoryProvider{}
	manager, err := NewManager(provider, "zap.example.com")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	result, err := manager.Put(ctx, Entry{User: "conf-2027", URI: "bitcoin:?lno=lno1first"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || !result.Changed || result.Record.TTL != DefaultTTL {
		t.Fatalf("unexpected create result: %+v", result)
	}

	result, err = manager.Put(ctx, Entry{User: "conf-2027", URI: "bitcoin:?lno=lno1first"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Created || result.Changed {
		t.Fatalf("idempotent Put changed record: %+v", result)
	}

	result, err = manager.Put(ctx, Entry{User: "conf-2027", URI: "bitcoin:?lno=lno1second", TTL: 600})
	if err != nil {
		t.Fatal(err)
	}
	if result.Created || !result.Changed || result.Record.Content != "bitcoin:?lno=lno1second" {
		t.Fatalf("unexpected update result: %+v", result)
	}

	if err := manager.Delete(ctx, "conf-2027"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Get(ctx, "conf-2027"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete error = %v, want ErrNotFound", err)
	}
}

func TestManagerPreservesUnrelatedTXT(t *testing.T) {
	t.Parallel()
	name := "conf.user._bitcoin-payment.zap.example.com"
	provider := &memoryProvider{records: []Record{{ID: "verification", Name: name, Content: "site-verification=123", TTL: 300}}}
	manager, _ := NewManager(provider, "zap.example.com")
	if _, err := manager.Put(context.Background(), Entry{User: "conf", URI: "bitcoin:?lno=lno1test"}); err != nil {
		t.Fatal(err)
	}
	if len(provider.records) != 2 || provider.records[0].ID != "verification" {
		t.Fatalf("unrelated TXT record was not preserved: %+v", provider.records)
	}
}

func TestManagerRejectsConflictingPaymentRecords(t *testing.T) {
	t.Parallel()
	name := "conf.user._bitcoin-payment.zap.example.com"
	provider := &memoryProvider{records: []Record{
		{ID: "one", Name: name, Content: "bitcoin:?lno=lno1one", TTL: 300},
		{ID: "two", Name: name, Content: "BITCOIN:?lno=lno1two", TTL: 300},
	}}
	manager, _ := NewManager(provider, "zap.example.com")
	_, err := manager.Put(context.Background(), Entry{User: "conf", URI: "bitcoin:?lno=lno1new"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Put error = %v, want ErrConflict", err)
	}
}

func TestManagerRequiresDNSSECForPut(t *testing.T) {
	t.Parallel()
	provider := &memoryProvider{dnssecErr: errors.New("not active")}
	manager, _ := NewManager(provider, "zap.example.com")
	_, err := manager.Put(context.Background(), Entry{User: "conf", URI: "bitcoin:?lno=lno1test"})
	if err == nil || len(provider.records) != 0 {
		t.Fatalf("Put error = %v, records = %+v", err, provider.records)
	}
}
