package emails

import (
	"btcpp-web/internal/types"
	"testing"
	"time"
)

func TestNextOrganizationWelcomeConference(t *testing.T) {
	now := time.Now()
	next := &types.Conf{Tag: "next", PublicationStatus: "published", StartDate: now.Add(time.Hour)}
	confs := []*types.Conf{nil,
		{PublicationStatus: "published", StartDate: now.Add(-time.Hour)},
		{PublicationStatus: "draft", StartDate: now.Add(time.Minute)},
		{PublicationStatus: "published", StartDate: now.Add(24 * time.Hour)}, next,
	}
	if got := nextOrganizationWelcomeConference(confs, now); got != next {
		t.Fatalf("selected %v, want next published conference", got)
	}
	if got := nextOrganizationWelcomeConference(confs, now.Add(48*time.Hour)); got != confs[3] {
		t.Fatal("expected the most recent published conference when none are upcoming")
	}
	if got := nextOrganizationWelcomeConference([]*types.Conf{nil, {PublicationStatus: "draft", StartDate: now}, {PublicationStatus: "published"}}, now); got != nil {
		t.Fatal("selected an unpublished or undated conference")
	}
}
