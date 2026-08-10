package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"strings"
	"time"
)

func GetVolInfo(ctx *config.AppContext, confRef string) (*types.VolInfo, error) {
	infos, err := GetVolInfos(ctx, confRef)
	if err != nil {
		return nil, err
	}

	if len(infos) == 0 {
		return nil, fmt.Errorf("Invalid confref for volinfos %s", confRef)
	}

	return infos[0], nil
}

func GetVolInfoMap(ctx *config.AppContext) (map[string]*types.VolInfo, error) {
	vmap := make(map[string]*types.VolInfo)
	volinfos, err := GetVolInfos(ctx, "")
	if err != nil {
		return vmap, err
	}

	confs, err := ListConfs(ctx)
	if err != nil {
		return vmap, err
	}
	for _, vi := range volinfos {
		for _, conf := range confs {
			if conf.Ref == vi.ConfRef {
				vmap[conf.Tag] = vi
				break
			}
		}
	}

	return vmap, nil
}

// registerConfirmedVolunteer persists an application only after the applicant
// has proved control of the submitted email address.
func registerConfirmedVolunteer(ctx *config.AppContext, vol *types.Volunteer) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if vol == nil {
		return fmt.Errorf("registerConfirmedVolunteer: volunteer is nil")
	}
	normalizeVolunteerInput(vol)
	vol.Shirt = types.ValidShirtSizeCode(vol.Shirt)
	if vol.Shirt == "" {
		return fmt.Errorf("registerConfirmedVolunteer: valid shirt size required")
	}
	if len(vol.ScheduleFor) == 0 || vol.ScheduleFor[0] == nil || vol.ScheduleFor[0].Ref == "" {
		return fmt.Errorf("registerConfirmedVolunteer: ScheduleFor required")
	}

	status := vol.Status
	if status == "" {
		status = "Applied"
	}

	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return fmt.Errorf("begin volunteer registration: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())

	conferenceID := vol.ScheduleFor[0].Ref
	// Serialize applications for the same email/event while identity ownership
	// is resolved below.
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		SELECT pg_advisory_xact_lock(hashtextextended(
			lower(btrim($1)) || ':' || $2,
			0
		))
	`, vol.Email, conferenceID); err != nil {
		return fmt.Errorf("lock volunteer application %q for conference %s: %w", vol.Email, conferenceID, err)
	}

	var personID string
	err = tx.QueryRow(ctx.DatabaseContext(), `
		SELECT person_id::text
		FROM person_emails
		WHERE email = $1::citext
	`, vol.Email).Scan(&personID)
	if errors.Is(err, pgx.ErrNoRows) {
		var conflicted bool
		if err := tx.QueryRow(ctx.DatabaseContext(), `
			SELECT EXISTS(SELECT 1 FROM person_email_conflicts WHERE email = $1::citext)
		`, vol.Email).Scan(&conflicted); err != nil {
			return fmt.Errorf("check volunteer email identity %q: %w", vol.Email, err)
		}
		if conflicted {
			return fmt.Errorf("registerConfirmedVolunteer: email %q belongs to profiles awaiting an account merge", vol.Email)
		}
		err = tx.QueryRow(ctx.DatabaseContext(), `
			INSERT INTO people (name, phone, signal, twitter_handle, nostr, tshirt)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id::text
		`, vol.Name, vol.Phone, vol.Signal, vol.Twitter.Handle, vol.Nostr, vol.Shirt).Scan(&personID)
		if err != nil {
			return fmt.Errorf("create person for volunteer %q: %w", vol.Email, err)
		}
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			INSERT INTO person_emails (person_id, email, is_primary, verified_at)
			VALUES ($1::uuid, $2::citext, true, now())
		`, personID, vol.Email); err != nil {
			return fmt.Errorf("attach volunteer email %q: %w", vol.Email, err)
		}
	} else if err != nil {
		return fmt.Errorf("resolve volunteer identity %q: %w", vol.Email, err)
	} else {
		// This function is called only after the submitted email has confirmed
		// the staged application. Fill missing canonical fields, but never replace
		// profile data that the person has already maintained.
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			UPDATE people
			SET phone = CASE WHEN btrim(phone) = '' THEN $2 ELSE phone END,
				signal = CASE WHEN btrim(signal) = '' THEN $3 ELSE signal END,
				twitter_handle = CASE WHEN btrim(twitter_handle) = '' THEN $4 ELSE twitter_handle END,
				nostr = CASE WHEN btrim(nostr) = '' THEN $5 ELSE nostr END,
				tshirt = CASE WHEN btrim(tshirt) = '' THEN $6 ELSE tshirt END
			WHERE id = $1::uuid
		`, personID, vol.Phone, vol.Signal, vol.Twitter.Handle, vol.Nostr, vol.Shirt); err != nil {
			return fmt.Errorf("complete volunteer profile %s: %w", personID, err)
		}
	}
	// Different verified aliases for the same person must serialize on the
	// person/event pair, not merely on the submitted email above.
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2, 0))
	`, personID, conferenceID); err != nil {
		return fmt.Errorf("lock volunteer person %s for conference %s: %w", personID, conferenceID, err)
	}

	var volunteerID string
	err = tx.QueryRow(ctx.DatabaseContext(), `
		SELECT volunteer.id::text
		FROM volunteers volunteer
		JOIN volunteers_conferences conference
			ON conference.volunteer_id = volunteer.id
		WHERE volunteer.person_id = $1::uuid
			AND conference.conference_id = $2::uuid
			AND conference.kind = 'schedule_for'
		ORDER BY volunteer.created_at DESC, volunteer.id
		LIMIT 1
		FOR UPDATE OF volunteer
	`, personID, conferenceID).Scan(&volunteerID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx.DatabaseContext(), `
			INSERT INTO volunteers (
				person_id, availability, contact_at, comments, discovered_via,
				first_event, hometown, status, captcha, subscribe
			) VALUES (
				$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10
			)
			RETURNING id::text
		`, personID, vol.Availability, vol.ContactAt, vol.Comments,
			vol.DiscoveredVia, vol.FirstEvent, vol.Hometown, status,
			vol.Captcha, vol.Subscribe).Scan(&volunteerID)
		if err != nil {
			return fmt.Errorf("insert volunteer %q: %w", vol.Email, err)
		}
	} else if err != nil {
		return fmt.Errorf("find existing volunteer application %q for conference %s: %w", vol.Email, conferenceID, err)
	} else {
		return ErrVolunteerAlreadyApplied
	}

	if err := insertVolunteerConferenceLinksPostgres(tx, volunteerID, vol.ScheduleFor, "schedule_for"); err != nil {
		return err
	}
	if err := insertVolunteerConferenceLinksPostgres(tx, volunteerID, vol.OtherEvents, "other_event"); err != nil {
		return err
	}
	if err := insertVolunteerJobLinksPostgres(tx, volunteerID, vol.WorkYes, "yes"); err != nil {
		return err
	}
	if err := insertVolunteerJobLinksPostgres(tx, volunteerID, vol.WorkNo, "no"); err != nil {
		return err
	}

	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return fmt.Errorf("commit volunteer registration: %w", err)
	}
	vol.Ref = volunteerID
	vol.Status = status
	return nil
}

