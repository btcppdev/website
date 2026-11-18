package main

import (
	"btcpp-web/internal/types"
	"testing"
)

func TestSelectedConference(t *testing.T) {
	for _, test := range []struct {
		name      string
		conf      *types.Conf
		tag       string
		all, want bool
	}{
		{"nil", nil, "", true, false},
		{"draft excluded", &types.Conf{Tag: "seoul", PublicationStatus: "draft"}, "", true, false},
		{"archive included", &types.Conf{Tag: "seoul", PublicationStatus: "published", Active: false}, "", true, true},
		{"matching event", &types.Conf{Tag: "seoul", PublicationStatus: "published"}, "seoul", false, true},
		{"other event", &types.Conf{Tag: "berlin26", PublicationStatus: "published"}, "seoul", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := selectedConference(test.conf, test.tag, test.all); got != test.want {
				t.Fatalf("selected=%t, want %t", got, test.want)
			}
		})
	}
}
