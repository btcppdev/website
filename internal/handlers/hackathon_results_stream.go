package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"
)

// One LISTEN connection per database/application instance, only while viewers
// are connected. It uses a dedicated connection rather than occupying a query
// pool slot for the lifetime of a stream.
var judgingStreams = struct {
	sync.Mutex
	hubs map[*pgxpool.Pool]*judgingStreamHub
}{hubs: make(map[*pgxpool.Pool]*judgingStreamHub)}

type judgingStreamHub struct {
	mu         sync.Mutex
	viewers    map[chan struct{}]struct{}
	generation uint64
	snapshots  map[string]string
	flights    singleflight.Group
	cancel     context.CancelFunc
}

func subscribeJudgingResults(app *config.AppContext) (*judgingStreamHub, <-chan struct{}, func()) {
	judgingStreams.Lock()
	h := judgingStreams.hubs[app.DB]
	if h == nil {
		ctx, cancel := context.WithCancel(context.Background())
		h = &judgingStreamHub{viewers: make(map[chan struct{}]struct{}), snapshots: make(map[string]string), cancel: cancel}
		judgingStreams.hubs[app.DB] = h
		go h.listen(ctx, app)
	}
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.viewers[ch] = struct{}{}
	h.mu.Unlock()
	judgingStreams.Unlock()
	var once sync.Once
	return h, ch, func() {
		once.Do(func() {
			judgingStreams.Lock()
			defer judgingStreams.Unlock()
			h.mu.Lock()
			delete(h.viewers, ch)
			empty := len(h.viewers) == 0
			h.mu.Unlock()
			if empty {
				h.cancel()
				delete(judgingStreams.hubs, app.DB)
			}
		})
	}
}

