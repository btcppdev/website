package types

import (
	"testing"
	"time"
)

func TestConfActiveIsPublishedAndNotEnded(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		conf *Conf
		want bool
	}{
		{
			name: "published future",
			conf: &Conf{PublicationStatus: "published", EndDate: now.Add(time.Hour)},
			want: true,
		},
		{
			name: "published ended",
			conf: &Conf{PublicationStatus: "published", EndDate: now.Add(-time.Hour)},
			want: false,
		},
		{
			name: "draft future",
			conf: &Conf{PublicationStatus: "draft", EndDate: now.Add(time.Hour)},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.conf.IsPublished() && !tc.conf.HasEndedAt(now); got != tc.want {
				t.Fatalf("derived active = %v, want %v", got, tc.want)
			}
		})
	}
}
