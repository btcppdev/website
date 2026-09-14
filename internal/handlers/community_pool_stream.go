package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/prizepool"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// One LISTEN connection per database/application instance, only while viewers
// are connected. It uses a dedicated connection rather than occupying a query
// pool slot for the lifetime of a stream.
var communityStreams = struct {
	sync.Mutex
	hubs map[*pgxpool.Pool]*communityStreamHub
}{hubs: make(map[*pgxpool.Pool]*communityStreamHub)}

type communityStreamHub struct {
	mu         sync.Mutex
	viewers    map[chan struct{}]struct{}
	generation uint64
	snapshots  map[string]string
	flights    singleflight.Group
	cancel     context.CancelFunc
	ready      chan struct{}
	readyOnce  sync.Once
	connected  bool
}

func subscribeCommunityResults(app *config.AppContext) (*communityStreamHub, <-chan struct{}, func()) {
	communityStreams.Lock()
	h := communityStreams.hubs[app.DB]
	if h == nil {
		ctx, cancel := context.WithCancel(context.Background())
		h = &communityStreamHub{viewers: make(map[chan struct{}]struct{}), snapshots: make(map[string]string), cancel: cancel, ready: make(chan struct{})}
		communityStreams.hubs[app.DB] = h
		go h.listen(ctx, app)
	}
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.viewers[ch] = struct{}{}
	h.mu.Unlock()
	communityStreams.Unlock()
	var once sync.Once
	return h, ch, func() {
		once.Do(func() {
			communityStreams.Lock()
			defer communityStreams.Unlock()
			h.mu.Lock()
			delete(h.viewers, ch)
			empty := len(h.viewers) == 0
			h.mu.Unlock()
			if empty {
				h.cancel()
				delete(communityStreams.hubs, app.DB)
			}
		})
	}
}

func (h *communityStreamHub) invalidate() {
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

func (h *communityStreamHub) listen(ctx context.Context, app *config.AppContext) {
	for ctx.Err() == nil {
		connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		conn, err := pgx.ConnectConfig(connectCtx, app.DB.Config().ConnConfig.Copy())
		cancel()
		if err == nil {
			queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, err = conn.Exec(queryCtx, "LISTEN btcpp_community_pools")
			cancel()
			if err == nil {
				h.mu.Lock()
				h.connected = true
				h.mu.Unlock()
				h.readyOnce.Do(func() { close(h.ready) })
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
		h.mu.Lock()
		h.connected = false
		h.mu.Unlock()
		h.invalidate()
		if ctx.Err() != nil {
			return
		}
		if app.Err != nil {
			app.Err.Printf("community results listener disconnected: %v", err)
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

func (h *communityStreamHub) snapshot(key string, load func() (string, error)) (string, error) {
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

var errCommunityUnavailable = errors.New("community pool is not public")

// Public totals only: never includes receipt hashes, descriptions or payer notes.
func communityStatusData(p *prizepool.Pool) map[string]any {
	var synced int64
	if p.SyncedAt != nil {
		synced = p.SyncedAt.Unix()
	}
	return map[string]any{"sats": p.Sats(), "goal": p.GoalLabel(), "progress": p.Progress(), "milestone": p.Milestone(), "count": p.Count, "stale": p.Stale(), "status": p.Status, "syncedAt": synced, "serverTime": time.Now().Unix()}
}
func communityPoolStream(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "no-store, no-transform")
	if app.DB == nil {
		http.Error(w, "Funding updates unavailable", 503)
		return
	}
	// Check public visibility before allocating a listener for this request.
	conf, err := getters.GetConfByTag(app, mux.Vars(r)["conf"])
	if err != nil {
		http.Error(w, "Funding updates unavailable", 503)
		return
	}
	if conf == nil || !conf.IsPublished() {
		http.NotFound(w, r)
		return
	}
	hub, changes, unsubscribe := subscribeCommunityResults(app)
	defer unsubscribe()
	ready := time.NewTimer(10 * time.Second)
	defer ready.Stop()
	select {
	case <-hub.ready:
	case <-ready.C:
		http.Error(w, "Funding updates unavailable", 503)
		return
	case <-r.Context().Done():
		return
	}
	load := func() (string, error) {
		hub.mu.Lock()
		connected := hub.connected
		hub.mu.Unlock()
		if !connected {
			return "", errors.New("notification listener unavailable")
		}
		return hub.snapshot(conf.Tag, func() (string, error) {
			current, err := getters.GetConfByTag(app, conf.Tag)
			if err != nil {
				return "", err
			}
			if current == nil || !current.IsPublished() {
				return "", errCommunityUnavailable
			}
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			p, err := prizepool.Load(ctx, app.DB, conf.Ref)
			if errors.Is(err, pgx.ErrNoRows) {
				return "", errCommunityUnavailable
			}
			if err != nil {
				return "", err
			}
			if p.Status == "draft" {
				return "", errCommunityUnavailable
			}
			data, err := json.Marshal(communityStatusData(p))
			return string(data), err
		})
	}
	serveCommunityStream(w, r, changes, load)
}

func serveCommunityStream(w http.ResponseWriter, r *http.Request, changes <-chan struct{}, load func() (string, error)) {
	w.Header().Set("Cache-Control", "no-store, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	streaming := false
	send := func(event, data string) error {
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return err
		}
		return controller.Flush()
	}
	last := ""
	update := func() bool {
		data, err := load()
		if err != nil {
			if streaming {
				event := "unavailable"
				if errors.Is(err, errCommunityUnavailable) {
					event = "revoked"
				}
				_ = send(event, "{}")
			} else {
				status := 503
				if errors.Is(err, errCommunityUnavailable) {
					status = 404
				}
				http.Error(w, "Funding updates unavailable", status)
			}
			return false
		}
		if !streaming {
			w.Header().Set("Content-Type", "text/event-stream")
			streaming = true
		}
		if data != last {
			// Add send time outside the shared cache so reconnects preserve accurate staleness.
			var payload map[string]any
			if err := json.Unmarshal([]byte(data), &payload); err != nil {
				return false
			}
			payload["serverTime"] = time.Now().Unix()
			encoded, err := json.Marshal(payload)
			if err != nil {
				return false
			}
			if err := send("funding", string(encoded)); err != nil {
				return false
			}
			last = data
		}
		return true
	}
	if !update() {
		return
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	// Renew before the normal server write timeout and refresh public visibility.
	renewal := time.NewTimer(60 * time.Second)
	defer renewal.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-changes:
			if !update() {
				return
			}
		case <-heartbeat.C:
			if err := send("heartbeat", "{}"); err != nil {
				return
			}
		case <-renewal.C:
			_ = send("reconnect", "{}")
			return
		}
	}
}
