package handlers

import (
	"strings"
	"testing"

	"btcpp-web/internal/types"
)

func TestBuildDiscountExpr(t *testing.T) {
	tests := []struct {
		name string
		form DiscountForm
		want string
	}{
		{
			name: "percent with max and date range",
			form: DiscountForm{
				CodeName:     "OSS",
				DiscountType: "percent",
				Amount:       "20",
				MaxAllowed:   "50",
				ValidFrom:    "2026-05-19",
				ExpiresAt:    "2026-05-22",
			},
			want: "%20:50@20260519-20260522",
		},
		{
			name: "dollars with expiry",
			form: DiscountForm{
				CodeName:     "TENOFF",
				DiscountType: "dollars",
				Amount:       "10",
				ExpiresAt:    "2026-05-22",
			},
			want: "$10<20260522",
		},
		{
			name: "exact price with max and date range",
			form: DiscountForm{
				CodeName:     "COMMUNITY",
				DiscountType: "fixed",
				Amount:       "25",
				MaxAllowed:   "70",
				ValidFrom:    "2026-05-19",
				ExpiresAt:    "2026-05-22",
			},
			want: "=25:70@20260519-20260522",
		},
		{
			name: "start only",
			form: DiscountForm{
				CodeName:     "LATE",
				DiscountType: "percent",
				Amount:       "15",
				ValidFrom:    "2026-05-19",
			},
			want: "%15@20260519-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildDiscountExpr(tt.form)
			if err != nil {
				t.Fatalf("buildDiscountExpr: %v", err)
			}
			if got != tt.want {
				t.Fatalf("buildDiscountExpr = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDiscountFormFromCodePreservesExactPrice(t *testing.T) {
	discount := &types.DiscountCode{DiscType: '=', Amount: 25}
	form := discountFormFromCode(discount)
	if form.DiscountType != "fixed" || form.Amount != "25" {
		t.Fatalf("exact-price form = %#v", form)
	}
}

func TestValidateGlobalDiscountConferences(t *testing.T) {
	confs := []*types.Conf{
		{Ref: "seoul-id", Tag: "seoul26", Desc: "Seoul"},
		{Ref: "berlin-id", Tag: "berlin26", Desc: "Berlin"},
	}
	refs, selected, err := validateGlobalDiscountConferences(confs, []string{"seoul-id", "berlin-id", "seoul-id"})
	if err != nil {
		t.Fatalf("validateGlobalDiscountConferences: %v", err)
	}
	if strings.Join(refs, ",") != "seoul-id,berlin-id" || conferenceNames(selected) != "Seoul and Berlin" {
		t.Fatalf("selection = refs:%v names:%q", refs, conferenceNames(selected))
	}
	if _, _, err := validateGlobalDiscountConferences(confs, nil); err == nil {
		t.Fatal("empty conference selection succeeded")
	}
	if _, _, err := validateGlobalDiscountConferences(confs, []string{"missing-id"}); err == nil {
		t.Fatal("unknown conference selection succeeded")
	}
}

func TestNormalizeGlobalDiscountScope(t *testing.T) {
	if got := normalizeGlobalDiscountScope("all"); got != "all" {
		t.Fatalf("scope = %q, want all", got)
	}
	for _, input := range []string{"", "selected", "unexpected"} {
		if got := normalizeGlobalDiscountScope(input); got != "selected" {
			t.Fatalf("scope %q = %q, want selected", input, got)
		}
	}
}

func TestBuildDiscountExprValidation(t *testing.T) {
	tests := []struct {
		name string
		form DiscountForm
	}{
		{
			name: "bad percent",
			form: DiscountForm{CodeName: "BAD", DiscountType: "percent", Amount: "101"},
		},
		{
			name: "bad date order",
			form: DiscountForm{
				CodeName:     "BAD",
				DiscountType: "dollars",
				Amount:       "10",
				ValidFrom:    "2026-05-22",
				ExpiresAt:    "2026-05-19",
			},
		},
		{
			name: "bad affiliate email",
			form: DiscountForm{
				CodeName:       "BAD",
				DiscountType:   "dollars",
				Amount:         "10",
				AffiliateEmail: "not-email",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := buildDiscountExpr(tt.form); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
