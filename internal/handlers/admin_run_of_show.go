package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/ics"
	"btcpp-web/internal/types"
)

// venuePalette is the cycle of hex colors assigned to venues for the
// Where-column text on the run-of-show. Tailwind 700-shade equivalents
// so the colors stay legible on the white table background and
// reasonably distinct under a B&W printer if anyone prints in mono.
// Picked by sorted-index of the conf's venue list, so a given conf
// always gets the same mapping across renders.
var venuePalette = []string{
	"#4338ca", // indigo-700
	"#047857", // emerald-700
	"#be123c", // rose-700
	"#b45309", // amber-700
	"#0e7490", // cyan-700
}

// venueLabels maps the raw Notion venue tags (the multi-select values
// stored on ConfInfo.Venues / ConfTalk.Venue) to friendly display
// labels per conference. Anything not in this map renders as the raw
// tag — admins can keep entering Notion-friendly slugs while the
// run-of-show shows the human-readable name.
var venueLabels = map[string]map[string]string{
	"vienna": {
		"one": "Main Stage",
		"two": "Talks Stage",
	},
}

// venueLabel resolves a raw venue tag to its display label for a
// given conf, falling back to the raw tag when no mapping is set.
func venueLabel(confTag, raw string) string {
	if raw == "" {
		return ""
	}
	if m, ok := venueLabels[confTag]; ok {
		if l, ok := m[raw]; ok {
			return l
		}
	}
	if label := ics.MapVenue(raw); label != "" {
		return label
	}
	return raw
}

