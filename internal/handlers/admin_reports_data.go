package handlers

import (
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"sort"
)

// Aggregate relationships separately so team size cannot multiply prizes.
const eventReportAttendanceSQL = `SELECT
 (SELECT count(*) FROM registrations WHERE conference_id=$1 AND NOT coalesce(revoked_before_account_deletion,revoked)),
 (SELECT count(DISTINCT coalesce('person:'||coalesce(r.person_id,pe.person_id)::text,'email:'||lower(trim(r.email::text)))) FROM registrations r LEFT JOIN person_emails pe ON pe.email=r.email WHERE r.conference_id=$1 AND NOT coalesce(r.revoked_before_account_deletion,r.revoked) AND r.checked_in_at IS NOT NULL)`

const eventReportMetricsSQL = eventReportAttendanceSQL + `,
 (SELECT count(*) FROM projects p JOIN competitions k ON k.id=p.competition_id WHERE k.conference_id=$1 AND p.status IN ('submitted','advanced')),
 (SELECT count(DISTINCT m.person_id) FROM project_members m JOIN projects p ON p.id=m.project_id JOIN competitions k ON k.id=p.competition_id WHERE k.conference_id=$1 AND p.status IN ('submitted','advanced')),
 (SELECT count(*) FROM awards a JOIN competitions k ON k.id=a.competition_id WHERE k.conference_id=$1 AND a.award_type='challenge' AND a.sponsored_by_org_id IS NOT NULL AND a.status<>'draft' AND a.archived_at IS NULL),
 (SELECT coalesce(sum(d.amount_sats),0)::bigint FROM award_distributions d JOIN competitions k ON k.id=d.competition_id WHERE k.conference_id=$1 AND d.distribution_type='sats' AND d.status IN ('sent','claimed')),
 (SELECT count(DISTINCT coalesce(v.person_id::text,v.id::text)) FROM work_shifts_volunteers wv JOIN work_shifts w ON w.id=wv.shift_id JOIN volunteers v ON v.id=wv.volunteer_id WHERE w.conference_id=$1),
 (SELECT count(*) FROM proposals WHERE conference_id=$1 AND lower(talk_type) IN ('talk','keynote','workshop','panel'))`

// One row per project, counting distinct members before grouping into size buckets.
const eventReportTeamSizesSQL = `WITH sizes AS (
 SELECT p.id,count(DISTINCT m.person_id) AS members
 FROM projects p JOIN competitions k ON k.id=p.competition_id
 LEFT JOIN project_members m ON m.project_id=p.id
 WHERE k.conference_id=$1 AND p.status IN ('submitted','advanced') GROUP BY p.id
) SELECT least(members,5),count(*) FROM sizes GROUP BY least(members,5) ORDER BY 1`

const eventReportSessionsSQL = `SELECT t.id::text,p.title,lower(p.talk_type),t.scheduled_start,t.scheduled_end,
 coalesce((SELECT string_agg(DISTINCT person.name,', ' ORDER BY person.name) FROM proposals_speaker_confs ps JOIN speaker_confs sc ON sc.id=ps.speaker_conf_id JOIN people person ON person.id=sc.speaker_id WHERE ps.proposal_id=p.id),''),coalesce(r.youtube_url,''),coalesce(r.x_url,''),coalesce(t.venue,''),coalesce(t.social_card_path,'')
 FROM conf_talks t JOIN proposals p ON p.id=t.proposal_id LEFT JOIN recordings r ON r.conf_talk_id=t.id
 WHERE t.conference_id=$1 AND t.archived_at IS NULL AND p.status IN ('Accepted','Scheduled') AND lower(p.talk_type) IN ('talk','keynote','panel','workshop')
 ORDER BY t.scheduled_start NULLS LAST,p.title,t.id`
