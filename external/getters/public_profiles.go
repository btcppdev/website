package getters

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5/pgtype"
)

// PublicProfile is the small, public projection needed by /whois. It avoids
// hydrating the much larger admin/dashboard object graph for every profile hit.
type PublicProfile struct {
	Speaker  *types.Speaker
	Talks    []*PublicProfileTalk
	Editions []*types.Conf
}

type PublicProfileTalk struct {
	Talk *types.Talk
	Conf *types.Conf
}

// ListPublicProfiles loads the complete public directory in two set-based
// queries: talks (including speakers + recordings) and ticket attendance.
// The old path used the general-purpose graph loaders and performed an extra
// recording query for every talk.
func ListPublicProfiles(ctx *config.AppContext) ([]*PublicProfile, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}

	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT
			person.id::text, person.name, coalesce(person.email::text, ''),
			person.norm_photo_path, person.phone, person.signal, person.telegram,
			person.twitter_handle, person.nostr, person.github_url, person.instagram,
			person.linkedin, person.leetcode, person.website_url, person.company,
			person.bio, person.org_logo_path, person.avail_to_hire,
			person.looking_to_hire, person.tshirt,
			sc.company, sc.org_photo_path, sc.record_ok,
			ct.id::text, ct.clipart_path, ct.scheduled_start, ct.scheduled_end,
			ct.venue, ct.section, ct.cal_notif, ct.social_card_path,
			ct.github_repo_url, ct.slides_url, ct.slides_object_key,
			proposal.title, proposal.description, proposal.talk_type, proposal.status,
			conf.id::text, conf.tag, conf.active, conf.publication_status,
			conf.description, conf.edition_type, conf.og_flavor, conf.emoji,
			conf.tagline, conf.date_desc, conf.start_date, conf.end_date,
			conf.timezone, conf.location,
			coalesce(recording.youtube_url, '')
		FROM conf_talks ct
		JOIN proposals proposal ON proposal.id = ct.proposal_id
		JOIN proposals_speaker_confs psc ON psc.proposal_id = proposal.id
		JOIN speaker_confs sc ON sc.id = psc.speaker_conf_id
		JOIN people person ON person.id = sc.speaker_id
		JOIN conferences conf ON conf.id = ct.conference_id
		LEFT JOIN recordings recording ON recording.conf_talk_id = ct.id
		WHERE ct.archived_at IS NULL
		  AND proposal.status IN ('', 'Accepted', 'Scheduled')
		  AND conf.publication_status = 'published'
		ORDER BY conf.start_date DESC NULLS LAST, ct.id, person.name, person.id
	`)
	if err != nil {
		return nil, fmt.Errorf("query public profile talks: %w", err)
	}
	defer rows.Close()

	peopleByID := map[string]*PublicProfile{}
	talkByID := map[string]*types.Talk{}
	confByID := map[string]*types.Conf{}
	personTalkSeen := map[string]bool{}
	talkSpeakerSeen := map[string]bool{}
	editionSeen := map[string]bool{}

	for rows.Next() {
		var speaker types.Speaker
		var twitter, speakerConfCompany, speakerConfOrg, recordOK string
		var talkID, clipart, venue, section, calNotif, socialCard string
		var githubRepo, slidesURL, slidesObjectKey string
		var title, description, talkType, status, recordingURL string
		var scheduledStart, scheduledEnd pgtype.Timestamptz
		var conf types.Conf
		var confStart, confEnd pgtype.Timestamptz
		if err := rows.Scan(
			&speaker.ID, &speaker.Name, &speaker.Email, &speaker.Photo,
			&speaker.Phone, &speaker.Signal, &speaker.Telegram, &twitter,
			&speaker.Nostr, &speaker.Github, &speaker.Instagram, &speaker.LinkedIn,
			&speaker.LeetCode, &speaker.Website, &speaker.Company, &speaker.Bio,
			&speaker.OrgLogo, &speaker.AvailToHire, &speaker.LookingToHire, &speaker.TShirt,
			&speakerConfCompany, &speakerConfOrg, &recordOK,
			&talkID, &clipart, &scheduledStart, &scheduledEnd, &venue, &section,
			&calNotif, &socialCard, &githubRepo, &slidesURL, &slidesObjectKey,
			&title, &description, &talkType, &status,
			&conf.Ref, &conf.Tag, &conf.Active, &conf.PublicationStatus,
			&conf.Desc, &conf.EditionType, &conf.OGFlavor, &conf.Emoji,
			&conf.Tagline, &conf.DateDesc, &confStart, &confEnd,
			&conf.Timezone, &conf.Location, &recordingURL,
		); err != nil {
			return nil, fmt.Errorf("scan public profile talk: %w", err)
		}
		speaker.Twitter = types.ParseTwitter(twitter)
		profile := peopleByID[speaker.ID]
		if profile == nil {
			profile = &PublicProfile{Speaker: &speaker}
			peopleByID[speaker.ID] = profile
		}

		confView := confByID[conf.Ref]
		if confView == nil {
			finishPublicProfileConf(&conf, confStart, confEnd)
			confView = &conf
			confByID[conf.Ref] = confView
		}

		talk := talkByID[talkID]
		if talk == nil {
			talk = &types.Talk{
				ID: talkID, Name: title, Description: description, Type: talkType,
				Status: status, Clipart: clipart, Venue: venue, Section: section,
				CalNotif: calNotif, TalkCardURL: socialCard, Event: confView.Tag,
				YTLink: recordingURL, GithubRepoURL: githubRepo, SlidesURL: slidesURL,
				SlidesObjectKey: slidesObjectKey,
			}
			if scheduledStart.Valid {
				start := scheduledStart.Time.In(confView.Loc())
				talk.Sched = &types.Times{Start: start}
				if scheduledEnd.Valid {
					end := scheduledEnd.Time.In(confView.Loc())
					talk.Sched.End = &end
				}
				talk.TimeDesc = talk.Sched.Desc()
				talk.Duration = talk.Sched.LenStr()
			}
			talkByID[talkID] = talk
		}

		speakerKey := talkID + "\x00" + speaker.ID
		if !talkSpeakerSeen[speakerKey] {
			view := speaker
			if strings.TrimSpace(speakerConfCompany) != "" {
				view.Company = speakerConfCompany
			}
			if strings.TrimSpace(speakerConfOrg) != "" {
				view.OrgLogo = speakerConfOrg
			}
			view.RecordingEmoji = recordingEmojiForRecordOK(recordOK)
			switch view.RecordingEmoji {
			case "🔇":
				talk.RecordingAudioOnly = true
			case "🛑":
				talk.RecordingRestricted = true
			}
			talk.Speakers = append(talk.Speakers, &view)
			talkSpeakerSeen[speakerKey] = true
		}

		personTalkKey := speaker.ID + "\x00" + talkID
		if !personTalkSeen[personTalkKey] {
			profile.Talks = append(profile.Talks, &PublicProfileTalk{Talk: talk, Conf: confView})
			personTalkSeen[personTalkKey] = true
		}
		addPublicProfileEdition(profile, confView, editionSeen)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate public profile talks: %w", err)
	}
	rows.Close()

	personIDs := make([]string, 0, len(peopleByID))
	for id := range peopleByID {
		personIDs = append(personIDs, id)
	}
	if len(personIDs) > 0 {
		if err := addPublicProfileAttendance(ctx, personIDs, peopleByID, confByID, editionSeen); err != nil {
			return nil, err
		}
	}

	profiles := make([]*PublicProfile, 0, len(peopleByID))
	for _, profile := range peopleByID {
		sort.SliceStable(profile.Editions, func(i, j int) bool {
			return profile.Editions[i].StartDate.After(profile.Editions[j].StartDate)
		})
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func addPublicProfileAttendance(ctx *config.AppContext, personIDs []string, people map[string]*PublicProfile, confs map[string]*types.Conf, seen map[string]bool) error {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT DISTINCT
			person.id::text,
			conf.id::text, conf.tag, conf.active, conf.publication_status,
			conf.description, conf.edition_type, conf.og_flavor, conf.emoji,
			conf.tagline, conf.date_desc, conf.start_date, conf.end_date,
			conf.timezone, conf.location
		FROM people person
		JOIN registrations registration ON registration.email = person.email
		JOIN conferences conf ON conf.id = registration.conference_id
		WHERE person.id::text = ANY($1::text[])
		  AND registration.revoked = false
		  AND conf.publication_status = 'published'
	`, personIDs)
	if err != nil {
		return fmt.Errorf("query public profile attendance: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var personID string
		var conf types.Conf
		var start, end pgtype.Timestamptz
		if err := rows.Scan(
			&personID, &conf.Ref, &conf.Tag, &conf.Active, &conf.PublicationStatus,
			&conf.Desc, &conf.EditionType, &conf.OGFlavor, &conf.Emoji,
			&conf.Tagline, &conf.DateDesc, &start, &end, &conf.Timezone, &conf.Location,
		); err != nil {
			return fmt.Errorf("scan public profile attendance: %w", err)
		}
		confView := confs[conf.Ref]
		if confView == nil {
			finishPublicProfileConf(&conf, start, end)
			confView = &conf
			confs[conf.Ref] = confView
		}
		if profile := people[personID]; profile != nil {
			addPublicProfileEdition(profile, confView, seen)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate public profile attendance: %w", err)
	}
	return nil
}

func finishPublicProfileConf(conf *types.Conf, start, end pgtype.Timestamptz) {
	if conf.Timezone != "" {
		if loc, err := time.LoadLocation(conf.Timezone); err == nil {
			conf.TZ = loc
		}
	}
	if start.Valid {
		conf.StartDate = start.Time.In(conf.Loc())
	}
	if end.Valid {
		conf.EndDate = end.Time.In(conf.Loc())
	}
}

func addPublicProfileEdition(profile *PublicProfile, conf *types.Conf, seen map[string]bool) {
	if profile == nil || profile.Speaker == nil || conf == nil {
		return
	}
	key := profile.Speaker.ID + "\x00" + conf.Ref
	if seen[key] {
		return
	}
	seen[key] = true
	profile.Editions = append(profile.Editions, conf)
}
