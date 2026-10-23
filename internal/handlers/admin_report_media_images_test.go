package handlers

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"testing"
)

func TestReportMediaSocialCardFallback(t *testing.T) {
	card := "https://btcpp.nyc3.digitaloceanspaces.com/cards/talk.png"
	media := []reportMedia{{Platform: "YouTube", ID: "abcdefghijk"}, {Platform: "X", ID: "unmatched"}}
	attachReportSocialCards(media, []reportSession{{YouTube: "https://youtu.be/abcdefghijk", SocialCard: card}})
	if got := string(media[0].ImageURL()); got != card {
		t.Fatalf("fallback = %q", got)
	}
	if media[1].ImageURL() != "" {
		t.Fatal("unmatched broadcast received another talk's card")
	}
	media[0].Thumbnail = "data:image/jpeg;base64,invalid"
	if string(media[0].ImageURL()) != card {
		t.Fatal("invalid thumbnail did not fall back")
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	thumbnail := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	media[0].Thumbnail = thumbnail
	if string(media[0].ImageURL()) != thumbnail {
		t.Fatal("valid thumbnail should take priority")
	}
	for _, unsafe := range []string{"javascript:alert(1)", "//evil.test/card.png", "http://example.test/card.png"} {
		if (reportMedia{SocialCard: unsafe}).ImageURL() != "" {
			t.Fatalf("accepted unsafe card %q", unsafe)
		}
	}
}
