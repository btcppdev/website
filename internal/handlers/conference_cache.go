package handlers

import (
	"sync"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

const conferenceCacheTTL = 5 * time.Minute

var conferenceCache = struct {
	sync.RWMutex
	app        *config.AppContext
	confs      []*types.Conf
	expires    time.Time
	refreshing bool
	generation uint64
}{}

// cachedConfs keeps the small, frequently rendered public conference list out
// of the request path. Once populated, an expired snapshot is returned
// immediately while one goroutine refreshes it; database trouble therefore
// cannot turn navigation or the 404 page into additional pool pressure.
func cachedConfs(ctx *config.AppContext) ([]*types.Conf, error) {
	now := time.Now()
	conferenceCache.RLock()
	sameApp := conferenceCache.app == ctx
	confs := conferenceCache.confs
	expires := conferenceCache.expires
	conferenceCache.RUnlock()

	if sameApp && len(confs) > 0 {
		if !now.Before(expires) {
			startConferenceCacheRefresh(ctx)
		}
		return cloneConfs(confs), nil
	}

	return refreshConferenceCache(ctx)
}

func startConferenceCacheRefresh(ctx *config.AppContext) {
	conferenceCache.Lock()
	if conferenceCache.app != ctx || conferenceCache.refreshing {
		conferenceCache.Unlock()
		return
	}
	conferenceCache.refreshing = true
	generation := conferenceCache.generation
	conferenceCache.Unlock()

	go func() {
		confs, err := getters.ListConfs(ctx)
		conferenceCache.Lock()
		defer conferenceCache.Unlock()
		if conferenceCache.app != ctx || conferenceCache.generation != generation {
			return
		}
		conferenceCache.refreshing = false
		if err != nil {
			if ctx.Err != nil {
				ctx.Err.Printf("conference cache refresh failed; serving stale data: %s", err)
			}
			return
		}
		conferenceCache.confs = cloneConfs(confs)
		conferenceCache.expires = time.Now().Add(conferenceCacheTTL)
	}()
}

func refreshConferenceCache(ctx *config.AppContext) ([]*types.Conf, error) {
	confs, err := getters.ListConfs(ctx)
	if err != nil {
		return nil, err
	}
	conferenceCache.Lock()
	conferenceCache.generation++
	conferenceCache.app = ctx
	conferenceCache.confs = cloneConfs(confs)
	conferenceCache.expires = time.Now().Add(conferenceCacheTTL)
	conferenceCache.refreshing = false
	conferenceCache.Unlock()
	return cloneConfs(confs), nil
}

// invalidateConferenceCache makes an administrative conference update visible
// on the very next render instead of waiting for the public-cache TTL.
func invalidateConferenceCache(ctx *config.AppContext) {
	conferenceCache.Lock()
	defer conferenceCache.Unlock()
	if conferenceCache.app == ctx {
		conferenceCache.generation++
		conferenceCache.confs = nil
		conferenceCache.expires = time.Time{}
		conferenceCache.refreshing = false
	}
}

func cloneConfs(confs []*types.Conf) []*types.Conf {
	out := make([]*types.Conf, 0, len(confs))
	for _, conf := range confs {
		if conf == nil {
			out = append(out, nil)
			continue
		}
		copy := *conf
		copy.Tickets = append([]*types.ConfTicket(nil), conf.Tickets...)
		out = append(out, &copy)
	}
	return out
}
