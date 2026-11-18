package publishing

import (
	"context"
	"os"
	"testing"
)

func TestBroadcastSlotMigrationKeepsActiveChannelAndAdminChanges(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `DELETE FROM youtube_publish_slots;
	 INSERT INTO youtube_publish_slots(channel,weekday,publish_time,timezone) VALUES
	 ('youtube',1,'10:05','America/Chicago'),
	 ('btcpp-youtube',2,'14:00','America/New_York');
	 UPDATE publishing_channels SET timezone='America/Chicago',
	 slots='[{"day":1,"time":"10:05"},{"day":2,"time":"14:00"}]',
	 updated_at='2000-01-01' WHERE id='broadcasts'`)
	if err != nil {
		t.Fatal(err)
	}
	query, err := os.ReadFile("../../db/migrations/164_publishing_broadcast_slot_channel.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, string(query)); err != nil {
		t.Fatal(err)
	}
	var correct bool
	err = tx.QueryRow(ctx, `SELECT timezone='America/New_York' AND slots='[{"day":2,"time":"14:00"}]'::jsonb FROM publishing_channels WHERE id='broadcasts'`).Scan(&correct)
	if err != nil || !correct {
		t.Fatalf("active channel cadence was not retained: %v", err)
	}
	// Even an edit that happens to match the original mixed cadence is preserved.
	_, err = tx.Exec(ctx, `UPDATE publishing_channels SET timezone='America/Chicago',
	 slots='[{"day":1,"time":"10:05"},{"day":2,"time":"14:00"}]',
	 updated_at=now()+interval '1 minute' WHERE id='broadcasts'`)
	if err != nil {
		t.Fatal(err)
	}
	tag, err := tx.Exec(ctx, string(query))
	if err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("administrator's slots must not change: %v", err)
	}
}
