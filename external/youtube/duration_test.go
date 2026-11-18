package youtube

import "testing"

func TestVideoDurationSeconds(t *testing.T) {
	for raw, want := range map[string]int64{
		"PT45M": 2700, "PT1H2M3S": 3723, "P1DT2H": 93600,
		"PT1.25S": 2, "PT0S": 0, "": 0, "nonsense": 0,
		"PT-5S": 0, "P999999999999D": 0,
	} {
		if got := videoDurationSeconds(raw); got != want {
			t.Errorf("%q = %d, want %d", raw, got, want)
		}
	}
}
