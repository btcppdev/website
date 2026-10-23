package handlers

import (
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Deduplicate each person within an event, including those without a check-in.
const annualRegistrationsSQL = `WITH visits AS (
 SELECT DISTINCT r.conference_id,coalesce('person:'||coalesce(r.person_id,pe.person_id)::text,'email:'||lower(trim(r.email::text))) AS identity
 FROM registrations r LEFT JOIN person_emails pe ON pe.email=r.email
 WHERE r.conference_id::text=ANY($1::text[]) AND NOT coalesce(r.revoked_before_account_deletion,r.revoked)
), people AS (SELECT identity,count(*) AS events FROM visits GROUP BY identity)
SELECT count(*),count(*) FILTER(WHERE events>1) FROM people`

func loadAnnualReport(ctx context.Context, app *config.AppContext, p *annualReportPage) error {
	tx, err := app.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	postLibrary, err := loadReportPosts(ctx, tx, p.Year)
	if err != nil {
		return err
	}
	// Fetch the roster first and close it before opening reading each event.
	rows, err := tx.Query(ctx, `SELECT id::text,tag,coalesce(nullif(description,''),tag),location,timezone,date_desc,start_date,end_date FROM conferences WHERE publication_status='published' ORDER BY start_date,tag`)
	if err != nil {
		return err
	}
	var roster []reportEvent
	for rows.Next() {
		var e reportEvent
		if err = rows.Scan(&e.ID, &e.Tag, &e.Title, &e.Location, &e.Timezone, &e.DateDesc, &e.Start, &e.End); err != nil {
			break
		}
		if e.Start != nil && e.Start.In(reportEventLocation(e)).Year() == p.Year {
			e.Period = reportEventPeriod(e)
			e.LeadImage = confImagePath(e.Tag, "leading")
			roster = append(roster, e)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	p.RegistrationBased = true
	var ids, hackIDs []string
	speakers := map[string]bool{}
	for _, event := range roster {
		e := annualEvent{Event: event, Eligible: annualEligible(event, p.Year, p.Cutoff) && event.End != nil && !event.End.After(time.Now()), HackCoverage: p.Saved["hack_"+event.ID]}
		if e.HackCoverage == "" {
			e.HackCoverage = "unknown"
			// Confirmed by the organizer: these 2026 editions had no hackathon.
			if p.Year == 2026 && (strings.Contains(strings.ToLower(event.Tag+" "+event.Location), "vienna") || strings.Contains(strings.ToLower(event.Tag+" "+event.Location), "vegas")) {
				e.HackCoverage = "none"
			}
		}
		e.Included = e.Eligible && (p.Saved["selection_saved"] != "yes" || p.Saved["include_"+event.ID] == "yes")
		if e.Included {
			data := sponsorReportPage{Event: event, Saved: map[string]string{}}
			if err := loadEventReportTx(ctx, app, tx, &data); err != nil {
				return fmt.Errorf("%s: %w", event.Tag, err)
			}
			e.Attendees = data.Attendees
			var repeated int64
			if err := tx.QueryRow(ctx, annualRegistrationsSQL, []string{event.ID}).Scan(&e.Registrations, &repeated); err != nil {
				return err
			}
			e.Projects, e.Participants, e.Challenges, e.Sats = data.Teams, data.Participants, data.Challenges, data.Sats
			e.Winners = int64(len(data.Winners))
			e.WinningProjects = data.Winners
			e.detectHackathonRecords()
			e.Speakers = int64(data.SpeakerCount())
			for _, s := range data.Speakers {
				speakers[s.ID] = true
			}
			if chart := data.ProgramSummary(); chart != nil {
				e.Talks = chart.Rows[0].Value
				e.Workshops = chart.Rows[1].Value
				e.Panels = chart.Rows[2].Value
			}
			var content []byte
			err := tx.QueryRow(ctx, `SELECT content FROM sponsor_report_drafts WHERE scope=$1`, "event-v2/"+event.ID).Scan(&content)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if len(content) > 0 {
				if err = json.Unmarshal(content, &data.Saved); err != nil {
					return err
				}
			} else if strings.Contains(strings.ToLower(event.Tag+" "+event.Location), "nairobi") {
				if err = applyNairobiReportDefaults(&data); err != nil {
					return err
				}
			}
			e.Proposals = data.Proposals
			e.Applications = verifiedReportApplications(data.Saved)
			data.PostLibrary = postLibrary
			e.Posts = data.ApprovedPosts()
			e.Media = data.Media()
			for _, platform := range data.ReachPlatforms() {
				if platform.Ready() {
					e.Reach = append(e.Reach, platform)
				}
			}
			// Contradictory coverage must be resolved, rather than hiding real records.
			if e.HackCoverage == "none" && (e.Projects > 0 || e.Participants > 0 || e.Sats > 0 || e.Winners > 0) {
				return fmt.Errorf("%s is marked no hackathon but has hackathon records", event.Tag)
			}
			ids = append(ids, event.ID)
			if e.HackKnown() {
				hackIDs = append(hackIDs, event.ID)
			}
		}
		p.Events = append(p.Events, e)
	}
	// A changed cutoff cannot silently leave stale selections in the draft.
	for key, value := range p.Saved {
		if !strings.HasPrefix(key, "include_") || value != "yes" {
			continue
		}
		found := false
		for _, e := range p.Events {
			if e.Event.ID == strings.TrimPrefix(key, "include_") && e.Eligible {
				found = true
				break
			}
		}
		if !found {
			delete(p.Saved, key)
		}
	}
	p.UniqueSpeakers = int64(len(speakers))
	if len(ids) > 0 {
		if err = tx.QueryRow(ctx, annualRegistrationsSQL, ids).Scan(&p.UniqueRegistrants, &p.RepeatRegistrants); err != nil {
			return err
		}
	}
	if len(hackIDs) > 0 {
		if err = tx.QueryRow(ctx, `SELECT count(DISTINCT m.person_id) FROM project_members m JOIN projects p ON p.id=m.project_id JOIN competitions c ON c.id=p.competition_id WHERE c.conference_id::text=ANY($1::text[]) AND p.status IN ('submitted','advanced')`, hackIDs).Scan(&p.UniqueParticipants); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

type annualMapPoint struct {
	Name, Tag    string
	X, Y, Radius float64
}

func (p annualReportPage) MapPoints() []annualMapPoint {
	// Approximate city centres, never attendees' or speakers' personal locations.
	centres := map[string][2]float64{"nairobi": {-1.29, 36.82}, "toronto": {43.65, -79.38}, "berlin": {52.52, 13.4}, "vienna": {48.21, 16.37}, "vegas": {36.17, -115.14}, "floripa": {-27.59, -48.55}, "florianópolis": {-27.59, -48.55}, "florianopolis": {-27.59, -48.55}, "seoul": {37.57, 126.98}}
	var points []annualMapPoint
	for _, e := range p.Included() {
		var coord [2]float64
		found := false
		for _, key := range []string{"nairobi", "toronto", "berlin", "vienna", "vegas", "floripa", "florianópolis", "florianopolis", "seoul"} {
			if strings.Contains(strings.ToLower(e.Event.Location+" "+e.Event.Tag), key) {
				coord = centres[key]
				found = true
				break
			}
		}
		if !found {
			continue
		}
		lat, lon := coord[0], coord[1]
		center, cx := 30.0, 180.0
		if math.Cos((lon-center)*math.Pi/180) < 0 {
			center, cx = -150, 540
		}
		size := e.Attendees
		if p.RegistrationBased {
			size = e.Registrations
		}
		points = append(points, annualMapPoint{e.Event.Location, e.Event.Tag, cx + 155*math.Cos(lat*math.Pi/180)*math.Sin((lon-center)*math.Pi/180), 175 - 155*math.Sin(lat*math.Pi/180), 4 + math.Min(13, math.Sqrt(float64(size))/2)})
	}
	return points
}
func (p annualReportPage) ProgramRows() []struct {
	Name                                 string
	Count                                int64
	TalkWidth, WorkshopWidth, PanelWidth float64
} {
	type row = struct {
		Name                                 string
		Count                                int64
		TalkWidth, WorkshopWidth, PanelWidth float64
	}
	var max int64
	for _, e := range p.Included() {
		if e.Sessions() > max {
			max = e.Sessions()
		}
	}
	var out []row
	for _, e := range p.Included() {
		r := row{Name: e.Event.Location, Count: e.Sessions()}
		if max > 0 {
			r.TalkWidth = 100 * float64(e.Talks) / float64(max)
			r.WorkshopWidth = 100 * float64(e.Workshops) / float64(max)
			r.PanelWidth = 100 * float64(e.Panels) / float64(max)
		}
		out = append(out, r)
	}
	return out
}
func (p annualReportPage) HackCoverageCount() string {
	n := 0
	for _, e := range p.Included() {
		if e.HackKnown() {
			n++
		}
	}
	return strconv.Itoa(n)
}