func UpdateVolunteerStatus(ctx *config.AppContext, volRef, status string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE volunteers
		SET status = $2
		WHERE id = $1
	`, volRef, status)
	if err != nil {
		return fmt.Errorf("update volunteer %s status: %w", volRef, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("volunteer %s not found", volRef)
	}
	return nil
}

func UpdateVolunteerAvailability(ctx *config.AppContext, volRef string, days []string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE volunteers
		SET availability = $2
		WHERE id = $1
	`, volRef, days)
	if err != nil {
		return fmt.Errorf("update volunteer %s availability: %w", volRef, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("volunteer %s not found", volRef)
	}
	return nil
}

func UpdateVolunteerWorkPrefs(ctx *config.AppContext, volRef string, workYesRefs, workNoRefs []string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return fmt.Errorf("begin volunteer work preference update: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())

	commandTag, err := tx.Exec(ctx.DatabaseContext(), `
		DELETE FROM volunteers_job_types
		WHERE volunteer_id = $1
	`, volRef)
	if err != nil {
		return fmt.Errorf("clear volunteer %s work preferences: %w", volRef, err)
	}

	if commandTag.RowsAffected() == 0 {
		var exists bool
		if err := tx.QueryRow(ctx.DatabaseContext(), `
			SELECT EXISTS(SELECT 1 FROM volunteers WHERE id = $1)
		`, volRef).Scan(&exists); err != nil {
			return fmt.Errorf("check volunteer %s: %w", volRef, err)
		}
		if !exists {
			return fmt.Errorf("volunteer %s not found", volRef)
		}
	}

	if err := insertVolunteerJobRefLinksPostgres(tx, volRef, workYesRefs, "yes"); err != nil {
		return err
	}
	if err := insertVolunteerJobRefLinksPostgres(tx, volRef, workNoRefs, "no"); err != nil {
		return err
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return fmt.Errorf("commit volunteer work preference update: %w", err)
	}
	return nil
}

func GetVolInfos(ctx *config.AppContext, confRef string) ([]*types.VolInfo, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}

	args := []interface{}{}
	where := ""
	if confRef != "" {
		args = append(args, confRef)
		where = "WHERE volunteer_info.conference_id::text = $1"
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT volunteer_info.id::text, volunteer_info.conference_id::text,
			volunteer_info.orient_link_url, volunteer_info.orient_start,
			volunteer_info.orient_end, volunteer_info.notes
		FROM volunteer_info
		`+where+`
		ORDER BY volunteer_info.created_at
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("query volunteer info: %w", err)
	}
	defer rows.Close()

	var out []*types.VolInfo
	for rows.Next() {
		var info types.VolInfo
		var orientStart pgtype.Timestamptz
		var orientEnd pgtype.Timestamptz
		if err := rows.Scan(&info.Ref, &info.ConfRef, &info.OrientLink, &orientStart, &orientEnd, &info.Notes); err != nil {
			return nil, fmt.Errorf("scan volunteer info: %w", err)
		}
		if orientStart.Valid {
			info.OrientTimes = &types.Times{Start: orientStart.Time}
			if orientEnd.Valid {
				info.OrientTimes.End = &orientEnd.Time
			}
		}
		out = append(out, &info)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate volunteer info: %w", err)
	}
	return out, nil
}

