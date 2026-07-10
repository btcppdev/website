package handlers

import (
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestShowTicketPriceIncreaseDate(t *testing.T) {
	start := time.Date(2026, 6, 17, 9, 0, 0, 0, time.UTC)
	conf := &types.Conf{StartDate: start}

	tests := []struct {
		name string
		conf *types.Conf
		tix  *types.ConfTicket
		want bool
	}{
		{
			name: "before conference start",
			conf: conf,
			tix:  &types.ConfTicket{Expires: &types.Times{Start: start.AddDate(0, 0, -1)}},
			want: true,
		},
		{
			name: "same as conference start",
			conf: conf,
			tix:  &types.ConfTicket{Expires: &types.Times{Start: start}},
			want: false,
		},
		{
			name: "after conference start",
			conf: conf,
			tix:  &types.ConfTicket{Expires: &types.Times{Start: start.AddDate(0, 0, 44)}},
			want: false,
		},
		{name: "nil conf", conf: nil, tix: &types.ConfTicket{Expires: &types.Times{Start: start}}, want: false},
		{name: "nil ticket", conf: conf, tix: nil, want: false},
		{name: "nil expires", conf: conf, tix: &types.ConfTicket{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := showTicketPriceIncreaseDate(tt.conf, tt.tix); got != tt.want {
				t.Fatalf("showTicketPriceIncreaseDate() = %v, want %v", got, tt.want)
			}
		})
	}
}
