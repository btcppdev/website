package getters

import (
	"context"
	"sort"
	"strings"
	"testing"
)

func TestDatabaseSmokeDiscountScopedToMultipleConferences(t *testing.T) {
	ctx := databaseSmokeContext(t)
	firstConfID, _ := insertSmokeConference(t, ctx)
	secondConfID, _ := insertSmokeConference(t, ctx)
	code := "MULTISMOKE" + strings.ToUpper(databaseSmokeSuffix())

	discountID, err := CreateDiscount(ctx, DiscountInput{
		CodeName:     code,
		DiscountExpr: "%50",
		ConfRefs:     []string{firstConfID, secondConfID, firstConfID},
	})
	if err != nil {
		t.Fatalf("CreateDiscount multi-conference: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM discounts WHERE id::text = $1 OR code_name = $2`, discountID, code)
	})

	found, err := GetDiscountByCode(ctx, strings.ToLower(code))
	if err != nil {
		t.Fatalf("GetDiscountByCode: %v", err)
	}
	if found == nil {
		t.Fatal("multi-conference discount was not found")
	}
	sort.Strings(found.ConfRef)
	wantRefs := []string{firstConfID, secondConfID}
	sort.Strings(wantRefs)
	if strings.Join(found.ConfRef, ",") != strings.Join(wantRefs, ",") {
		t.Fatalf("conference refs = %v, want %v", found.ConfRef, wantRefs)
	}
	for _, confID := range wantRefs {
		total, discount, err := CalcDiscount(ctx, confID, code, 100, 1)
		if err != nil {
			t.Fatalf("CalcDiscount for %s: %v", confID, err)
		}
		if total != 50 || discount == nil || discount.Ref != discountID {
			t.Fatalf("CalcDiscount for %s = total:%d discount:%+v", confID, total, discount)
		}
	}
	if err := DeleteDiscount(ctx, discountID); err != nil {
		t.Fatalf("DeleteDiscount: %v", err)
	}
	available, err := IsCodeNameAvailable(ctx, code)
	if err != nil {
		t.Fatalf("IsCodeNameAvailable deleted code: %v", err)
	}
	if !available {
		t.Fatal("deleted code name was not available for reuse")
	}
	recreatedID, err := CreateDiscount(ctx, DiscountInput{
		CodeName:     code,
		DiscountExpr: "%40",
		ConfRefs:     []string{firstConfID},
	})
	if err != nil {
		t.Fatalf("recreate deleted discount: %v", err)
	}
	if recreatedID == discountID {
		t.Fatal("recreated discount unexpectedly reused the deleted row")
	}
}

func TestDatabaseSmokeDiscountWildcardScope(t *testing.T) {
	ctx := databaseSmokeContext(t)
	code := "WILDCARDSMOKE" + strings.ToUpper(databaseSmokeSuffix())

	discountID, err := CreateDiscount(ctx, DiscountInput{
		CodeName:       code,
		DiscountExpr:   "%25",
		AllConferences: true,
	})
	if err != nil {
		t.Fatalf("CreateDiscount wildcard: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM discounts WHERE id::text = $1 OR code_name = $2`, discountID, code)
	})

	found, err := GetDiscountByCode(ctx, code)
	if err != nil {
		t.Fatalf("GetDiscountByCode wildcard: %v", err)
	}
	if found == nil || len(found.ConfRef) != 0 {
		t.Fatalf("wildcard discount = %+v, want no conference links", found)
	}
	total, applied, err := CalcDiscount(ctx, "future-conference-id", code, 100, 1)
	if err != nil {
		t.Fatalf("CalcDiscount wildcard: %v", err)
	}
	if total != 75 || applied == nil || applied.Ref != discountID {
		t.Fatalf("CalcDiscount wildcard = total:%d discount:%+v", total, applied)
	}
}
