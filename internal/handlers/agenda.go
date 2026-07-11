package handlers

import (
	"sort"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

const agendaPixelsPerMinute = 2.6
const agendaMinSessionHeight = 236.0

// AgendaDay is one day's slice of the rendered agenda. Talks come
// pre-sorted (Sched.Start, then Venue) and pre-bucketed against the
// day's ConfInfo break times so templates only render — no logic.
//
// Bucketing rules:
//
//   - Morning   = talks whose Start is before ConfInfo.Lunch.Start.
//   - Afternoon = talks between Lunch.Start and ConfInfo.Coffee.Start.
//   - Evening   = talks at or after Coffee.Start.
//
// When ConfInfo for the day is nil (or break times are missing),
// everything collapses into Morning + All — the template falls back
// to the chrono-only past-conf rendering.
type AgendaDay struct {
	Idx       int              // 1-based day index (Day 1 = conf.StartDate)
	Date      time.Time        // for the "Sat, Nov 15th" day header
	Active    bool             // first rendered day; may not be Idx==1 for legacy imports
	Info      *types.ConfInfo  // doors / breakfast / lunch / coffee — nil ok
	Morning   []*types.Session // before Lunch (or all-day if no Lunch info)
	Afternoon []*types.Session // between Lunch and Coffee
	Evening   []*types.Session // at/after Coffee
	All       []*types.Session // every session this day in chrono order
}

// buildAgendaDays groups the conf's talks by day and buckets them
// against the per-day ConfInfo break times. Days with no talks are
// dropped — a 4-day conf with talks only on days 1/2/4 yields 3
// AgendaDays, not 4.
//
// Status=="Scheduled" talks contribute for active/future events.
// Past events also render Accepted talks with a scheduled time, because
// older imported schedules predate the explicit Scheduled status flip.
//
// infosByDay is indexed by 1-based Day matching ConfInfo.Day; days
// without an entry leave AgendaDay.Info nil.
func buildAgendaDays(ctx *config.AppContext, conf *types.Conf, talks []*types.Talk, infosByDay map[int]*types.ConfInfo) []*AgendaDay {
	if conf == nil {
		return nil
	}
	loc := conf.Loc()
	startDate := dayStart(conf.StartDate, loc)

	byDay := make(map[int][]*types.Talk)
	for _, t := range talks {
		if !publicAgendaTalk(conf, t) {
			continue
		}
		idx := dayIndex(startDate, t.Sched.Start, loc)
		if idx < 1 {
			continue
		}
		byDay[idx] = append(byDay[idx], t)
	}

	idxs := make([]int, 0, len(byDay))
	for i := range byDay {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)

	out := make([]*AgendaDay, 0, len(idxs))
	for _, idx := range idxs {
		dayTalks := byDay[idx]
		sortTalksForAgenda(dayTalks)
		sessions := make([]*types.Session, 0, len(dayTalks))
		for _, t := range dayTalks {
			sessions = append(sessions, talkToSession(ctx, t, conf))
		}

		ad := &AgendaDay{
			Idx:    idx,
			Date:   startDate.AddDate(0, 0, idx-1),
			Active: len(out) == 0,
			Info:   infosByDay[idx],
			All:    sessions,
		}
		bucketByBreaks(ad, sessions)
		out = append(out, ad)
	}
	return out
}

// dayStart returns the midnight at the start of t in loc. We compare
// by date alone when computing day indices, so the time-of-day on
// conf.StartDate doesn't matter (some confs may store noon, some 09:00,
// etc).
func dayStart(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// dayIndex returns the 1-based day index of when within the conf:
// Day 1 = conf start date, Day N = N-1 calendar days later.
func dayIndex(confStart, when time.Time, loc *time.Location) int {
	w := dayStart(when, loc)
	return int(w.Sub(confStart).Hours()/24) + 1
}

// sortTalksForAgenda orders talks by Sched.Start, then by venue rank
// (one < two < three < four < anything-else < empty). Talks with no
// Sched have already been filtered upstream.
func sortTalksForAgenda(talks []*types.Talk) {
	sort.SliceStable(talks, func(i, j int) bool {
		ti, tj := talks[i].Sched.Start, talks[j].Sched.Start
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return venueRank(talks[i].Venue) < venueRank(talks[j].Venue)
	})
}

// venueRank gives the canonical display order. Unknown venues sort
// last (alphabetically among themselves via SliceStable on the caller).
func venueRank(v string) int {
	switch v {
	case "one":
		return 1
	case "two":
		return 2
	case "three":
		return 3
	case "four":
		return 4
	case "":
		return 1000
	default:
		return 100
	}
}

// bucketByBreaks splits sessions into Morning/Afternoon/Evening using
// the day's Lunch and Coffee start times. Missing Lunch → no
// Morning/Afternoon split (everything before Coffee is Morning).
// Missing Coffee → no Afternoon/Evening split. Both missing → all
// sessions land in Morning so the desktop bucketed view still has a
// single grid to render.
//
// Comparison is wall-clock minute-of-day, not absolute time: ConfInfo
// times come back anchored in conf.StartDate's tz (often the conf
// admin's local tz, e.g. CDT), while talk Sched.Start carries the
// venue's tz (e.g. CEST). Both are conceptually "venue local time" so
// we normalize by clock minutes and ignore offset.
func bucketByBreaks(ad *AgendaDay, sessions []*types.Session) {
	lunchMin, hasLunch := -1, false
	coffeeMin, hasCoffee := -1, false
	if ad.Info != nil {
		if ad.Info.Lunch != nil {
			lunchMin = ad.Info.Lunch.Start.Hour()*60 + ad.Info.Lunch.Start.Minute()
			hasLunch = true
		}
		if ad.Info.Coffee != nil {
			coffeeMin = ad.Info.Coffee.Start.Hour()*60 + ad.Info.Coffee.Start.Minute()
			hasCoffee = true
		}
	}
	for _, s := range sessions {
		if s.Sched == nil {
			ad.Morning = append(ad.Morning, s)
			continue
		}
		startMin := s.Sched.Start.Hour()*60 + s.Sched.Start.Minute()
		switch {
		case hasCoffee && startMin >= coffeeMin:
			ad.Evening = append(ad.Evening, s)
		case hasLunch && startMin >= lunchMin:
			ad.Afternoon = append(ad.Afternoon, s)
		default:
			ad.Morning = append(ad.Morning, s)
		}
	}
}

func agendaSessionsForVenue(day *AgendaDay, venue string) []*types.Session {
	if day == nil {
		return nil
	}
	out := make([]*types.Session, 0)
	for _, session := range day.All {
		if session != nil && session.Venue == venue {
			out = append(out, session)
			continue
		}
		if venue == "three" && session != nil && session.Venue != "one" && session.Venue != "two" {
			out = append(out, session)
		}
	}
	return out
}

func agendaTypeClass(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "keynote":
		return "keynote"
	case "panel", "45panel":
		return "panel"
	case "workshop":
		return "workshop"
	case "hackathon":
		return "hackathon"
	case "break", "lunch", "coffee":
		return "break"
	default:
		return "talk"
	}
}

func agendaTypeLabel(raw string) string {
	s := strings.TrimSpace(raw)
	if label := mapPresTypeToTalkType(strings.ToLower(s)); label != "" {
		return label
	}
	return s
}

type agendaHourMark struct {
	Label string
	Top   float64
}

func agendaDayStartMinute(day *AgendaDay) int {
	minute := -1
	if day != nil && day.Info != nil && day.Info.Doors != nil {
		minute = day.Info.Doors.Start.Hour()*60 + day.Info.Doors.Start.Minute()
	}
	for _, session := range agendaAllSessions(day) {
		if session.Sched == nil {
			continue
		}
		start := session.Sched.Start.Hour()*60 + session.Sched.Start.Minute()
		if minute < 0 || start < minute {
			minute = start
		}
	}
	if minute < 0 {
		return 9 * 60
	}
	return (minute / 30) * 30
}

func agendaDayEndMinute(day *AgendaDay) int {
	minute := -1
	if day != nil && day.Info != nil && day.Info.Doors != nil && day.Info.Doors.End != nil {
		minute = day.Info.Doors.End.Hour()*60 + day.Info.Doors.End.Minute()
	}
	for _, session := range agendaAllSessions(day) {
		if session.Sched == nil {
			continue
		}
		end := session.Sched.Start.Add(45 * time.Minute)
		if session.Sched.End != nil {
			end = *session.Sched.End
		}
		endMin := end.Hour()*60 + end.Minute()
		if minute < 0 || endMin > minute {
			minute = endMin
		}
	}
	if minute < 0 {
		return 18 * 60
	}
	return ((minute + 29) / 30) * 30
}

func agendaDayHeight(day *AgendaDay) float64 {
	minutes := agendaDayEndMinute(day) - agendaDayStartMinute(day)
	if minutes < 60 {
		minutes = 60
	}
	return float64(minutes) * agendaPixelsPerMinute
}

func agendaSessionTop(day *AgendaDay, session *types.Session) float64 {
	if day == nil || session == nil || session.Sched == nil {
		return 0
	}
	start := session.Sched.Start.Hour()*60 + session.Sched.Start.Minute()
	top := float64(start-agendaDayStartMinute(day)) * agendaPixelsPerMinute
	if top < 0 {
		return 0
	}
	return top
}

func agendaSessionHeight(session *types.Session) float64 {
	if session == nil || session.Sched == nil {
		return agendaMinSessionHeight
	}
	end := session.Sched.Start.Add(45 * time.Minute)
	if session.Sched.End != nil {
		end = *session.Sched.End
	}
	minutes := end.Sub(session.Sched.Start).Minutes()
	if minutes <= 0 {
		minutes = 45
	}
	height := minutes * agendaPixelsPerMinute
	if height < agendaMinSessionHeight {
		return agendaMinSessionHeight
	}
	return height
}

func agendaHourMarks(day *AgendaDay) []agendaHourMark {
	start := agendaDayStartMinute(day)
	end := agendaDayEndMinute(day)
	firstHour := ((start + 59) / 60) * 60
	marks := make([]agendaHourMark, 0)
	for minute := firstHour; minute <= end; minute += 60 {
		t := time.Date(2000, 1, 1, minute/60, minute%60, 0, 0, time.Local)
		marks = append(marks, agendaHourMark{
			Label: t.Format("3PM"),
			Top:   float64(minute-start) * agendaPixelsPerMinute,
		})
	}
	return marks
}

func agendaAllSessions(day *AgendaDay) []*types.Session {
	if day == nil {
		return nil
	}
	return day.All
}

// anyScheduledTalk reports whether at least one talk should appear on
// the public agenda. For active/future events, the explicit Scheduled
// status is required; for ended events, Accepted talks with scheduled
// times are included to preserve older archives whose proposal status
// was never flipped after import.
func anyScheduledTalk(conf *types.Conf, talks []*types.Talk) bool {
	for _, t := range talks {
		if publicAgendaTalk(conf, t) {
			return true
		}
	}
	return false
}

func publicAgendaTalk(conf *types.Conf, talk *types.Talk) bool {
	if talk == nil || talk.Sched == nil {
		return false
	}
	if talk.Status == StatusScheduled {
		return true
	}
	return conf != nil && conf.HasEnded() && talk.Status == StatusAccepted
}

// confInfosByDay flattens a tag-keyed map (the dashboard's existing
// map[tag][]*ConfInfo) into a Day → *ConfInfo map for one conf. Used
// by RenderConf to feed buildAgendaDays.
func confInfosByDay(infos []*types.ConfInfo) map[int]*types.ConfInfo {
	out := make(map[int]*types.ConfInfo, len(infos))
	for _, ci := range infos {
		if ci != nil && ci.Day > 0 {
			out[ci.Day] = ci
		}
	}
	return out
}

// computeCountdownBounds returns the (doors-open-day-1, doors-close-
// last-day) timestamps that drive the countdown widget on conf pages.
// Prefers ConfInfo.Doors when available (Day 1 for start, the highest
// Day with a Doors row for end); falls back to Conf.StartDate /
// Conf.EndDate when no ConfInfo is available, snapping to a 9:00 -
// 18:00 default window so the countdown still shows something useful.
// Either return value can be nil if even the fallback isn't set.
func computeCountdownBounds(conf *types.Conf, infosByDay map[int]*types.ConfInfo) (*time.Time, *time.Time) {
	if conf == nil {
		return nil, nil
	}
	loc := conf.Loc()
	var start, end *time.Time

	if infosByDay != nil {
		if ci, ok := infosByDay[1]; ok && ci != nil && ci.Doors != nil {
			t := ci.Doors.Start.In(loc)
			start = &t
		}
		// Find the highest Day index that has a Doors.End set.
		hi := -1
		for d := range infosByDay {
			if d > hi {
				if ci := infosByDay[d]; ci != nil && ci.Doors != nil && ci.Doors.End != nil {
					hi = d
				}
			}
		}
		if hi > 0 {
			t := infosByDay[hi].Doors.End.In(loc)
			end = &t
		}
	}

	// Fallbacks for confs without ConfInfo doors filled in: 9 AM
	// of StartDate's calendar day for open, 18:00 of EndDate's day
	// for close. Approximate but better than nothing.
	if start == nil && !conf.StartDate.IsZero() {
		sd := conf.StartDate.In(loc)
		t := time.Date(sd.Year(), sd.Month(), sd.Day(), 9, 0, 0, 0, loc)
		start = &t
	}
	if end == nil && !conf.EndDate.IsZero() {
		ed := conf.EndDate.In(loc)
		t := time.Date(ed.Year(), ed.Month(), ed.Day(), 18, 0, 0, 0, loc)
		end = &t
	}
	return start, end
}
