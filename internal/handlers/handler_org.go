package handlers

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func sessionDay(key string) (int, error) {
	/* Some keys are "3H" others are "2A+"
	 * We want to preserve the ability to do just '10E'
	 */
	var index string
	for _, c := range key {
		if !unicode.IsDigit(c) {
			break
		}
		index += string(c)
	}

	return strconv.Atoi(index)
}

func isLegacySessionKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	last := key[len(key)-1]
	if last != '+' && last != '=' && last != '-' {
		return false
	}
	_, err := sessionDay(key)
	return err == nil
}

func talkSectionKey(conf *types.Conf, talk *types.Talk) string {
	if talk == nil {
		return ""
	}
	if isLegacySessionKey(talk.Section) {
		return strings.TrimSpace(talk.Section)
	}
	if talk.Sched == nil {
		return ""
	}
	loc := time.UTC
	if conf != nil {
		loc = conf.Loc()
	}
	start := talk.Sched.Start.In(loc)
	confStart := start
	if conf != nil && !conf.StartDate.IsZero() {
		confStart = conf.StartDate.In(loc)
	}
	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	confDay := time.Date(confStart.Year(), confStart.Month(), confStart.Day(), 0, 0, 0, 0, loc)
	day := int(startDay.Sub(confDay).Hours()/24) + 1
	if day < 1 {
		day = 1
	}
	marker := "+"
	if start.Hour() >= 17 {
		marker = "-"
	} else if start.Hour() >= 12 {
		marker = "="
	}
	return fmt.Sprintf("%d%s", day, marker)
}

func filterSessions(days []*Day, dayref, venue string) ([]*types.Session, error) {
	seshList, err := pickSessions(days, dayref)
	if err != nil {
		return nil, err
	}

	s := make([]*types.Session, 0)
	for _, sessions := range seshList {
		for _, sesh := range sessions {
			if sesh.Venue != venue {
				continue
			}
			s = append(s, sesh)
		}
	}
	return s, nil
}

func pickSessions(days []*Day, dayref string) ([]types.SessionTime, error) {
	i, err := sessionDay(dayref)
	if err != nil {
		return nil, err
	}
	if i > len(days) || i < 1 {
		return nil, fmt.Errorf("Index out of range %d of %d", i, len(days))
	}

	day := days[i-1]
	switch string(dayref[len(dayref)-1]) {
	case "+":
		return day.Morning, nil
	case "=":
		return day.Afternoon, nil
	case "-":
		return day.Evening, nil
	}

	return nil, fmt.Errorf("Unknown day time marker %s", dayref)
}

func talkDays(ctx *config.AppContext, conf *types.Conf, talks types.TalkTime) ([]*Day, error) {
	buckets, err := bucketTalks(ctx, conf, talks)
	if err != nil {
		return nil, err
	}
	/* Sort keys alphabetically */
	keys := make([]string, 0)
	for k, _ := range buckets {
		if k == "" {
			continue
		}
		keys = append(keys, k)
	}
	// FIXME: double digit days?
	sort.Strings(keys)

	if len(keys) == 0 {
		return nil, nil
	}

	/* populate days */
	lastKey := keys[len(keys)-1]
	maxDays, err := sessionDay(lastKey)
	if err != nil {
		return nil, err
	}

	days := make([]*Day, maxDays)
	for i := 0; i < maxDays; i++ {
		days[i] = &Day{
			Morning:   make([]types.SessionTime, 0),
			Afternoon: make([]types.SessionTime, 0),
			Evening:   make([]types.SessionTime, 0),
			Idx:       i + 1,
		}
	}

	for _, k := range keys {
		v, _ := buckets[k]
		i, err := sessionDay(k)
		if err != nil {
			return nil, err
		}

		day := days[i-1]
		switch string(k[len(k)-1]) {
		case "+":
			day.Morning = append(day.Morning, v)
		case "=":
			day.Afternoon = append(day.Afternoon, v)
		case "-":
			day.Evening = append(day.Evening, v)
		}

	}

	return days, nil
}

func talkToSession(ctx *config.AppContext, talk *types.Talk, conf *types.Conf) *types.Session {
	sesh := &types.Session{
		Name:          talk.Name,
		Description:   talk.Description,
		Speakers:      talk.Speakers,
		TalkPhoto:     talk.Clipart,
		Sched:         talk.Sched,
		Type:          talk.Type,
		Venue:         talk.Venue,
		AnchorTag:     talk.AnchorTag(),
		ConfTag:       conf.Tag,
		GithubRepoURL: talk.GithubRepoURL,
		SlidesURL:     talk.SlidesURL,
	}

	if talk.Sched != nil {
		sesh.Len = talk.Sched.LenStr()
		sesh.StartTime = talk.Sched.StartTime()
	}

	// First try a direct ConfTalk.ID lookup (cheap when talks come from
	// LoadTalksFromConfTalks); fall back to the (tag, title) bridge for
	// the legacy Talks-DB renderer where talk.ID is a Talks-DB page ID.
	if rec, err := getters.GetRecordingByConfTalk(ctx, talk.ID); err != nil {
		ctx.Err.Printf("talkToSession recording lookup %s: %s", talk.ID, err)
	} else if rec != nil {
		sesh.YTLink = rec.YTLink
	}

	return sesh
}

func bucketTalks(ctx *config.AppContext, conf *types.Conf, talks types.TalkTime) (map[string]types.SessionTime, error) {
	sort.Sort(talks)

	sessions := make(map[string]types.SessionTime)
	for _, talk := range talks {
		key := talkSectionKey(conf, talk)
		if key == "" {
			continue
		}
		session := talkToSession(ctx, talk, conf)
		section, ok := sessions[key]
		if !ok {
			section = make(types.SessionTime, 0)
		}
		section = append(section, session)
		sessions[key] = section
	}
	return sessions, nil
}
