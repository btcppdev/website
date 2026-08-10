package spaces

import (
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestStreamingHTTPClientDoesNotTimeOutResponseBody(t *testing.T) {
	client := newStreamingHTTPClient()
	if client.Timeout != 0 {
		t.Fatalf("streaming client timeout = %s, want no whole-request timeout", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("streaming transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.ResponseHeaderTimeout != spacesRequestTimeout {
		t.Fatalf("response header timeout = %s, want %s", transport.ResponseHeaderTimeout, spacesRequestTimeout)
	}
}

func TestNewerObjectKeySelectsLatestMatchingObject(t *testing.T) {
	older := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	objects := []s3types.Object{
		{Key: aws.String("talks/older.png"), LastModified: aws.Time(older)},
		{Key: aws.String("talks/newest.avif"), LastModified: aws.Time(newer.Add(time.Hour))},
		{Key: aws.String("talks/newest.png"), LastModified: aws.Time(newer)},
	}
	var key string
	var modifiedAt time.Time
	for _, obj := range objects {
		key, modifiedAt = newerObjectKey(key, modifiedAt, obj, ".png")
	}
	if key != "talks/newest.png" || !modifiedAt.Equal(newer) {
		t.Fatalf("latest PNG = (%q, %s), want talks/newest.png at %s", key, modifiedAt, newer)
	}
}
