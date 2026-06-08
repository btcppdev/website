package handlers

import (
	"reflect"
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestFullSpeakerAvailabilityUsesConferenceDays(t *testing.T) {
	conf := &types.Conf{
		StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
	}

	got := fullSpeakerAvailability(conf)
	want := []string{"07/01/2026", "07/02/2026", "07/03/2026"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fullSpeakerAvailability = %v, want %v", got, want)
	}
}
