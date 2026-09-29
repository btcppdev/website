package mtypes

import "testing"

func TestAddSublistAddsEveryMembership(t *testing.T) {
	sub := &Subscriber{}
	if !sub.AddSublist([]string{"nairobi", "genpop", "nairobi-genpop", "newsletter"}) || len(sub.Subs) != 4 {
		t.Fatalf("memberships = %+v", sub.Subs)
	}
	if sub.AddSublist([]string{"nairobi", "newsletter"}) {
		t.Fatal("duplicates reported as additions")
	}
	if !sub.AddSublist([]string{"nairobi", "speaker", "nairobi-speaker"}) || len(sub.Subs) != 6 {
		t.Fatal("existing first membership prevented later additions")
	}
}
