package types

import "testing"

func TestOrganizationShopSplit(t *testing.T) {
	for _, tc := range []struct{ amount, fee, supplier uint }{
		{0, 0, 0}, {1, 0, 1}, {10, 2, 8}, {100, 15, 85},
		{4000, 600, 3400}, {2250, 338, 1912}, {9999, 1500, 8499},
		{100000000, 15000000, 85000000},
	} {
		fee, supplier := OrganizationShopSplit(tc.amount)
		if fee != tc.fee || supplier != tc.supplier || fee+supplier != tc.amount {
			t.Fatalf("split %d: fee=%d supplier=%d", tc.amount, fee, supplier)
		}
	}
}
