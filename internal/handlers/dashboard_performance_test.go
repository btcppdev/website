package handlers

import (
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestDashboardCachedWhoIsPublicIDUsesSnapshotWithoutRefresh(t *testing.T) {
	ctx := &config.AppContext{}
	speaker := &types.Speaker{ID: "person-id"}

	whoIsCache.Lock()
	whoIsCache.app = ctx
	whoIsCache.people = []*WhoIsPerson{}
	whoIsCache.publicIDs = map[string]string{speaker.ID: "mara-chen"}
	whoIsCache.expires = time.Now().Add(time.Minute)
	whoIsCache.refreshing = false
	whoIsCache.Unlock()
	t.Cleanup(func() {
		whoIsCache.Lock()
		whoIsCache.app = nil
		whoIsCache.people = nil
		whoIsCache.publicIDs = nil
		whoIsCache.expires = time.Time{}
		whoIsCache.refreshing = false
		whoIsCache.Unlock()
	})

	publicID, ok := dashboardCachedWhoIsPublicID(ctx, speaker)
	if !ok || publicID != "mara-chen" {
		t.Fatalf("dashboardCachedWhoIsPublicID() = %q, %t", publicID, ok)
	}
	whoIsCache.Lock()
	refreshing := whoIsCache.refreshing
	whoIsCache.Unlock()
	if refreshing {
		t.Fatal("fresh dashboard profile snapshot unexpectedly triggered a refresh")
	}
}