const eventReportSpeakersSQL = `SELECT person.id::text,person.name,person.norm_photo_path,person.company,
 coalesce(string_agg(DISTINCT nullif(trim(sc.coming_from),''),' / ' ORDER BY nullif(trim(sc.coming_from),'')) FILTER (WHERE lower(trim(sc.coming_from)) NOT IN ('unknown','n/a','not provided')),'')
 FROM conf_talks t JOIN proposals p ON p.id=t.proposal_id JOIN proposals_speaker_confs ps ON ps.proposal_id=p.id JOIN speaker_confs sc ON sc.id=ps.speaker_conf_id JOIN people person ON person.id=sc.speaker_id
 WHERE t.conference_id=$1 AND t.archived_at IS NULL AND p.status IN ('Accepted','Scheduled') AND lower(p.talk_type) IN ('talk','keynote','panel','workshop') GROUP BY person.id,person.name,person.norm_photo_path,person.company ORDER BY person.name,person.id::text`
const eventReportJudgesSQL = `WITH assignments AS (
 SELECT j.person_id,coalesce(nullif(j.public_label_override,''),j.judge_type) AS role FROM competition_judges j JOIN competitions k ON k.id=j.competition_id WHERE k.conference_id=$1
 UNION SELECT j.person_id,'Sponsor challenge judge' FROM award_judges j JOIN awards a ON a.id=j.award_id JOIN competitions k ON k.id=a.competition_id WHERE k.conference_id=$1 AND a.archived_at IS NULL AND a.status<>'draft'
 ) SELECT p.id::text,p.name,p.norm_photo_path,p.company,string_agg(DISTINCT a.role,', ' ORDER BY a.role) FROM assignments a JOIN people p ON p.id=a.person_id GROUP BY p.id,p.name,p.norm_photo_path,p.company ORDER BY p.name,p.id`
const eventReportWinnersSQL = `SELECT p.id::text,p.title,p.short_description,p.image_url,p.github_url,p.demo_url,string_agg(DISTINCT a.title,' · ' ORDER BY a.title),
 coalesce((SELECT string_agg(DISTINCT person.name,', ' ORDER BY person.name) FROM project_members m JOIN people person ON person.id=m.person_id WHERE m.project_id=p.id),''),
 (SELECT coalesce(sum(d.amount_sats),0)::bigint FROM award_distributions d WHERE d.project_id=p.id AND d.competition_id=p.competition_id AND d.distribution_type='sats' AND d.status IN ('sent','claimed')),
 coalesce((SELECT jsonb_agg(jsonb_build_object('Name',person.name,'Photo',coalesce(person.norm_photo_path,'')) ORDER BY person.name)
 FROM project_members m JOIN people person ON person.id=m.person_id
 WHERE m.project_id=p.id),'[]'::jsonb)
 FROM projects p JOIN project_awards pa ON pa.project_id=p.id JOIN awards a ON a.id=pa.award_id AND a.competition_id=p.competition_id JOIN competitions k ON k.id=p.competition_id
 WHERE k.conference_id=$1 AND k.results_finalized_at IS NOT NULL AND p.status IN ('submitted','advanced') AND a.status='awarded' AND a.archived_at IS NULL
 GROUP BY p.id,p.title,p.short_description,p.image_url,p.github_url,p.demo_url,p.competition_id ORDER BY p.title,p.id`

