package getters

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestDatabaseSmokeSocialPostClaimsAndConcurrentUpserts(t *testing.T) {
	ctx := databaseSmokeContext(t)
	ref := "recording:claim-smoke-" + databaseSmokeSuffix() + ":youtube"
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM social_posts WHERE ref = $1`, ref)
	})

	status := "uploading"
	claim, claimed, err := ClaimSocialPost(ctx, SocialPostUpdate{
		Ref:      ref,
		PostedTo: "youtube",
		Kind:     SocialPostKindRecording,
		Status:   &status,
	}, time.Minute)
	if err != nil || !claimed {
		t.Fatalf("first claim = (%+v, %t, %v), want acquired", claim, claimed, err)
	}
	if _, claimed, err := ClaimSocialPost(ctx, SocialPostUpdate{Ref: ref, Status: &status}, time.Minute); err != nil || claimed {
		t.Fatalf("competing claim = (%t, %v), want unavailable", claimed, err)
	}
	wrongClaim := &SocialPostClaim{
		Ref: ref, Token: "00000000-0000-0000-0000-000000000000",
	}
	textWhileClaimed := "unclaimed write"
	if _, err := UpsertSocialPost(ctx, SocialPostUpdate{Ref: ref, Text: &textWhileClaimed}); err == nil {
		t.Fatal("ordinary upsert changed an actively claimed social post")
	}
	if _, err := UpdateClaimedSocialPost(ctx, wrongClaim, SocialPostUpdate{Ref: ref, Text: &textWhileClaimed}); err == nil {
		t.Fatal("claimed update with the wrong token succeeded")
	}
	claimedText := "claimed write"
	if _, err := UpdateClaimedSocialPost(ctx, claim, SocialPostUpdate{Ref: ref, Text: &claimedText}); err != nil {
		t.Fatalf("update with claim: %s", err)
	}
	if err := RenewSocialPostClaim(ctx, wrongClaim, time.Minute); err == nil {
		t.Fatal("renew with the wrong token succeeded")
	}
	if err := RenewSocialPostClaim(ctx, claim, time.Minute); err != nil {
		t.Fatalf("renew claim: %s", err)
	}
	if err := ReleaseSocialPostClaim(ctx, wrongClaim); err == nil {
		t.Fatal("release with the wrong token succeeded")
	}
	if err := ReleaseSocialPostClaim(ctx, claim); err != nil {
		t.Fatalf("release first claim: %s", err)
	}
	claim, claimed, err = ClaimSocialPost(ctx, SocialPostUpdate{Ref: ref, Status: &status}, time.Minute)
	if err != nil || !claimed {
		t.Fatalf("claim after release = (%+v, %t, %v), want acquired", claim, claimed, err)
	}
	if err := ReleaseSocialPostClaim(ctx, claim); err != nil {
		t.Fatalf("release second claim: %s", err)
	}

	uploaded := "uploaded"
	url := "https://example.test/video"
	message := "temporary error"
	if _, err := UpsertSocialPost(ctx, SocialPostUpdate{
		Ref: ref, Status: &uploaded, URL: &url, Error: &message,
	}); err != nil {
		t.Fatalf("upsert completed publication: %s", err)
	}
	updatedText := "updated copy"
	post, err := UpsertSocialPost(ctx, SocialPostUpdate{Ref: ref, Text: &updatedText})
	if err != nil {
		t.Fatalf("upsert partial social post: %s", err)
	}
	if post.Status != uploaded || post.URL != url || post.Error != message {
		t.Fatalf("partial upsert lost state: %+v", post)
	}
	clear := ""
	post, err = UpsertSocialPost(ctx, SocialPostUpdate{Ref: ref, Error: &clear})
	if err != nil {
		t.Fatalf("clear social post error: %s", err)
	}
	if post.Error != "" {
		t.Fatalf("social post error = %q, want cleared", post.Error)
	}

	const workers = 16
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			text := fmt.Sprintf("copy %d", i)
			_, err := UpsertSocialPost(ctx, SocialPostUpdate{
				Ref: ref, Text: &text, PostedTo: "youtube", Kind: SocialPostKindRecording,
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent upsert: %s", err)
		}
	}

	var count int
	if err := ctx.DB.QueryRow(context.Background(), `SELECT count(*) FROM social_posts WHERE ref = $1`, ref).Scan(&count); err != nil {
		t.Fatalf("count social posts: %s", err)
	}
	if count != 1 {
		t.Fatalf("social post rows = %d, want 1", count)
	}
}
