package mtypes

import "testing"

func TestAddSublistAddsEveryChoice(t *testing.T) {
	var sub Subscriber
	if !sub.AddSublist([]string{"newsletter", "test-news", "newsletter"}) || len(sub.Subs) != 2 {
		t.Fatalf("did not retain every distinct choice: %+v", sub.Subs)
	}
	if sub.AddSublist([]string{"newsletter", "test-news"}) {
		t.Fatal("unchanged choices reported a change")
	}
}