func loadEventReport(ctx context.Context, app *config.AppContext, page *sponsorReportPage) error {
	tx, err := app.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := loadEventReportTx(ctx, app, tx, page); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Shared by annual reports so every event and deduplicated total uses one snapshot.
func loadEventReportTx(ctx context.Context, app *config.AppContext, tx pgx.Tx, page *sponsorReportPage) error {
	var err error
	if err = tx.QueryRow(ctx, eventReportMetricsSQL, page.Event.ID).Scan(&page.Registrations, &page.Attendees, &page.Teams, &page.Participants, &page.Challenges, &page.Sats, &page.Volunteers, &page.Proposals); err != nil {
		return err
	}
	statusRows, err := tx.Query(ctx, `SELECT coalesce(nullif(status,''),'Unclassified'),count(*) FROM proposals WHERE conference_id=$1 AND lower(talk_type) IN ('talk','keynote','workshop','panel') GROUP BY status ORDER BY status`, page.Event.ID)
	if err != nil {
		return err
	}
	page.ProposalStatuses, err = pgx.CollectRows(statusRows, func(row pgx.CollectableRow) (reportBar, error) {
		var b reportBar
		err := row.Scan(&b.Label, &b.Value)
		return b, err
	})
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, eventReportTeamSizesSQL, page.Event.ID)
	if err != nil {
		return err
	}
	page.TeamSizes = []reportBar{{Label: "No members"}, {Label: "1"}, {Label: "2"}, {Label: "3"}, {Label: "4"}, {Label: "5+"}}
	for rows.Next() {
		var bucket int
		var count int64
		if err = rows.Scan(&bucket, &count); err != nil {
			rows.Close()
			return err
		}
		page.TeamSizes[bucket].Value = count
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	rows, err = tx.Query(ctx, eventReportSessionsSQL, page.Event.ID)
	if err != nil {
		return err
	}
	page.Sessions, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (reportSession, error) {
		var s reportSession
		err := row.Scan(&s.ID, &s.Title, &s.Kind, &s.Start, &s.End, &s.Speakers, &s.YouTube, &s.X, &s.Venue, &s.SocialCard)
		return s, err
	})
	if err != nil {
		return err
	}
	rows, err = tx.Query(ctx, eventReportSpeakersSQL, page.Event.ID)
	if err != nil {
		return err
	}
	page.Speakers, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (reportPerson, error) {
		var p reportPerson
		err := row.Scan(&p.ID, &p.Name, &p.Photo, &p.Company, &p.ComingFrom)
		p.Photo = SpeakerPhotoURL(app, p.Photo)
		return p, err
	})
	if err != nil {
		return err
	}
	rows, err = tx.Query(ctx, eventReportJudgesSQL, page.Event.ID)
	if err != nil {
		return err
	}
	page.Judges, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (reportPerson, error) {
		var p reportPerson
		err := row.Scan(&p.ID, &p.Name, &p.Photo, &p.Company, &p.Role)
		p.Photo = SpeakerPhotoURL(app, p.Photo)
		return p, err
	})
	if err != nil {
		return err
	}
	rows, err = tx.Query(ctx, eventReportWinnersSQL, page.Event.ID)
	if err != nil {
		return err
	}
	page.Winners, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (reportWinner, error) {
		var p reportWinner
		var portraits []byte
		err := row.Scan(&p.ID, &p.Title, &p.Description, &p.Image, &p.GitHub, &p.Demo, &p.Awards, &p.Members, &p.Sats, &portraits)
		if err == nil {
			err = json.Unmarshal(portraits, &p.Portraits)
		}
		for i := range p.Portraits {
			p.Portraits[i].Photo = SpeakerPhotoURL(app, p.Portraits[i].Photo)
		}
		return p, err
	})
	if err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `SELECT p.id::text,p.title,p.short_description,p.image_url FROM projects p JOIN competitions c ON c.id=p.competition_id WHERE c.conference_id=$1 AND p.status IN ('submitted','advanced') ORDER BY lower(p.title),p.id`, page.Event.ID)
	if err != nil {
		return err
	}
	page.Projects, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (reportWinner, error) {
		var p reportWinner
		err := row.Scan(&p.ID, &p.Title, &p.Description, &p.Image)
		return p, err
	})
	if err != nil {
		return err
	}
	dates := map[string]bool{}
	for _, s := range page.Sessions {
		if s.Start != nil {
			dates[s.Start.In(reportEventLocation(page.Event)).Format("2006-01-02")] = true
		}
	}
	for date := range dates {
		page.Days = append(page.Days, reportDay{Date: date})
	}
	sort.Slice(page.Days, func(i, j int) bool { return page.Days[i].Date < page.Days[j].Date })
	if len(page.Days) == 0 {
		page.Days = eventReportDays(page.Event)
	}
	return nil
}
