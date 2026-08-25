package xstudio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreateUploadFinalizeMatchesCapturedWorkflow(t *testing.T) {
	t.Parallel()
	const sessionID = "11111111-2222-4333-8444-555555555555"
	startsAt := time.Date(2026, 8, 26, 18, 30, 0, 0, time.UTC)
	var mu sync.Mutex
	steps := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if request.Header.Get("Cookie") != "auth=fake" || request.Header.Get("X-Session-ID") != sessionID {
			t.Errorf("credentials/session headers were not forwarded")
		}
		if request.URL.RawQuery != "rwebShell=1" {
			t.Errorf("query = %q", request.URL.RawQuery)
		}
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/live/create-scheduled-broadcast":
			steps = append(steps, "create")
			var payload map[string]any
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if payload["ingestId"] != "fake-ingest" || payload["title"] != "Test broadcast" || payload["startTimeUtc"] != float64(startsAt.UnixMilli()) {
				t.Errorf("create payload = %#v", payload)
			}
			fmt.Fprint(response, `{"value":{"scheduledBroadcastId":"scheduled-1","broadcastId":"broadcast-1"},"error":null}`)
		case "/api/live/upload-poster-image":
			steps = append(steps, "poster")
			if err := request.ParseMultipartForm(MaxPosterSize + 1024); err != nil {
				t.Error(err)
				return
			}
			files := request.MultipartForm.File["file"]
			if len(files) != 1 || len(request.MultipartForm.File) != 1 || len(request.MultipartForm.Value) != 0 {
				t.Errorf("multipart fields = %#v %#v", request.MultipartForm.File, request.MultipartForm.Value)
				return
			}
			file, err := files[0].Open()
			if err != nil {
				t.Error(err)
				return
			}
			defer file.Close()
			contents, _ := io.ReadAll(file)
			if string(contents) != "fake-poster" {
				t.Errorf("poster = %q", contents)
			}
			fmt.Fprint(response, `{"value":"poster-1","error":null}`)
		case "/api/live/update-scheduled-broadcast":
			steps = append(steps, "finalize")
			var payload map[string]any
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if payload["broadcastId"] != "broadcast-1" || payload["scheduledBroadcastId"] != "scheduled-1" || payload["preLiveSlateMediaId"] != "poster-1" {
				t.Errorf("finalize payload = %#v", payload)
			}
			response.Header().Set("X-Rate-Limit-Limit", "300")
			response.Header().Set("X-Rate-Limit-Remaining", "287")
			fmt.Fprint(response, `{"value":{"scheduledBroadcastId":"scheduled-1","broadcastId":"broadcast-1","preLiveSlateUrl":"https://pbs.twimg.com/media/fake.jpg","chatOption":2},"error":null}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client, err := New(Config{Cookie: "auth=fake", UserAgent: "test", IngestID: "fake-ingest", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.Create(context.Background(), CreateInput{
		Title: "Test broadcast", StartsAt: startsAt, SessionID: sessionID,
		OptimisticPosterURL: "blob:https://studio.x.com/fake", HighLatency: true, ChatOption: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	uploaded, err := client.UploadPoster(context.Background(), sessionID, "poster.png", "image/png", strings.NewReader("fake-poster"))
	if err != nil {
		t.Fatal(err)
	}
	finalized, err := client.Finalize(context.Background(), FinalizeInput{
		ScheduledBroadcastID: created.ScheduledBroadcastID, BroadcastID: created.BroadcastID,
		PosterMediaID: uploaded.MediaID, StartsAt: startsAt, SessionID: sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(steps, ",") != "create,poster,finalize" || finalized.PosterURL == "" || finalized.RateLimit.Remaining != 287 {
		t.Fatalf("steps=%v finalized=%+v", steps, finalized)
	}
}

func TestHTTPErrorRedactsRuntimeSecrets(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, `{"message":"auth=fake failed for fake-ingest"}`, http.StatusForbidden)
	}))
	defer server.Close()
	client, err := New(Config{Cookie: "auth=fake", IngestID: "fake-ingest", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Create(context.Background(), CreateInput{Title: "Test", StartsAt: time.Now().Add(time.Hour), SessionID: "session"})
	if err == nil || strings.Contains(err.Error(), "auth=fake") || strings.Contains(err.Error(), "fake-ingest") {
		t.Fatalf("error did not redact runtime credentials: %v", err)
	}
}
