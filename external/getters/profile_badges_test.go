package getters

import (
	"testing"

	"btcpp-web/internal/types"
)

func TestReplacePersonBadgePresentations(t *testing.T) {
	ctx := postgresSmokeContext(t)
	personID := insertSmokePerson(t, ctx, "badge-presentation")

	initial := []*types.PersonBadgePresentation{
		{BadgeRef: "btcpp-grant:one", FeaturedPosition: 1},
		{BadgeRef: "btcpp-grant:two", Hidden: true},
		{BadgeRef: "nostr-award:three", FeaturedPosition: 2},
	}
	if err := ReplacePersonBadgePresentations(ctx, personID, initial); err != nil {
		t.Fatalf("ReplacePersonBadgePresentations initial: %v", err)
	}

	got, err := ListPersonBadgePresentations(ctx, personID)
	if err != nil {
		t.Fatalf("ListPersonBadgePresentations initial: %v", err)
	}
	if len(got) != 3 || got[0].BadgeRef != "btcpp-grant:one" || got[0].FeaturedPosition != 1 || got[0].Hidden || got[1].BadgeRef != "nostr-award:three" || got[1].FeaturedPosition != 2 || got[2].BadgeRef != "btcpp-grant:two" || !got[2].Hidden {
		t.Fatalf("initial presentations=%+v", got)
	}

	replacement := []*types.PersonBadgePresentation{
		{BadgeRef: "btcpp-grant:two", FeaturedPosition: 1},
		{BadgeRef: "btcpp-grant:one"},
	}
	if err := ReplacePersonBadgePresentations(ctx, personID, replacement); err != nil {
		t.Fatalf("ReplacePersonBadgePresentations replacement: %v", err)
	}
	got, err = ListPersonBadgePresentations(ctx, personID)
	if err != nil {
		t.Fatalf("ListPersonBadgePresentations replacement: %v", err)
	}
	if len(got) != 2 || got[0].BadgeRef != "btcpp-grant:two" || got[0].FeaturedPosition != 1 || got[1].BadgeRef != "btcpp-grant:one" || got[1].FeaturedPosition != 0 || got[1].Hidden {
		t.Fatalf("replacement presentations=%+v", got)
	}
}

func TestReplacePersonBadgePresentationsRejectsInvalidSnapshots(t *testing.T) {
	ctx := postgresSmokeContext(t)
	personID := insertSmokePerson(t, ctx, "invalid-badge-presentation")

	tests := []struct {
		name  string
		items []*types.PersonBadgePresentation
	}{
		{name: "duplicate reference", items: []*types.PersonBadgePresentation{{BadgeRef: "same"}, {BadgeRef: "same"}}},
		{name: "duplicate position", items: []*types.PersonBadgePresentation{{BadgeRef: "one", FeaturedPosition: 1}, {BadgeRef: "two", FeaturedPosition: 1}}},
		{name: "hidden featured", items: []*types.PersonBadgePresentation{{BadgeRef: "one", Hidden: true, FeaturedPosition: 1}}},
		{name: "seventh position", items: []*types.PersonBadgePresentation{{BadgeRef: "one", FeaturedPosition: 7}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ReplacePersonBadgePresentations(ctx, personID, test.items); err == nil {
				t.Fatal("expected invalid snapshot error")
			}
		})
	}
}