func (h *judgingStreamHub) invalidate() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.generation++
	clear(h.snapshots)
	for ch := range h.viewers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (h *judgingStreamHub) listen(ctx context.Context, app *config.AppContext) {
	for ctx.Err() == nil {
		connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		conn, err := pgx.ConnectConfig(connectCtx, app.DB.Config().ConnConfig.Copy())
		cancel()
		if err == nil {
			queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, err = conn.Exec(queryCtx, "LISTEN btcpp_judging_results")
			cancel()
			if err == nil {
				// Catch changes between the initial snapshot and LISTEN, and changes missed
				// during a connection outage. No durable notification history is required.
				h.invalidate()
				for ctx.Err() == nil {
					if _, err = conn.WaitForNotification(ctx); err != nil {
						break
					}
					h.invalidate()
				}
			}
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = conn.Close(closeCtx)
			cancel()
		}
		if ctx.Err() != nil {
			return
		}
		if app.Err != nil {
			app.Err.Printf("judging results listener disconnected: %v", err)
		}
		// Force a new snapshot on reconnect; do not poll results on listener failure.
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (h *judgingStreamHub) snapshot(key string, load func() (string, error)) (string, error) {
	h.mu.Lock()
	generation := h.generation
	cached, ok := h.snapshots[key]
	h.mu.Unlock()
	if ok {
		return cached, nil
	}
	value, err, _ := h.flights.Do(strconv.FormatUint(generation, 10)+":"+key, func() (any, error) {
		// Another caller may have finished between our cache lookup and Do.
		h.mu.Lock()
		cached, ok := h.snapshots[key]
		current := h.generation
		h.mu.Unlock()
		if ok && current == generation {
			return cached, nil
		}
		html, err := load()
		if err != nil {
			return "", err
		}
		h.mu.Lock()
		if h.generation == generation {
			// Bound memory even when viewers browse many events between writes.
			if len(h.snapshots) >= 128 {
				clear(h.snapshots)
			}
			h.snapshots[key] = html
		}
		h.mu.Unlock()
		return html, nil
	})
	if err != nil {
		return "", err
	}
	return value.(string), nil
}

// Results remain restricted to closed rounds and assigned judges/staff. Each
// update rechecks access before using a shared, viewer-independent snapshot.
func HackathonJudgingLiveResults(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store, no-transform")
	if app.DB == nil {
		http.Error(w, "Results unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	eventID := strings.TrimSpace(r.URL.Query().Get("judge_event"))
	hub, changes, unsubscribe := subscribeJudgingResults(app)
	defer unsubscribe()
	streaming := false
	lastHTML := ""
	send := func(event string, payload any) error {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return err
		}
		return http.NewResponseController(w).Flush()
	}
	update := func() bool {
		// Buffer access failures so login redirects or HTML errors never corrupt an
		// already-open event stream. Re-entering the loader also refreshes identity.
		access := newJudgingAccessResponse()
		competition, conf, id, events, err := loadHackathonJudgingAccess(access, r, app)
		if err != nil {
			if streaming {
				_ = send("revoked", struct{}{})
			} else {
				http.Error(w, "Results unavailable", http.StatusForbidden)
			}
			return false
		}
		viewer := hackathonViewerFromIdentity(id, conf)
		resultEvents := judgingResultEvents(competition, events, viewer, judgeTypesForPerson(app, competition.ID, viewer.PersonID), time.Now())
		if judgeEventByID(resultEvents, eventID) == nil {
			if streaming {
				_ = send("revoked", struct{}{})
			} else {
				http.NotFound(w, r)
			}
			return false
		}
		html, err := hub.snapshot(competition.ID+":"+eventID, func() (string, error) {
			// All viewers have passed the assigned-judge/staff check above. Both can
			// see all competition projects; the results builder limits them to the round.
			projects, err := getters.ListProjectsForCompetition(app, competition.ID, types.HackathonViewer{Admin: true})
			if err != nil {
				return "", err
			}
			results, err := loadHackathonJudgingResults(app, competition, events, resultEvents, projects, eventID, time.Now())
			if err != nil {
				return "", err
			}
			var out bytes.Buffer
			err = app.TemplateCache.ExecuteTemplate(&out, "hackathon_judging_results_live", &HackathonPage{Competition: competition, Conf: conf, JudgingResults: results})
			return out.String(), err
		})
		if err != nil {
			if app.Err != nil {
				app.Err.Printf("judging results stream: %v", err)
			}
			if !streaming {
				http.Error(w, "Unable to load results", http.StatusInternalServerError)
			}
			return false
		}
		if !streaming {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("X-Accel-Buffering", "no")
			streaming = true
		}
		if html != lastHTML {
			if err := send("results", map[string]string{"html": html}); err != nil {
				return false
			}
			lastHTML = html
		}
		return true
	}
	if !update() {
		return
	}
	// Reauthenticate via a new HTTP request before the server's two-minute write
	// timeout. This also picks up revoked sessions and time-driven round changes.
	renewal := time.NewTimer(60 * time.Second)
	defer renewal.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-changes:
			if !update() {
				return
			}
		case <-renewal.C:
			_ = send("reconnect", struct{}{})
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			if err := http.NewResponseController(w).Flush(); err != nil {
				return
			}
		}
	}
}

// Access helpers can produce an error page/redirect. Only their decision is
// needed here, not that body or headers. This avoids buffering potentially large
// pages during an SSE authorization check.
type judgingAccessResponse struct{ header http.Header }

func newJudgingAccessResponse() *judgingAccessResponse {
	return &judgingAccessResponse{header: make(http.Header)}
}
func (w *judgingAccessResponse) Header() http.Header         { return w.header }
func (w *judgingAccessResponse) WriteHeader(int)             {}
func (w *judgingAccessResponse) Write(p []byte) (int, error) { return len(p), nil }

// SessionMiddleware preserves the existing buffered session behavior for normal
// requests. SSE needs a read-only session and the original streaming writer:
// scs v2.5 buffers the complete response in LoadAndSave and hides Flush.
func SessionMiddleware(app *config.AppContext, next http.Handler) http.Handler {
	normal := app.Session.LoadAndSave(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if r.Method != http.MethodGet || len(parts) != 5 || parts[1] != "hackathon" || parts[2] != "judging" || parts[3] != "results" || parts[4] != "live" {
			normal.ServeHTTP(w, r)
			return
		}
		var token string
		if cookie, err := r.Cookie(app.Session.Cookie.Name); err == nil {
			token = cookie.Value
		}
		ctx, err := app.Session.Load(r.Context(), token)
		if err != nil {
			http.Error(w, "Unable to load session", http.StatusUnauthorized)
			return
		}
		w.Header().Add("Vary", "Cookie")
		// Never commit this session after streaming: it could overwrite a newer
		// session from a concurrent browser request. Auth checks still read current
		// person/session-version state, and each renewal reloads the session store.
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