func UpdateVolInfoOrientation(ctx *config.AppContext, volInfoRef string, start, end time.Time, orientLink string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if strings.TrimSpace(volInfoRef) == "" {
		return fmt.Errorf("volinfo ref is required")
	}
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE volunteer_info
		SET orient_start = $2, orient_end = $3, orient_link_url = $4
		WHERE id::text = $1
	`, volInfoRef, start, end, strings.TrimSpace(orientLink))
	if err != nil {
		return fmt.Errorf("update volunteer info orientation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("volinfo %s not found", volInfoRef)
	}
	return nil
}

func ListVolunteerApps(ctx *config.AppContext, email string) ([]*types.Volunteer, error) {
	where := ""
	args := []interface{}{}
	if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
		args = append(args, email)
		where = `WHERE volunteer.person_id = (SELECT person_id FROM person_emails WHERE email = $1::citext)`
	}
	return listVolunteersPostgres(ctx, where, args...)
}

func ListVolunteerAppsForPerson(ctx *config.AppContext, personID string) ([]*types.Volunteer, error) {
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return nil, nil
	}
	return listVolunteersPostgres(ctx, "WHERE volunteer.person_id = $1::uuid", personID)
}

func FetchVolunteer(ctx *config.AppContext, volRef string) (*types.Volunteer, error) {
	vols, err := listVolunteersPostgres(ctx, "WHERE volunteer.id::text = $1", volRef)
	if err != nil {
		return nil, err
	}
	if len(vols) == 0 {
		return nil, fmt.Errorf("volunteer %s not found", volRef)
	}
	return vols[0], nil
}

func ListVolunteersForConf(ctx *config.AppContext, confRef string) ([]*types.Volunteer, error) {
	return listVolunteersPostgres(ctx, `
		WHERE volunteer.id IN (
			SELECT volunteer_id
			FROM volunteers_conferences
			WHERE conference_id::text = $1 AND kind = 'schedule_for'
		)
	`, confRef)
}

func listVolunteersPostgres(ctx *config.AppContext, where string, args ...interface{}) ([]*types.Volunteer, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT volunteer.id::text, person.name, coalesce(primary_email.email::text, ''),
			person.phone, person.signal, volunteer.availability, volunteer.contact_at,
			volunteer.comments, volunteer.discovered_via, volunteer.first_event,
			volunteer.hometown, person.twitter_handle, person.nostr, person.tshirt,
			volunteer.status, volunteer.captcha, volunteer.subscribe, volunteer.created_at
		FROM volunteers volunteer
		JOIN people person ON person.id = volunteer.person_id
		LEFT JOIN LATERAL (
			SELECT candidate.email
			FROM (
				SELECT email, CASE WHEN is_primary THEN 0 ELSE 1 END AS priority,
					created_at, id
				FROM person_emails
				WHERE person_id = person.id
				UNION ALL
				SELECT email, 2, detected_at, person.id
				FROM person_email_conflicts
				WHERE person_id = person.id
			) candidate
			ORDER BY candidate.priority, candidate.created_at, candidate.id
			LIMIT 1
		) primary_email ON true
		`+where+`
		ORDER BY volunteer.created_at DESC, person.name
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("query volunteers: %w", err)
	}
	defer rows.Close()

	var out []*types.Volunteer
	for rows.Next() {
		var vol types.Volunteer
		var twitterHandle string
		var createdAt pgtype.Timestamptz
		if err := rows.Scan(
			&vol.Ref,
			&vol.Name,
			&vol.Email,
			&vol.Phone,
			&vol.Signal,
			&vol.Availability,
			&vol.ContactAt,
			&vol.Comments,
			&vol.DiscoveredVia,
			&vol.FirstEvent,
			&vol.Hometown,
			&twitterHandle,
			&vol.Nostr,
			&vol.Shirt,
			&vol.Status,
			&vol.Captcha,
			&vol.Subscribe,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan volunteer: %w", err)
		}
		vol.Twitter = types.ParseTwitter(twitterHandle)
		if createdAt.Valid {
			vol.CreatedAt = &createdAt.Time
		}
		out = append(out, &vol)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate volunteers: %w", err)
	}
	if err := hydrateVolunteerRelationsPostgres(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func hydrateVolunteerRelationsPostgres(ctx *config.AppContext, vols []*types.Volunteer) error {
	if len(vols) == 0 {
		return nil
	}
	volByID := make(map[string]*types.Volunteer, len(vols))
	ids := make([]string, 0, len(vols))
	for _, vol := range vols {
		if vol == nil {
			continue
		}
		volByID[vol.Ref] = vol
		ids = append(ids, vol.Ref)
	}

	confs, err := listConferencesOnlyPostgres(ctx)
	if err != nil {
		return err
	}
	confByID := make(map[string]*types.Conf, len(confs))
	for _, conf := range confs {
		if conf != nil {
			confByID[conf.Ref] = conf
		}
	}
	if err := hydrateVolunteerConferenceRelationsPostgres(ctx, ids, volByID, confByID); err != nil {
		return err
	}

	jobs, err := ListJobTypes(ctx)
	if err != nil {
		return err
	}
	jobByID := make(map[string]*types.JobType, len(jobs))
	for _, job := range jobs {
		if job != nil {
			jobByID[job.Ref] = job
		}
	}
	return hydrateVolunteerJobRelationsPostgres(ctx, ids, volByID, jobByID)
}

func hydrateVolunteerConferenceRelationsPostgres(ctx *config.AppContext, ids []string, volByID map[string]*types.Volunteer, confByID map[string]*types.Conf) error {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT volunteer_id::text, conference_id::text, kind
		FROM volunteers_conferences
		WHERE volunteer_id::text = ANY($1::text[])
		ORDER BY kind
	`, ids)
	if err != nil {
		return fmt.Errorf("query volunteer conference links: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var volunteerID string
		var confID string
		var kind string
		if err := rows.Scan(&volunteerID, &confID, &kind); err != nil {
			return fmt.Errorf("scan volunteer conference link: %w", err)
		}
		vol := volByID[volunteerID]
		conf := confByID[confID]
		if vol == nil || conf == nil {
			continue
		}
		switch kind {
		case "schedule_for":
			vol.ScheduleFor = append(vol.ScheduleFor, conf)
		case "other_event":
			vol.OtherEvents = append(vol.OtherEvents, conf)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate volunteer conference links: %w", err)
	}
	return nil
}

func insertVolunteerConferenceLinksPostgres(tx pgx.Tx, volunteerID string, confs []*types.Conf, kind string) error {
	for _, conf := range confs {
		if conf == nil || strings.TrimSpace(conf.Ref) == "" {
			continue
		}
		if _, err := tx.Exec(config.DatabaseContext(), `
			INSERT INTO volunteers_conferences (volunteer_id, conference_id, kind)
			VALUES ($1, $2, $3)
			ON CONFLICT (volunteer_id, conference_id, kind) DO NOTHING
		`, volunteerID, conf.Ref, kind); err != nil {
			return fmt.Errorf("insert volunteer conference link %s/%s/%s: %w", volunteerID, conf.Ref, kind, err)
		}
	}
	return nil
}

func insertVolunteerJobLinksPostgres(tx pgx.Tx, volunteerID string, jobs []*types.JobType, preference string) error {
	refs := make([]string, 0, len(jobs))
	for _, job := range jobs {
		if job == nil || strings.TrimSpace(job.Ref) == "" {
			continue
		}
		refs = append(refs, job.Ref)
	}
	return insertVolunteerJobRefLinksPostgres(tx, volunteerID, refs, preference)
}

func insertVolunteerJobRefLinksPostgres(tx pgx.Tx, volunteerID string, jobRefs []string, preference string) error {
	for _, jobRef := range jobRefs {
		jobRef = strings.TrimSpace(jobRef)
		if jobRef == "" {
			continue
		}
		if _, err := tx.Exec(config.DatabaseContext(), `
			INSERT INTO volunteers_job_types (volunteer_id, job_type_id, preference)
			VALUES ($1, $2, $3)
			ON CONFLICT (volunteer_id, job_type_id, preference) DO NOTHING
		`, volunteerID, jobRef, preference); err != nil {
			return fmt.Errorf("insert volunteer job link %s/%s/%s: %w", volunteerID, jobRef, preference, err)
		}
	}
	return nil
}

func hydrateVolunteerJobRelationsPostgres(ctx *config.AppContext, ids []string, volByID map[string]*types.Volunteer, jobByID map[string]*types.JobType) error {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT volunteer_id::text, job_type_id::text, preference
		FROM volunteers_job_types
		WHERE volunteer_id::text = ANY($1::text[])
		ORDER BY preference
	`, ids)
	if err != nil {
		return fmt.Errorf("query volunteer job links: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var volunteerID string
		var jobID string
		var preference string
		if err := rows.Scan(&volunteerID, &jobID, &preference); err != nil {
			return fmt.Errorf("scan volunteer job link: %w", err)
		}
		vol := volByID[volunteerID]
		job := jobByID[jobID]
		if vol == nil || job == nil {
			continue
		}
		switch preference {
		case "yes":
			vol.WorkYes = append(vol.WorkYes, job)
		case "no":
			vol.WorkNo = append(vol.WorkNo, job)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate volunteer job links: %w", err)
	}
	return nil
}