// RunOfShowAdmin renders /{conf}/admin/run-of-show — a per-day
// timeline table interleaving ConfInfo events (doors, coffee, lunch),
// volunteer shifts, and conference talks. Read-only; no writes.
func RunOfShowAdmin(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfStaff(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	infos, err := getters.ListConfInfos(ctx, conf.Tag)
	if err != nil {
		ctx.Err.Printf("/%s/admin/run-of-show list confinfos: %s", conf.Tag, err)
		http.Error(w, "Unable to load run of show", http.StatusInternalServerError)
		return
	}
	talks, err := getters.GetTalksFor(ctx, conf.Tag)
	if err != nil {
		ctx.Err.Printf("/%s/admin/run-of-show list talks: %s", conf.Tag, err)
		http.Error(w, "Unable to load run of show", http.StatusInternalServerError)
		return
	}
	shifts, err := getters.GetShiftsForConf(ctx, conf.Tag)
	if err != nil {
		ctx.Err.Printf("/%s/admin/run-of-show list shifts: %s", conf.Tag, err)
		http.Error(w, "Unable to load run of show", http.StatusInternalServerError)
		return
	}
	// Resolve volunteer page-IDs → names so the Who column can show
	// readable assignee lists. Best-effort: a list error degrades to
	// empty Who cells rather than failing the page.
	volByRef := map[string]*types.Volunteer{}
	if vols, err := getters.ListVolunteersForConf(ctx, conf.Ref); err != nil {
		ctx.Err.Printf("/%s/admin/run-of-show list volunteers (continuing): %s", conf.Tag, err)
	} else {
		for _, v := range vols {
			if v != nil && v.Ref != "" {
				volByRef[v.Ref] = v
			}
		}
	}

	confForRun, days, venues := buildRunOfShowData(conf, infos, talks, shifts, volByRef, true)
	page := &RunOfShowPage{
		Conf:         confForRun,
		Days:         days,
		Venues:       venues,
		FlashMessage: r.URL.Query().Get("flash"),
		Year:         helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/run_of_show.tmpl", page); err != nil {
		ctx.Err.Printf("/%s/admin/run-of-show render: %s", conf.Tag, err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

func RunOfShowPublic(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	infos, err := getters.ListConfInfos(ctx, conf.Tag)
	if err != nil {
		ctx.Err.Printf("/%s/run-of-show list confinfos: %s", conf.Tag, err)
		http.Error(w, "Unable to load run of show", http.StatusInternalServerError)
		return
	}
	talks, err := getters.GetTalksFor(ctx, conf.Tag)
	if err != nil {
		ctx.Err.Printf("/%s/run-of-show list talks: %s", conf.Tag, err)
		http.Error(w, "Unable to load run of show", http.StatusInternalServerError)
		return
	}
	shifts, err := getters.GetShiftsForConf(ctx, conf.Tag)
	if err != nil {
		ctx.Err.Printf("/%s/run-of-show list shifts: %s", conf.Tag, err)
		http.Error(w, "Unable to load run of show", http.StatusInternalServerError)
		return
	}
	volByRef := map[string]*types.Volunteer{}
	if vols, err := getters.ListVolunteersForConf(ctx, conf.Ref); err != nil {
		ctx.Err.Printf("/%s/run-of-show list volunteers (continuing): %s", conf.Tag, err)
	} else {
		for _, v := range vols {
			if v != nil && v.Ref != "" {
				volByRef[v.Ref] = v
			}
		}
	}

	confForRun, days, venues := buildRunOfShowData(conf, infos, talks, shifts, volByRef, false)
	stages := buildPublicRunOfShowStages(days, venues)
	markRunOfShowStagesProgress(stages, time.Now().In(runOfShowLocation(confForRun)))
	page := &PublicRunOfShowPage{
		Conf:   confForRun,
		Stages: stages,
		Year:   helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "run_of_show.tmpl", page); err != nil {
		ctx.Err.Printf("/%s/run-of-show render: %s", conf.Tag, err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

func buildRunOfShowData(conf *types.Conf, infos []*types.ConfInfo, talks []*types.Talk, shifts []*types.WorkShift, volByRef map[string]*types.Volunteer, includeVolunteers bool) (*types.Conf, []*RunOfShowDay, []VenueOption) {
	loc := runOfShowLocation(conf)
	confForRun := *conf
	confForRun.TZ = loc
	confForRun.Timezone = loc.String()
	confForRun.StartDate = conf.StartDate.In(loc)
	confForRun.EndDate = conf.EndDate.In(loc)

	dayByIdx := map[int]*RunOfShowDay{}
	dayFor := func(idx int) *RunOfShowDay {
		d, ok := dayByIdx[idx]
		if !ok {
			d = &RunOfShowDay{Idx: idx, Date: dayDateFor(&confForRun, idx)}
			dayByIdx[idx] = d
		}
		return d
	}

	// Each entry with a time range produces TWO rows — one at start
	// and one at end — bucketed independently so an overnight shift
	// (start day N, end day N+1) lands on both days correctly.
	//
	// Normalize every row's Start to conf-local tz before bucketing
	// + sorting. parseTimes returns whatever zone Notion stored
	// (typically UTC for datetimes), but parseTimesRange anchors
	// ConfInfo events to conf-local. Without this conversion, a shift
	// end at "17:00 UTC" displays as "5:00 PM" but sorts at the same
	// instant as "09:00 conf-local" — which is exactly the
	// "shift ends in the wrong chronological position" symptom.
	addRows := func(rows []*RunOfShowRow) {
		for _, row := range rows {
			if row == nil {
				continue
			}
			row.Start = row.Start.In(loc)
			idx := dayIndexFor(&confForRun, row.Start)
			dayFor(idx).Rows = append(dayFor(idx).Rows, row)
		}
	}
	for _, ci := range infos {
		addRows(rowsFromConfInfo(ci))
	}
	for _, t := range talks {
		if t == nil || t.Sched == nil || !runOfShowTalkVisible(t) {
			continue
		}
		addRows(rowsFromTalk(conf.Tag, t, stageCrewForTalk(conf.Tag, t, shifts, volByRef)))
	}
	if includeVolunteers {
		for _, s := range shifts {
			if s == nil || s.ShiftTime == nil {
				continue
			}
			addRows(rowsFromShift(s, volByRef))
		}
	}

	days := make([]*RunOfShowDay, 0, len(dayByIdx))
	for _, d := range dayByIdx {
		sort.SliceStable(d.Rows, func(i, j int) bool {
			return d.Rows[i].Start.Before(d.Rows[j].Start)
		})
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Idx < days[j].Idx })
	markRunOfShowProgress(days, time.Now().In(loc))

	// Collect unique non-empty venue tags across every talk row so
	// the template can render a checkbox per venue. Sort by display
	// label for stable, alphabetical UI, then assign each venue a
	// color from a fixed palette by sorted-index — same conf always
	// gets the same color mapping across renders.
	venueSeen := map[string]bool{}
	var venues []VenueOption
	for _, d := range days {
		for _, row := range d.Rows {
			if row.VenueTag == "" || venueSeen[row.VenueTag] {
				continue
			}
			venueSeen[row.VenueTag] = true
			venues = append(venues, VenueOption{
				Tag:   row.VenueTag,
				Label: venueLabel(conf.Tag, row.VenueTag),
			})
		}
	}
	sort.SliceStable(venues, func(i, j int) bool { return venues[i].Label < venues[j].Label })
	for i := range venues {
		venues[i].Color = venuePalette[i%len(venuePalette)]
	}

	return &confForRun, days, venues
}

func buildPublicRunOfShowStages(days []*RunOfShowDay, venues []VenueOption) []*RunOfShowStage {
	stages := make([]*RunOfShowStage, 0, len(venues))
	for _, venue := range venues {
		stage := &RunOfShowStage{Venue: venue}
		for _, day := range days {
			if day == nil {
				continue
			}
			stageDay := &RunOfShowDay{Idx: day.Idx, Date: day.Date}
			for _, row := range day.Rows {
				if row == nil {
					continue
				}
				if row.Kind == "info" || (row.Kind == "talk" && row.VenueTag == venue.Tag) {
					stageDay.Rows = append(stageDay.Rows, cloneRunOfShowRow(row))
				}
			}
			if len(stageDay.Rows) > 0 {
				stage.Days = append(stage.Days, stageDay)
			}
		}
		stages = append(stages, stage)
	}
	if len(stages) > 0 {
		return stages
	}
	stage := &RunOfShowStage{Venue: VenueOption{Tag: "schedule", Label: "Schedule"}}
	for _, day := range days {
		if day == nil {
			continue
		}
		stageDay := &RunOfShowDay{Idx: day.Idx, Date: day.Date}
		for _, row := range day.Rows {
			if row != nil && row.Kind == "info" {
				stageDay.Rows = append(stageDay.Rows, cloneRunOfShowRow(row))
			}
		}
		if len(stageDay.Rows) > 0 {
			stage.Days = append(stage.Days, stageDay)
		}
	}
	if len(stage.Days) == 0 {
		return nil
	}
	return []*RunOfShowStage{stage}
}

func cloneRunOfShowRow(row *RunOfShowRow) *RunOfShowRow {
	if row == nil {
		return nil
	}
	copied := *row
	copied.IsCurrent = false
	copied.NowMarkerBefore = false
	return &copied
}

func markRunOfShowProgress(days []*RunOfShowDay, now time.Time) {
	for _, day := range days {
		if day == nil {
			continue
		}
		day.NowMarkerAfter = false
		for _, row := range day.Rows {
			if row == nil {
				continue
			}
			row.IsCurrent = false
			row.NowMarkerBefore = false
		}
		if !sameRunOfShowDate(day.Date, now) {
			continue
		}

		hasCurrent := false
		for _, row := range day.Rows {
			if row == nil || row.End == nil || !row.End.After(row.Start) {
				continue
			}
			if !now.Before(row.Start) && now.Before(*row.End) {
				row.IsCurrent = true
				hasCurrent = true
			}
		}
		if hasCurrent {
			continue
		}

		for _, row := range day.Rows {
			if row == nil {
				continue
			}
			if row.Start.After(now) {
				row.NowMarkerBefore = true
				hasCurrent = true
				break
			}
		}
		if !hasCurrent && len(day.Rows) > 0 {
			day.NowMarkerAfter = true
		}
	}
}

func markRunOfShowStagesProgress(stages []*RunOfShowStage, now time.Time) {
	for _, stage := range stages {
		if stage == nil {
			continue
		}
		markRunOfShowProgress(stage.Days, now)
	}
}

func sameRunOfShowDate(a, b time.Time) bool {
	b = b.In(a.Location())
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func runOfShowTalkVisible(t *types.Talk) bool {
	if t == nil {
		return false
	}
	switch t.Status {
	case "", StatusAccepted, StatusScheduled:
		return true
	default:
		return false
	}
}

// rangedRows emits a "start" row at t.Start and, when t.End is set
// and is strictly after t.Start, a matching "end" row prefixed with
// "End: " so the timeline shows both moments at their actual times.
// startRow carries the full content (Who / Where); the end row keeps
// only the labelled What so the timeline doesn't repeat speaker /
// venue info on the closing line.
func rangedRows(t *types.Times, kind, label, who, where string) []*RunOfShowRow {
	if t == nil {
		return nil
	}
	rows := []*RunOfShowRow{{
		Start: t.Start,
		End:   t.End,
		Kind:  kind,
		What:  label,
		Who:   who,
		Where: where,
	}}
	if t.End != nil && t.End.After(t.Start) {
		rows = append(rows, &RunOfShowRow{
			Start: *t.End,
			Kind:  kind,
			What:  "End: " + label,
		})
	}
	return rows
}

// rowsFromConfInfo emits timeline rows for the per-day strip events.
// Each event with an End time produces two rows (start + end), placed
// at their respective times so the run-of-show reads chronologically.
func rowsFromConfInfo(ci *types.ConfInfo) []*RunOfShowRow {
	var rows []*RunOfShowRow
	if ci == nil {
		return rows
	}
	// Doors gets a custom pair so the labels read "Doors open" /
	// "Doors close" rather than "Doors" / "End: Doors".
	if ci.Doors != nil {
		rows = append(rows, &RunOfShowRow{
			Start: ci.Doors.Start,
			End:   ci.Doors.End,
			Kind:  "info",
			What:  "Doors open",
		})
		if ci.Doors.End != nil && ci.Doors.End.After(ci.Doors.Start) {
			rows = append(rows, &RunOfShowRow{
				Start: *ci.Doors.End,
				Kind:  "info",
				What:  "Doors close",
			})
		}
	}
	rows = append(rows, rangedRows(ci.Breakfast, "info", "Breakfast", "", "")...)
	rows = append(rows, rangedRows(ci.Coffee, "info", "Coffee", "", "")...)
	rows = append(rows, rangedRows(ci.Lunch, "info", "Lunch", "", "")...)
	return rows
}

// rowsFromTalk emits a single timeline row for a talk. The talk's
// duration is folded into the label as "Title (30m)" rather than
// emitting a separate "End:" row at the close time — talks fit
// densely on the page and an inline duration reads more cleanly than
// duplicate rows. Where carries the human-readable venue label
// (resolved per confTag); the raw tag rides along on VenueTag for
// the per-venue visibility toggle.
func rowsFromTalk(confTag string, t *types.Talk, crew []RunOfShowCrew) []*RunOfShowRow {
	names := make([]string, 0, len(t.Speakers))
	seen := map[string]bool{}
	for _, sp := range t.Speakers {
		if sp == nil || sp.Name == "" || seen[sp.ID] {
			continue
		}
		seen[sp.ID] = true
		name := sp.Name
		if len(t.Speakers) > 1 && sp.RecordingEmoji != "" {
			name += " " + sp.RecordingEmoji
		}
		names = append(names, name)
	}
	label := t.Name
	if t.Sched.End != nil && t.Sched.End.After(t.Sched.Start) {
		durMin := int(t.Sched.End.Sub(t.Sched.Start).Minutes())
		label = fmt.Sprintf("%s (%dm)", t.Name, durMin)
	}
	if t.RecordingRestricted {
		label = "🛑 " + label
	} else if t.RecordingAudioOnly {
		label = "🔇 " + label
	}
	return []*RunOfShowRow{{
		Start:    t.Sched.Start,
		End:      t.Sched.End,
		Kind:     "talk",
		What:     label,
		Who:      strings.Join(names, ", "),
		Crew:     crew,
		Where:    venueLabel(confTag, t.Venue),
		VenueTag: t.Venue,
	}}
}

func stageCrewForTalk(confTag string, t *types.Talk, shifts []*types.WorkShift, volByRef map[string]*types.Volunteer) []RunOfShowCrew {
	if t == nil || t.Sched == nil {
		return nil
	}
	var crew []RunOfShowCrew
	for _, role := range []struct {
		Label string
		Tags  []string
	}{
		{Label: "Stage Manager", Tags: []string{"showrunner", "stage-manager", "stage_manager"}},
		{Label: "A/V Tech", Tags: []string{"avdesk", "av", "a/v", "av-tech", "av_tech"}},
	} {
		names := stageCrewNames(confTag, t, shifts, volByRef, role.Tags)
		if len(names) == 0 {
			continue
		}
		crew = append(crew, RunOfShowCrew{
			Label: role.Label,
			Names: strings.Join(names, ", "),
		})
	}
	return crew
}

func stageCrewNames(confTag string, t *types.Talk, shifts []*types.WorkShift, volByRef map[string]*types.Volunteer, roleTags []string) []string {
	names := []string{}
	seen := map[string]bool{}
	for _, s := range shifts {
		if !shiftCoversTalk(s, t) || !shiftMatchesRole(s, roleTags) || !shiftMatchesVenue(confTag, s, t.Venue) {
			continue
		}
		for _, ref := range shiftVolunteerRefs(s) {
			if ref == "" || seen[ref] {
				continue
			}
			if v := volByRef[ref]; v != nil && v.Name != "" {
				names = append(names, v.Name)
				seen[ref] = true
			}
		}
	}
	return names
}

func shiftCoversTalk(s *types.WorkShift, t *types.Talk) bool {
	if s == nil || s.ShiftTime == nil || s.ShiftTime.End == nil || t == nil || t.Sched == nil {
		return false
	}
	start := t.Sched.Start
	return !start.Before(s.ShiftTime.Start) && start.Before(*s.ShiftTime.End)
}

func shiftMatchesRole(s *types.WorkShift, tags []string) bool {
	if s == nil || s.Type == nil {
		return false
	}
	tag := strings.ToLower(strings.TrimSpace(s.Type.Tag))
	title := strings.ToLower(strings.TrimSpace(s.Type.Title))
	for _, want := range tags {
		if tag == want || strings.Contains(title, want) {
			return true
		}
	}
	return false
}

func shiftMatchesVenue(confTag string, s *types.WorkShift, venue string) bool {
	if s == nil || venue == "" {
		return false
	}
	name := strings.ToLower(s.Name)
	return strings.Contains(name, strings.ToLower(venue)) ||
		strings.Contains(name, strings.ToLower(venueLabel(confTag, venue)))
}

func shiftVolunteerRefs(s *types.WorkShift) []string {
	if s == nil {
		return nil
	}
	refs := make([]string, 0, len(s.AssigneesRef)+1)
	if s.ShiftLeaderRef != "" {
		refs = append(refs, s.ShiftLeaderRef)
	}
	for _, ref := range s.AssigneesRef {
		if ref != s.ShiftLeaderRef {
			refs = append(refs, ref)
		}
	}
	return refs
}

// rowsFromShift emits a start row listing every assigned volunteer
// (leader first, tagged " (lead)") and, when the shift has an End
// time, a closing "End: <label>" row with the same volunteer list
// so staff can see whose coverage is ending at that moment.
func rowsFromShift(s *types.WorkShift, volByRef map[string]*types.Volunteer) []*RunOfShowRow {
	var who []string
	included := map[string]bool{}
	if s.ShiftLeaderRef != "" {
		if v := volByRef[s.ShiftLeaderRef]; v != nil && v.Name != "" {
			who = append(who, v.Name+" (lead)")
			included[s.ShiftLeaderRef] = true
		}
	}
	for _, ref := range s.AssigneesRef {
		if ref == "" || included[ref] {
			continue
		}
		v := volByRef[ref]
		if v == nil || v.Name == "" {
			continue
		}
		who = append(who, v.Name)
		included[ref] = true
	}
	whoLabel := strings.Join(who, ", ")
	rows := rangedRows(s.ShiftTime, "shift", shiftLabel(s), whoLabel, "")
	if len(rows) > 1 {
		rows[1].Who = whoLabel
	}
	return rows
}

// shiftLabel produces the "What" string for a volunteer shift row.
// Prefer the JobType title (e.g. "Registration", "Catering"); fall
// back to the shift's own Name; empty-string in last resort. Always
// prefixed with "Volunteer shift: " so the row sorts visually under
// the same prefix on the Run-of-Show table.
func shiftLabel(s *types.WorkShift) string {
	label := ""
	if s.Type != nil && s.Type.Title != "" {
		label = s.Type.Title
	} else if s.Name != "" {
		label = s.Name
	}
	if label == "" {
		return "Volunteer shift"
	}
	return "Volunteer shift: " + label
}

func runOfShowLocation(conf *types.Conf) *time.Location {
	if conf == nil {
		return time.Local
	}
	if strings.TrimSpace(conf.Timezone) != "" {
		if loc, err := time.LoadLocation(strings.TrimSpace(conf.Timezone)); err == nil {
			if loc != time.UTC {
				return loc
			}
		}
	}
	if loc := conf.Loc(); loc != nil {
		return loc
	}
	if strings.TrimSpace(conf.Timezone) != "" {
		if loc, err := time.LoadLocation(strings.TrimSpace(conf.Timezone)); err == nil {
			return loc
		}
	}
	if loc := conf.StartDate.Location(); loc != nil {
		return loc
	}
	return time.Local
}

// formatRunOfShowTime returns "9:30 AM" for any time.Time. Wired
// into the template funcMap as `formatTime` (see handlers.go).
func formatRunOfShowTime(t time.Time) string {
	return t.Format("3:04 PM")
}
