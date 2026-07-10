package main

import (
	"context"
	"fmt"
	"log"

	"btcpp-web/internal/db"
	"btcpp-web/internal/envconfig"

	"github.com/jackc/pgx/v5"
)

const (
	devConfID      = "00000000-0000-4000-8000-000000000001"
	devPastConfID  = "00000000-0000-4000-8000-000000000002"
	devDraftConfID = "00000000-0000-4000-8000-000000000003"
	devLocalConfID = "00000000-0000-4000-8000-000000000004"

	devDay1ID = "00000000-0000-4000-8000-000000000011"
	devDay2ID = "00000000-0000-4000-8000-000000000012"
	devDay3ID = "00000000-0000-4000-8000-000000000013"

	devEarlyTixID           = "00000000-0000-4000-8000-000000000021"
	devGeneralTixID         = "00000000-0000-4000-8000-000000000022"
	devAdminID              = "00000000-0000-4000-8000-000000000031"
	devArchiveSpeakerConfID = "00000000-0000-4000-8000-000000000033"
	devArchiveProposalID    = "00000000-0000-4000-8000-000000000034"
	devArchiveTalkID        = "00000000-0000-4000-8000-000000000035"
	devArchiveRecordingID   = "00000000-0000-4000-8000-000000000036"
	devAffiliateDiscountID  = "00000000-0000-4000-8000-000000000037"
	devVolunteerID          = "00000000-0000-4000-8000-000000000038"
	devJobTypeID            = "00000000-0000-4000-8000-000000000039"
	devWorkShiftID          = "00000000-0000-4000-8000-000000000040"
	devVolInfoID            = "00000000-0000-4000-8000-000000000041"
	devHomepagePerson7ID    = "00000000-0000-4000-8000-000000000042"
	devHomepagePerson8ID    = "00000000-0000-4000-8000-000000000043"
)

type daySeed struct {
	id                           string
	number                       int
	doorsStart, doorsEnd         string
	breakfastStart, breakfastEnd string
	lunchStart, lunchEnd         string
	coffeeStart, coffeeEnd       string
	venues                       []string
}

type ticketSeed struct {
	id, key, tier, expiresStart, currency string
	local, btc, usd, max                  int
	symbol, postSymbol                    string
}

type speakerSeed struct {
	personID, speakerConfID, proposalID, talkID string
	name, email, photo                          string
	company, twitter, github, website           string
	comingFrom                                  string
	title, description, talkType                string
	start, end, venue, clipart                  string
	duration                                    int
}

type orgSeed struct {
	id, name, tagline, logo, website, twitter string
}

type sponsorshipSeed struct {
	id, orgID, level, label, status string
}

type hotelSeed struct {
	id, name, url, img, hotelType, desc string
	order                               int
}

type satelliteSeed struct {
	id, title, description, eventURL, eventType string
	start, end, location, imageURL              string
	hostName, hostURL, hostLogoURL              string
}

var devDays = []daySeed{
	{
		id:             devDay1ID,
		number:         1,
		doorsStart:     "09:00",
		doorsEnd:       "18:00",
		breakfastStart: "09:00",
		breakfastEnd:   "10:00",
		lunchStart:     "12:30",
		lunchEnd:       "13:30",
		coffeeStart:    "15:00",
		coffeeEnd:      "15:30",
		venues:         []string{"one", "two", "three"},
	},
	{
		id:             devDay2ID,
		number:         2,
		doorsStart:     "09:30",
		doorsEnd:       "18:30",
		breakfastStart: "09:30",
		breakfastEnd:   "10:00",
		lunchStart:     "12:30",
		lunchEnd:       "13:30",
		coffeeStart:    "15:15",
		coffeeEnd:      "15:45",
		venues:         []string{"one", "two", "three"},
	},
	{
		id:             devDay3ID,
		number:         3,
		doorsStart:     "10:00",
		doorsEnd:       "16:00",
		breakfastStart: "10:00",
		breakfastEnd:   "10:30",
		lunchStart:     "12:30",
		lunchEnd:       "13:30",
		coffeeStart:    "14:45",
		coffeeEnd:      "15:15",
		venues:         []string{"one", "three"},
	},
}

var devTickets = []ticketSeed{
	{
		id:           devEarlyTixID,
		key:          "early",
		tier:         "Early Bird All Conference Pass",
		local:        75,
		btc:          75,
		usd:          95,
		expiresStart: "2026-09-15 00:00:00-05",
		max:          25,
		currency:     "usd",
		symbol:       "$",
	},
	{
		id:           devGeneralTixID,
		key:          "general",
		tier:         "General Admission",
		local:        120,
		btc:          120,
		usd:          150,
		expiresStart: "2027-01-01 00:00:00+00",
		max:          150,
		currency:     "usd",
		symbol:       "$",
	},
}

var devSpeakers = []speakerSeed{
	{
		personID:      "00000000-0000-4000-8000-000000000101",
		speakerConfID: "00000000-0000-4000-8000-000000000201",
		proposalID:    "00000000-0000-4000-8000-000000000301",
		talkID:        "00000000-0000-4000-8000-000000000401",
		name:          "Mara Chen",
		email:         "mara.chen@example.test",
		photo:         "../static/img/julien.jpg",
		company:       "Signet Systems",
		twitter:       "mara_signet",
		github:        "https://github.com/example/mara-signet",
		website:       "https://example.test/mara",
		comingFrom:    "Vancouver, Canada",
		title:         "Package Relay in Practice",
		description:   "A practical tour through package relay assumptions, fee bumping edge cases, and the testing fixtures that keep mempool behavior understandable.",
		talkType:      "talk",
		start:         "2026-10-01 10:00:00-05",
		end:           "2026-10-01 10:45:00-05",
		venue:         "one",
		clipart:       "../static/img/floripa26/leading.png",
		duration:      45,
	},
	{
		personID:      "00000000-0000-4000-8000-000000000102",
		speakerConfID: "00000000-0000-4000-8000-000000000202",
		proposalID:    "00000000-0000-4000-8000-000000000302",
		talkID:        "00000000-0000-4000-8000-000000000402",
		name:          "Eli Turner",
		email:         "eli.turner@example.test",
		photo:         "../static/img/jonasnick.jpeg",
		company:       "Anchor Labs",
		twitter:       "eli_turner",
		github:        "https://github.com/example/eli-turner",
		website:       "https://example.test/eli",
		comingFrom:    "Austin, TX",
		title:         "Building a Signet-First Release Pipeline",
		description:   "How to keep local, signet, and production deployments aligned without making every contributor run the entire stack.",
		talkType:      "workshop",
		start:         "2026-10-01 11:15:00-05",
		end:           "2026-10-01 12:30:00-05",
		venue:         "three",
		clipart:       "../static/img/floripa26/leading_two.png",
		duration:      75,
	},
	{
		personID:      "00000000-0000-4000-8000-000000000103",
		speakerConfID: "00000000-0000-4000-8000-000000000203",
		proposalID:    "00000000-0000-4000-8000-000000000303",
		talkID:        "00000000-0000-4000-8000-000000000403",
		name:          "Priya Shah",
		email:         "priya.shah@example.test",
		photo:         "../static/img/pavol.jpg",
		company:       "Relay Club",
		twitter:       "priya_relay",
		github:        "https://github.com/example/priya-relay",
		website:       "https://example.test/priya",
		comingFrom:    "New York, NY",
		title:         "Designing Good Failure Modes for Lightning Apps",
		description:   "Patterns for making channel state, payment retries, and mobile reconnects visible enough for users and operators to recover.",
		talkType:      "talk",
		start:         "2026-10-01 14:00:00-05",
		end:           "2026-10-01 14:45:00-05",
		venue:         "two",
		clipart:       "../static/img/taipei/logo_1080p.png",
		duration:      45,
	},
	{
		personID:      "00000000-0000-4000-8000-000000000104",
		speakerConfID: "00000000-0000-4000-8000-000000000204",
		proposalID:    "00000000-0000-4000-8000-000000000304",
		talkID:        "00000000-0000-4000-8000-000000000404",
		name:          "Jon Bell",
		email:         "jon.bell@example.test",
		photo:         "../static/img/mc.jpg",
		company:       "Node House",
		twitter:       "jon_nodes",
		github:        "https://github.com/example/jon-nodes",
		website:       "https://example.test/jon",
		comingFrom:    "Denver, CO",
		title:         "Hardware Wallet UX for Power Users",
		description:   "A review of multisig onboarding, descriptor backups, and how advanced wallet flows can be made safer without hiding the important bits.",
		talkType:      "talk",
		start:         "2026-10-02 10:00:00-05",
		end:           "2026-10-02 10:45:00-05",
		venue:         "one",
		clipart:       "../static/img/taipei/bnw.png",
		duration:      45,
	},
	{
		personID:      "00000000-0000-4000-8000-000000000105",
		speakerConfID: "00000000-0000-4000-8000-000000000205",
		proposalID:    "00000000-0000-4000-8000-000000000305",
		talkID:        "00000000-0000-4000-8000-000000000405",
		name:          "Samira Cole",
		email:         "samira.cole@example.test",
		photo:         "../static/img/corn.jpg",
		company:       "Mempool Tools",
		twitter:       "samira_mempool",
		github:        "https://github.com/example/samira-mempool",
		website:       "https://example.test/samira",
		comingFrom:    "Chicago, IL",
		title:         "Observability for Bitcoin Services",
		description:   "Metrics, alerts, and dashboards that help operators separate normal chain weirdness from issues they should wake up for.",
		talkType:      "workshop",
		start:         "2026-10-02 11:15:00-05",
		end:           "2026-10-02 12:30:00-05",
		venue:         "three",
		clipart:       "../static/img/vienna/btc_austria.png",
		duration:      75,
	},
	{
		personID:      "00000000-0000-4000-8000-000000000106",
		speakerConfID: "00000000-0000-4000-8000-000000000206",
		proposalID:    "00000000-0000-4000-8000-000000000306",
		talkID:        "00000000-0000-4000-8000-000000000406",
		name:          "Rafael Silva",
		email:         "rafael.silva@example.test",
		photo:         "../static/img/ninja_default.jpeg",
		company:       "FOSS Operations",
		twitter:       "rafael_ops",
		github:        "https://github.com/example/rafael-ops",
		website:       "https://example.test/rafael",
		comingFrom:    "Sao Paulo, Brazil",
		title:         "Async Payments and Mobile Reliability",
		description:   "A panel on the tradeoffs between background services, blinded paths, offline notifications, and the mobile constraints users actually have.",
		talkType:      "panel",
		start:         "2026-10-02 14:00:00-05",
		end:           "2026-10-02 15:00:00-05",
		venue:         "two",
		clipart:       "../static/img/toronto/og_card_standard.png",
		duration:      60,
	},
}

var devOrgs = []orgSeed{
	{
		id:      "00000000-0000-4000-8000-000000000501",
		name:    "Signet Systems",
		tagline: "Infrastructure for bitcoin test networks",
		logo:    "/static/img/sponsors/NYDIG.svg",
		website: "https://example.test/signet-systems",
		twitter: "signet_systems",
	},
	{
		id:      "00000000-0000-4000-8000-000000000502",
		name:    "Anchor Labs",
		tagline: "Protocol engineering and applied research",
		logo:    "/static/img/sponsors/vinteum.png",
		website: "https://example.test/anchor-labs",
		twitter: "anchor_labs",
	},
	{
		id:      "00000000-0000-4000-8000-000000000503",
		name:    "Relay Club",
		tagline: "Developer tooling for routing and payments",
		logo:    "/static/img/sponsors/stak.svg",
		website: "https://example.test/relay-club",
		twitter: "relay_club",
	},
	{
		id:      "00000000-0000-4000-8000-000000000504",
		name:    "Mempool Tools",
		tagline: "Operational visibility for bitcoin services",
		logo:    "/static/img/mempool.svg",
		website: "https://example.test/mempool-tools",
		twitter: "mempool_tools",
	},
	{
		id:      "00000000-0000-4000-8000-000000000505",
		name:    "Node House",
		tagline: "Open hardware for node runners",
		logo:    "/static/img/sponsors/bitvmx.png",
		website: "https://example.test/node-house",
		twitter: "node_house",
	},
}

var devSponsorships = []sponsorshipSeed{
	{
		id:     "00000000-0000-4000-8000-000000000601",
		orgID:  "00000000-0000-4000-8000-000000000501",
		level:  "Headline",
		label:  "Headline Sponsors",
		status: "Paid",
	},
	{
		id:     "00000000-0000-4000-8000-000000000602",
		orgID:  "00000000-0000-4000-8000-000000000502",
		level:  "Diamond",
		label:  "Diamond Sponsors",
		status: "Committed",
	},
	{
		id:     "00000000-0000-4000-8000-000000000603",
		orgID:  "00000000-0000-4000-8000-000000000503",
		level:  "Workshop",
		label:  "Workshop Sponsors",
		status: "Paid",
	},
	{
		id:     "00000000-0000-4000-8000-000000000604",
		orgID:  "00000000-0000-4000-8000-000000000504",
		level:  "Gold",
		label:  "Gold Sponsors",
		status: "Committed",
	},
	{
		id:     "00000000-0000-4000-8000-000000000605",
		orgID:  "00000000-0000-4000-8000-000000000505",
		level:  "Community",
		label:  "Community Sponsors",
		status: "Paid",
	},
}

var devHotels = []hotelSeed{
	{
		id:        "00000000-0000-4000-8000-000000000701",
		name:      "The Annex Hotel",
		url:       "https://example.test/dev26/hotels/annex",
		img:       "static/img/toronto/sonder.webp",
		hotelType: "Hotel",
		desc:      "Walkable rooms near the venue with enough desk space to keep hacking after the talks.",
		order:     1,
	},
	{
		id:        "00000000-0000-4000-8000-000000000702",
		name:      "Congress House",
		url:       "https://example.test/dev26/hotels/congress",
		img:       "static/img/palmer_night.jpg",
		hotelType: "Boutique",
		desc:      "A smaller stay option close to food, coffee, and the evening satellite events.",
		order:     2,
	},
	{
		id:        "00000000-0000-4000-8000-000000000703",
		name:      "Node Hostel",
		url:       "https://example.test/dev26/hotels/node-hostel",
		img:       "static/img/selina.webp",
		hotelType: "Hostel",
		desc:      "Budget-friendly shared rooms for attendees who want to spend more on hardware and less on lodging.",
		order:     3,
	},
}

var devSatellites = []satelliteSeed{
	{
		id:          "00000000-0000-4000-8000-000000000801",
		title:       "Austin BitDevs Socratic Seminar",
		description: "An evening review of recent mailing list threads, mempool policy proposals, and releases worth testing before the conference starts.",
		eventURL:    "https://example.test/dev26/bitdevs",
		eventType:   "BitDevs",
		start:       "2026-10-01 19:00:00-05",
		end:         "2026-10-01 21:00:00-05",
		location:    "East Side Coffee",
		imageURL:    "static/img/floripa26/bitdevs.webp",
		hostName:    "Austin BitDevs",
		hostURL:     "https://example.test/austin-bitdevs",
		hostLogoURL: "static/img/logo_blk.svg",
	},
	{
		id:          "00000000-0000-4000-8000-000000000802",
		title:       "Signet Hack Night",
		description: "Bring a laptop, clone the local harness, and pair up on small fixes that make bitcoin apps easier to run in development.",
		eventURL:    "https://example.test/dev26/hack-night",
		eventType:   "Hackathon",
		start:       "2026-10-02 18:30:00-05",
		end:         "2026-10-02 22:00:00-05",
		location:    "Localhost Hall Workshop Room",
		imageURL:    "static/img/austin/pleblabhack.jpg",
		hostName:    "Signet Systems",
		hostURL:     "https://example.test/signet-systems",
		hostLogoURL: "static/img/sponsors/NYDIG.svg",
	},
	{
		id:          "00000000-0000-4000-8000-000000000803",
		title:       "Closing Dinner and Demo Hour",
		description: "A casual dinner where attendees can show what they built, compare notes, and trade testnet war stories before heading home.",
		eventURL:    "https://example.test/dev26/closing",
		eventType:   "Dinner",
		start:       "2026-10-03 18:00:00-05",
		end:         "2026-10-03 20:30:00-05",
		location:    "Congress Avenue",
		imageURL:    "static/img/stacks_drinks.avif",
		hostName:    "bitcoin++",
		hostURL:     "https://btcpp.dev",
		hostLogoURL: "static/img/logo_blk.svg",
	},
}

func main() {
	ctx := context.Background()
	env, err := envconfig.Load(".env")
	if err != nil {
		log.Fatal(err)
	}
	if env.Prod {
		log.Fatal("refusing to seed while PROD=true")
	}

	pool, err := db.Open(ctx, env.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(ctx)

	confID := seedConference(ctx, tx)
	pastConfID := seedRedesignConferences(ctx, tx)
	seedConferenceDays(ctx, tx, confID)
	seedTickets(ctx, tx, confID)
	seedAdmin(ctx, tx)
	seedProgram(ctx, tx, confID)
	seedSponsors(ctx, tx, confID)
	seedHotels(ctx, tx, confID)
	seedSatelliteEvents(ctx, tx, confID)
	seedHomepageFeaturedSpeakers(ctx, tx)
	seedDashboardFixtures(ctx, tx, confID, pastConfID)

	if err := tx.Commit(ctx); err != nil {
		log.Fatal(err)
	}

	log.Printf("seeded local dev conferences and dashboard fixture for dev-admin@example.test")
}

func seedConference(ctx context.Context, tx pgx.Tx) string {
	var confID string
	err := tx.QueryRow(ctx, `
		INSERT INTO conferences (
			id, tag, public_uid, active, publication_status, description, edition_type, og_flavor, emoji, tagline,
			date_desc, start_date, end_date, timezone, location, venue,
			venue_map_url, venue_website_url, show_hackathon, orient_cal_notif,
			hero_title, hero_caption, about_title, about_body, about_body_2,
			venue_title, venue_subtitle, venue_body, hotels_intro, local_ticket_body,
			speakers_title, speakers_body, map_embed_url,
			map_latitude, map_longitude, map_x_percent, map_y_percent, map_label, map_label_side
		)
		VALUES (
			$1::uuid, 'dev26', 260001, true, 'published', 'bitcoin++ Local Dev 2026, signet edition', 'global',
			'A realistic local event fixture for developing tickets, talks, sponsors, hotels, satellites, and admin flows without production data.',
			'++', 'signet edition',
			'Oct 1 - 3, 2026', '2026-10-01 09:00:00-05', '2026-10-03 17:00:00-05',
			'America/Chicago', 'Austin, TX', 'Localhost Hall',
			'https://maps.example.test/localhost-hall', 'https://example.test/localhost-hall',
			true, '',
			'<span class="font-bitcoin">bitcoin++</span> local dev',
			'A full local fixture for the redesigned public event page.',
			'Build with real fixture data',
			'This local edition seeds the content, talks, tickets, sponsors, map data, and dashboard relationships the redesigned site expects.',
			'Use it to catch public-page regressions before production data is involved.',
			'Localhost Hall',
			'Austin, Texas',
			'Three days of protocol work, workshops, and hallway conversations in a deterministic development fixture.',
			'Nearby dev-friendly hotels seeded for layout testing.',
			'Local tickets are available for this development edition.',
			'Expert Speakers',
			'Fixture speakers with photos, talks, clipart, and scheduling data.',
			'https://www.google.com/maps/embed/v1/place?q=Austin%2C%20TX&key=dev',
			30.2672, -97.7431, 23.5, 47.5, 'Austin', 'right'
		)
		ON CONFLICT (tag) DO UPDATE SET
			active = EXCLUDED.active,
			publication_status = EXCLUDED.publication_status,
			description = EXCLUDED.description,
			edition_type = EXCLUDED.edition_type,
			og_flavor = EXCLUDED.og_flavor,
			emoji = EXCLUDED.emoji,
			tagline = EXCLUDED.tagline,
			date_desc = EXCLUDED.date_desc,
			start_date = EXCLUDED.start_date,
			end_date = EXCLUDED.end_date,
			timezone = EXCLUDED.timezone,
			location = EXCLUDED.location,
			venue = EXCLUDED.venue,
			venue_map_url = EXCLUDED.venue_map_url,
			venue_website_url = EXCLUDED.venue_website_url,
			show_hackathon = EXCLUDED.show_hackathon,
			hero_title = EXCLUDED.hero_title,
			hero_caption = EXCLUDED.hero_caption,
			about_title = EXCLUDED.about_title,
			about_body = EXCLUDED.about_body,
			about_body_2 = EXCLUDED.about_body_2,
			venue_title = EXCLUDED.venue_title,
			venue_subtitle = EXCLUDED.venue_subtitle,
			venue_body = EXCLUDED.venue_body,
			hotels_intro = EXCLUDED.hotels_intro,
			local_ticket_body = EXCLUDED.local_ticket_body,
			speakers_title = EXCLUDED.speakers_title,
			speakers_body = EXCLUDED.speakers_body,
			map_embed_url = EXCLUDED.map_embed_url,
			map_latitude = EXCLUDED.map_latitude,
			map_longitude = EXCLUDED.map_longitude,
			map_x_percent = EXCLUDED.map_x_percent,
			map_y_percent = EXCLUDED.map_y_percent,
			map_label = EXCLUDED.map_label,
			map_label_side = EXCLUDED.map_label_side
		RETURNING id::text
	`, devConfID).Scan(&confID)
	if err != nil {
		log.Fatal(fmt.Errorf("seed conference: %w", err))
	}
	return confID
}

func seedRedesignConferences(ctx context.Context, tx pgx.Tx) string {
	type confSeed struct {
		id, tag, status, desc, tagline, dateDesc, start, end string
		location, venue, editionType                         string
		lat, lng, x, y                                       float64
		label, side                                          string
		active                                               bool
	}
	confs := []confSeed{
		{
			id:          devPastConfID,
			tag:         "archive24",
			status:      "published",
			desc:        "bitcoin++ Austin 2024, archive fixture",
			tagline:     "bitcoin script edition",
			dateDesc:    "May 1 - 2, 2024",
			start:       "2024-05-01 09:00:00-05",
			end:         "2024-05-02 17:00:00-05",
			location:    "Austin, TX",
			venue:       "Archive Hall",
			editionType: "global",
			lat:         30.2672,
			lng:         -97.7431,
			x:           23.5,
			y:           47.5,
			label:       "Austin",
			side:        "right",
			active:      false,
		},
		{
			id:          devDraftConfID,
			tag:         "draft27",
			status:      "draft",
			desc:        "bitcoin++ Draft 2027, unpublished fixture",
			tagline:     "draft edition",
			dateDesc:    "Feb 4 - 5, 2027",
			start:       "2027-02-04 09:00:00+00",
			end:         "2027-02-05 17:00:00+00",
			location:    "Madeira, Portugal",
			venue:       "Draft Venue",
			editionType: "global",
			lat:         32.7607,
			lng:         -16.9595,
			x:           45.5,
			y:           40.2,
			label:       "Madeira",
			side:        "left",
			active:      false,
		},
		{
			id:          devLocalConfID,
			tag:         "local25",
			status:      "published",
			desc:        "bitcoin++ Durham 2025, local edition fixture",
			tagline:     "local edition",
			dateDesc:    "Nov 8, 2025",
			start:       "2025-11-08 10:00:00-05",
			end:         "2025-11-08 18:00:00-05",
			location:    "Durham, NC",
			venue:       "Local Edition Hall",
			editionType: "local",
			lat:         35.9940,
			lng:         -78.8986,
			x:           27.8,
			y:           48.9,
			label:       "Durham",
			side:        "right",
			active:      false,
		},
	}

	for _, conf := range confs {
		mustExec(ctx, tx, "seed redesign conference", `
			INSERT INTO conferences (
				id, tag, public_uid, active, publication_status, description, edition_type,
				og_flavor, emoji, tagline, date_desc, start_date, end_date, timezone,
				location, venue, venue_map_url, venue_website_url, show_hackathon,
				orient_cal_notif, hero_title, hero_caption, about_title, about_body,
				about_body_2, venue_title, venue_subtitle, venue_body, hotels_intro,
				local_ticket_body, speakers_title, speakers_body, map_embed_url,
				map_latitude, map_longitude, map_x_percent, map_y_percent, map_label,
				map_label_side
			)
			VALUES (
				$1::uuid, $2, NULL, $3, $4, $5, $6,
				'Development fixture conference', '++', $7, $8, $9::timestamptz,
				$10::timestamptz, 'America/Chicago', $11, $12,
				'https://maps.example.test/dev-fixture', 'https://example.test/dev-fixture',
				false, '', $13, $14, 'Fixture overview',
				'Seeded by cmd/dev-seed so redesign pages have realistic conference copy.',
				'This event exists to exercise timeline, map, archive, and dashboard states.',
				$12, $11, 'A deterministic venue description for local visual QA.',
				'Seeded hotel copy for layout testing.', 'Seeded ticket copy for layout testing.',
				'Fixture Speakers', 'Talk and speaker data for local development.',
				'https://www.google.com/maps/embed/v1/place?q=fixture&key=dev',
				$15, $16, $17, $18, $19, $20
			)
			ON CONFLICT (tag) DO UPDATE SET
				active = EXCLUDED.active,
				publication_status = EXCLUDED.publication_status,
				description = EXCLUDED.description,
				edition_type = EXCLUDED.edition_type,
				tagline = EXCLUDED.tagline,
				date_desc = EXCLUDED.date_desc,
				start_date = EXCLUDED.start_date,
				end_date = EXCLUDED.end_date,
				location = EXCLUDED.location,
				venue = EXCLUDED.venue,
				hero_title = EXCLUDED.hero_title,
				hero_caption = EXCLUDED.hero_caption,
				about_title = EXCLUDED.about_title,
				about_body = EXCLUDED.about_body,
				about_body_2 = EXCLUDED.about_body_2,
				venue_title = EXCLUDED.venue_title,
				venue_subtitle = EXCLUDED.venue_subtitle,
				venue_body = EXCLUDED.venue_body,
				hotels_intro = EXCLUDED.hotels_intro,
				local_ticket_body = EXCLUDED.local_ticket_body,
				speakers_title = EXCLUDED.speakers_title,
				speakers_body = EXCLUDED.speakers_body,
				map_embed_url = EXCLUDED.map_embed_url,
				map_latitude = EXCLUDED.map_latitude,
				map_longitude = EXCLUDED.map_longitude,
				map_x_percent = EXCLUDED.map_x_percent,
				map_y_percent = EXCLUDED.map_y_percent,
				map_label = EXCLUDED.map_label,
				map_label_side = EXCLUDED.map_label_side
		`, conf.id, conf.tag, conf.active, conf.status, conf.desc, conf.editionType,
			conf.tagline, conf.dateDesc, conf.start, conf.end, conf.location, conf.venue,
			fmt.Sprintf(`<span class="font-bitcoin">bitcoin++</span> %s`, conf.tagline),
			conf.desc, conf.lat, conf.lng, conf.x, conf.y, conf.label, conf.side)
	}

	mustExec(ctx, tx, "seed past conference day", `
		INSERT INTO conference_days (
			id, conference_id, day_number, doors_start, doors_end,
			breakfast_start, breakfast_end, lunch_start, lunch_end, coffee_start, coffee_end, venues
		)
		VALUES (
			'00000000-0000-4000-8000-000000000014'::uuid, $1::uuid, 1,
			'09:00'::time, '17:00'::time, '09:00'::time, '09:30'::time,
			'12:00'::time, '13:00'::time, '15:00'::time, '15:30'::time, $2
		)
		ON CONFLICT (conference_id, day_number) DO UPDATE SET
			doors_start = EXCLUDED.doors_start,
			doors_end = EXCLUDED.doors_end,
			breakfast_start = EXCLUDED.breakfast_start,
			breakfast_end = EXCLUDED.breakfast_end,
			lunch_start = EXCLUDED.lunch_start,
			lunch_end = EXCLUDED.lunch_end,
			coffee_start = EXCLUDED.coffee_start,
			coffee_end = EXCLUDED.coffee_end,
			venues = EXCLUDED.venues
	`, devPastConfID, []string{"main"})

	return devPastConfID
}

func seedConferenceDays(ctx context.Context, tx pgx.Tx, confID string) {
	for _, day := range devDays {
		mustExec(ctx, tx, "seed conference day", `
			INSERT INTO conference_days (
				id, conference_id, day_number, doors_start, doors_end,
				breakfast_start, breakfast_end, lunch_start, lunch_end,
				coffee_start, coffee_end, venues
			)
			VALUES (
				$1::uuid, $2::uuid, $3,
				$4::time, $5::time, $6::time, $7::time,
				$8::time, $9::time, $10::time, $11::time, $12
			)
			ON CONFLICT (conference_id, day_number) DO UPDATE SET
				doors_start = EXCLUDED.doors_start,
				doors_end = EXCLUDED.doors_end,
				breakfast_start = EXCLUDED.breakfast_start,
				breakfast_end = EXCLUDED.breakfast_end,
				lunch_start = EXCLUDED.lunch_start,
				lunch_end = EXCLUDED.lunch_end,
				coffee_start = EXCLUDED.coffee_start,
				coffee_end = EXCLUDED.coffee_end,
				venues = EXCLUDED.venues
		`, day.id, confID, day.number, day.doorsStart, day.doorsEnd,
			day.breakfastStart, day.breakfastEnd, day.lunchStart, day.lunchEnd,
			day.coffeeStart, day.coffeeEnd, day.venues)
	}
}

func seedTickets(ctx context.Context, tx pgx.Tx, confID string) {
	for _, tix := range devTickets {
		mustExec(ctx, tx, "seed ticket", `
			INSERT INTO conference_tickets (
				id, conference_id, ticket_key, tier, local_price, btc_price, usd_price,
				expires_start, max_count, currency, symbol, post_symbol
			)
			VALUES (
				$1::uuid, $2::uuid, $3, $4, $5, $6, $7,
				$8::timestamptz, $9, $10, $11, $12
			)
			ON CONFLICT (conference_id, ticket_key) DO UPDATE SET
				tier = EXCLUDED.tier,
				local_price = EXCLUDED.local_price,
				btc_price = EXCLUDED.btc_price,
				usd_price = EXCLUDED.usd_price,
				expires_start = EXCLUDED.expires_start,
				max_count = EXCLUDED.max_count,
				currency = EXCLUDED.currency,
				symbol = EXCLUDED.symbol,
				post_symbol = EXCLUDED.post_symbol
		`, tix.id, confID, tix.key, tix.tier, tix.local, tix.btc, tix.usd,
			tix.expiresStart, tix.max, tix.currency, tix.symbol, tix.postSymbol)
	}
}

func seedAdmin(ctx context.Context, tx pgx.Tx) {
	mustExec(ctx, tx, "seed admin person", `
		INSERT INTO people (
			id, name, email, norm_photo_path, phone, signal, telegram, twitter_handle,
			nostr, github_url, instagram, linkedin, website_url, company,
			org_logo_path, avail_to_hire, looking_to_hire, tshirt
		)
		VALUES (
			$1::uuid, 'Dev Admin', 'dev-admin@example.test', '', '', '', '', '',
			'', '', '', '', '', 'bitcoin++ local dev', '', false, false, ''
		)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			email = EXCLUDED.email,
			company = EXCLUDED.company
	`, devAdminID)

	mustExec(ctx, tx, "seed admin role", `
		INSERT INTO people_roles (person_id, scope, position)
		VALUES ($1::uuid, 'global', 'admin')
		ON CONFLICT DO NOTHING
	`, devAdminID)
}

func seedProgram(ctx context.Context, tx pgx.Tx, confID string) {
	for i, sp := range devSpeakers {
		mustExec(ctx, tx, "seed speaker person", `
			INSERT INTO people (
				id, name, email, norm_photo_path, phone, signal, telegram, twitter_handle,
				nostr, github_url, instagram, linkedin, website_url, company,
				org_logo_path, avail_to_hire, looking_to_hire, tshirt
			)
			VALUES (
				$1::uuid, $2, NULLIF($3, '')::citext, $4, '', '', '', $5,
				'', $6, '', '', $7, $8, '', false, false, ''
			)
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				email = EXCLUDED.email,
				norm_photo_path = EXCLUDED.norm_photo_path,
				twitter_handle = EXCLUDED.twitter_handle,
				github_url = EXCLUDED.github_url,
				website_url = EXCLUDED.website_url,
				company = EXCLUDED.company
		`, sp.personID, sp.name, sp.email, sp.photo, sp.twitter, sp.github, sp.website, sp.company)

		mustExec(ctx, tx, "seed speaker conf", `
			INSERT INTO speaker_confs (
				id, speaker_id, coming_from, availability, record_ok, visa,
				first_event, dinner_rsvp, sponsor, company, org_photo_path,
				invited_at, viewed_at, accepted_at, featured_rank
			)
			VALUES (
				$1::uuid, $2::uuid, $3, $4, '', 'Not needed',
				false, true, false, $5, '', now(), now(), now(), $6
			)
			ON CONFLICT (id) DO UPDATE SET
				speaker_id = EXCLUDED.speaker_id,
				coming_from = EXCLUDED.coming_from,
				availability = EXCLUDED.availability,
				record_ok = EXCLUDED.record_ok,
				visa = EXCLUDED.visa,
				first_event = EXCLUDED.first_event,
				dinner_rsvp = EXCLUDED.dinner_rsvp,
				sponsor = EXCLUDED.sponsor,
				company = EXCLUDED.company,
				org_photo_path = EXCLUDED.org_photo_path,
				accepted_at = EXCLUDED.accepted_at,
				featured_rank = EXCLUDED.featured_rank
		`, sp.speakerConfID, sp.personID, sp.comingFrom, []string{"Day 1", "Day 2", "Day 3"}, sp.company, i+1)

		mustExec(ctx, tx, "seed speaker conf conference link", `
			INSERT INTO speaker_confs_conferences (speaker_conf_id, conference_id)
			VALUES ($1::uuid, $2::uuid)
			ON CONFLICT DO NOTHING
		`, sp.speakerConfID, confID)

		mustExec(ctx, tx, "seed proposal", `
			INSERT INTO proposals (
				id, conference_id, title, description, setup, comments, talk_type,
				status, desired_duration_min, avail_duration_min, invite_token
			)
			VALUES (
				$1::uuid, $2::uuid, $3, $4, '', 'Seeded by cmd/dev-seed for local development.',
				$5, 'Scheduled', $6, $6, ''
			)
			ON CONFLICT (id) DO UPDATE SET
				conference_id = EXCLUDED.conference_id,
				title = EXCLUDED.title,
				description = EXCLUDED.description,
				setup = EXCLUDED.setup,
				comments = EXCLUDED.comments,
				talk_type = EXCLUDED.talk_type,
				status = EXCLUDED.status,
				desired_duration_min = EXCLUDED.desired_duration_min,
				avail_duration_min = EXCLUDED.avail_duration_min
		`, sp.proposalID, confID, sp.title, sp.description, sp.talkType, sp.duration)

		mustExec(ctx, tx, "seed proposal speaker link", `
			INSERT INTO proposals_speaker_confs (proposal_id, speaker_conf_id)
			VALUES ($1::uuid, $2::uuid)
			ON CONFLICT DO NOTHING
		`, sp.proposalID, sp.speakerConfID)

		mustExec(ctx, tx, "seed conf talk", `
			INSERT INTO conf_talks (
				id, conference_id, proposal_id, clipart_path, scheduled_start,
				scheduled_end, production_notes, venue, section, cal_notif,
				social_card_path
			)
			VALUES (
				$1::uuid, $2::uuid, $3::uuid, $4, $5::timestamptz,
				$6::timestamptz, '', $7, '', '', ''
			)
			ON CONFLICT (id) DO UPDATE SET
				conference_id = EXCLUDED.conference_id,
				proposal_id = EXCLUDED.proposal_id,
				clipart_path = EXCLUDED.clipart_path,
				scheduled_start = EXCLUDED.scheduled_start,
				scheduled_end = EXCLUDED.scheduled_end,
				venue = EXCLUDED.venue,
				archived_at = NULL
		`, sp.talkID, confID, sp.proposalID, sp.clipart, sp.start, sp.end, sp.venue)
	}

	mustExec(ctx, tx, "seed panel co-speaker link", `
		INSERT INTO proposals_speaker_confs (proposal_id, speaker_conf_id)
		VALUES ($1::uuid, $2::uuid)
		ON CONFLICT DO NOTHING
	`, "00000000-0000-4000-8000-000000000306", "00000000-0000-4000-8000-000000000203")
}

func seedSponsors(ctx context.Context, tx pgx.Tx, confID string) {
	for _, org := range devOrgs {
		mustExec(ctx, tx, "seed organization", `
			INSERT INTO organizations (
				id, name, tagline, logo_light_url, logo_dark_url, email,
				website_url, linkedin_url, instagram_url, youtube_url,
				github_url, twitter_handle, nostr, matrix, hiring, notes
			)
			VALUES (
				$1::uuid, $2, $3, $4, $4, NULL, $5, '', '', '',
				'', $6, '', '', false, 'Local dev fixture sponsor.'
			)
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				tagline = EXCLUDED.tagline,
				logo_light_url = EXCLUDED.logo_light_url,
				logo_dark_url = EXCLUDED.logo_dark_url,
				website_url = EXCLUDED.website_url,
				twitter_handle = EXCLUDED.twitter_handle,
				notes = EXCLUDED.notes
		`, org.id, org.name, org.tagline, org.logo, org.website, org.twitter)
	}

	for _, sp := range devSponsorships {
		mustExec(ctx, tx, "seed sponsorship", `
			INSERT INTO sponsorships (
				id, organization_id, name, level, label, status, is_vendor, notes, archived_at
			)
			VALUES (
				$1::uuid, $2::uuid,
				(SELECT name || ' @ ' || $3 FROM organizations WHERE id = $2::uuid),
				$3, $4, $5, false, 'Local dev fixture sponsorship.', NULL
			)
			ON CONFLICT (id) DO UPDATE SET
				organization_id = EXCLUDED.organization_id,
				name = EXCLUDED.name,
				level = EXCLUDED.level,
				label = EXCLUDED.label,
				status = EXCLUDED.status,
				is_vendor = EXCLUDED.is_vendor,
				notes = EXCLUDED.notes,
				archived_at = NULL
		`, sp.id, sp.orgID, sp.level, sp.label, sp.status)

		mustExec(ctx, tx, "seed sponsorship conference link", `
			INSERT INTO sponsorships_conferences (sponsorship_id, conference_id)
			VALUES ($1::uuid, $2::uuid)
			ON CONFLICT DO NOTHING
		`, sp.id, confID)
	}
}

func seedHotels(ctx context.Context, tx pgx.Tx, confID string) {
	for _, hotel := range devHotels {
		mustExec(ctx, tx, "seed hotel", `
			INSERT INTO hotels (
				id, conference_id, name, url, img_path, type, description,
				display_order, archived_at
			)
			VALUES (
				$1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, NULL
			)
			ON CONFLICT (id) DO UPDATE SET
				conference_id = EXCLUDED.conference_id,
				name = EXCLUDED.name,
				url = EXCLUDED.url,
				img_path = EXCLUDED.img_path,
				type = EXCLUDED.type,
				description = EXCLUDED.description,
				display_order = EXCLUDED.display_order,
				archived_at = NULL
		`, hotel.id, confID, hotel.name, hotel.url, hotel.img, hotel.hotelType, hotel.desc, hotel.order)
	}
}

func seedSatelliteEvents(ctx context.Context, tx pgx.Tx, confID string) {
	for _, sat := range devSatellites {
		mustExec(ctx, tx, "seed satellite event", `
			INSERT INTO satellite_events (
				id, conference_id, title, description, event_url, event_type,
				starts_at, ends_at, location, image_url, host_name, host_url,
				host_logo_url, submitter_email, status, notes, published_at
			)
			VALUES (
				$1::uuid, $2::uuid, $3, $4, $5, $6,
				$7::timestamptz, $8::timestamptz, $9, $10, $11, $12,
				$13, 'dev-admin@example.test', 'published',
				'Seeded by cmd/dev-seed for local development.', now()
			)
			ON CONFLICT (id) DO UPDATE SET
				conference_id = EXCLUDED.conference_id,
				title = EXCLUDED.title,
				description = EXCLUDED.description,
				event_url = EXCLUDED.event_url,
				event_type = EXCLUDED.event_type,
				starts_at = EXCLUDED.starts_at,
				ends_at = EXCLUDED.ends_at,
				location = EXCLUDED.location,
				image_url = EXCLUDED.image_url,
				host_name = EXCLUDED.host_name,
				host_url = EXCLUDED.host_url,
				host_logo_url = EXCLUDED.host_logo_url,
				submitter_email = EXCLUDED.submitter_email,
				status = EXCLUDED.status,
				notes = EXCLUDED.notes,
				published_at = EXCLUDED.published_at
		`, sat.id, confID, sat.title, sat.description, sat.eventURL, sat.eventType,
			sat.start, sat.end, sat.location, sat.imageURL, sat.hostName, sat.hostURL, sat.hostLogoURL)
	}
}

func seedHomepageFeaturedSpeakers(ctx context.Context, tx pgx.Tx) {
	extraPeople := []struct {
		id, name, email, photo, company string
	}{
		{devHomepagePerson7ID, "Nora Blake", "nora.blake@example.test", "../static/img/laolu.jpg", "Channel Studio"},
		{devHomepagePerson8ID, "Owen Park", "owen.park@example.test", "../static/img/reardencode.png", "Archive Systems"},
	}
	for _, person := range extraPeople {
		mustExec(ctx, tx, "seed homepage-only speaker", `
			INSERT INTO people (
				id, name, email, norm_photo_path, phone, signal, telegram, twitter_handle,
				nostr, github_url, instagram, linkedin, website_url, company,
				org_logo_path, avail_to_hire, looking_to_hire, tshirt
			)
			VALUES (
				$1::uuid, $2, $3::citext, $4, '', '', '', '',
				'', '', '', '', '', $5, '', false, false, ''
			)
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				email = EXCLUDED.email,
				norm_photo_path = EXCLUDED.norm_photo_path,
				company = EXCLUDED.company
		`, person.id, person.name, person.email, person.photo, person.company)
	}

	featured := []string{
		"00000000-0000-4000-8000-000000000101",
		"00000000-0000-4000-8000-000000000102",
		"00000000-0000-4000-8000-000000000103",
		"00000000-0000-4000-8000-000000000104",
		"00000000-0000-4000-8000-000000000105",
		"00000000-0000-4000-8000-000000000106",
		devHomepagePerson7ID,
		devHomepagePerson8ID,
	}
	for i, personID := range featured {
		mustExec(ctx, tx, "seed homepage featured speaker", `
			INSERT INTO homepage_featured_speakers (position, person_id)
			VALUES ($1, $2::uuid)
			ON CONFLICT (position) DO UPDATE SET
				person_id = EXCLUDED.person_id
		`, i+1, personID)
	}
}

func seedDashboardFixtures(ctx context.Context, tx pgx.Tx, confID, pastConfID string) {
	seedDashboardRegistrations(ctx, tx, confID, pastConfID)
	seedDashboardAffiliate(ctx, tx, confID, pastConfID)
	seedDashboardArchiveTalk(ctx, tx, pastConfID)
	seedDashboardVolunteer(ctx, tx, confID)
}

func seedDashboardRegistrations(ctx context.Context, tx pgx.Tx, confID, pastConfID string) {
	regs := []struct {
		id, ref, confID, item, amount, registered string
	}{
		{"00000000-0000-4000-8000-000000000901", "dev-ticket-upcoming-001", confID, "General Admission", "150.00", "2026-07-01 12:00:00-05"},
		{"00000000-0000-4000-8000-000000000902", "dev-ticket-archive-001", pastConfID, "Archive Pass", "95.00", "2024-04-10 12:00:00-05"},
	}
	for _, reg := range regs {
		mustExec(ctx, tx, "seed dashboard registration", `
			INSERT INTO registrations (
				id, ref_id, checkout_id, conference_id, type, email, item_bought,
				amount_paid, currency, platform, registered_at, revoked
			)
			VALUES (
				$1::uuid, $2, '', $3::uuid, 'ticket', 'dev-admin@example.test',
				$4, $5::numeric, 'USD', 'dev-seed', $6::timestamptz, false
			)
			ON CONFLICT (ref_id) DO UPDATE SET
				conference_id = EXCLUDED.conference_id,
				type = EXCLUDED.type,
				email = EXCLUDED.email,
				item_bought = EXCLUDED.item_bought,
				amount_paid = EXCLUDED.amount_paid,
				currency = EXCLUDED.currency,
				platform = EXCLUDED.platform,
				registered_at = EXCLUDED.registered_at,
				revoked = false
		`, reg.id, reg.ref, reg.confID, reg.item, reg.amount, reg.registered)
	}
}

func seedDashboardAffiliate(ctx context.Context, tx pgx.Tx, confID, pastConfID string) {
	mustExec(ctx, tx, "seed affiliate discount", `
		INSERT INTO discounts (
			id, code_name, discount_expr, uses_count, affiliate_email, disc_type,
			amount, max_uses, extra_qty, valid_from, valid_until, archived_at
		)
		VALUES (
			$1::uuid, 'DEVADMIN15', '%15', 3, 'dev-admin@example.test',
			'%', 15, NULL, 0, NULL, NULL, NULL
		)
		ON CONFLICT (code_name) DO UPDATE SET
			discount_expr = EXCLUDED.discount_expr,
			uses_count = EXCLUDED.uses_count,
			affiliate_email = EXCLUDED.affiliate_email,
			disc_type = EXCLUDED.disc_type,
			amount = EXCLUDED.amount,
			max_uses = EXCLUDED.max_uses,
			extra_qty = EXCLUDED.extra_qty,
			valid_from = EXCLUDED.valid_from,
			valid_until = EXCLUDED.valid_until,
			archived_at = NULL
	`, devAffiliateDiscountID)

	usages := []struct {
		id, confID             string
		saved, earned, tickets int
	}{
		{"00000000-0000-4000-8000-000000000903", confID, 15000, 5000, 2},
		{"00000000-0000-4000-8000-000000000904", pastConfID, 7500, 2500, 1},
	}
	for _, usage := range usages {
		mustExec(ctx, tx, "seed affiliate usage", `
			INSERT INTO affiliate_usages (
				id, discount_id, conference_id, code_name_snapshot, affiliate_email,
				saved_sats, earned_sats, tickets_count
			)
			VALUES (
				$1::uuid, $2::uuid, $3::uuid, 'DEVADMIN15', 'dev-admin@example.test',
				$4, $5, $6
			)
			ON CONFLICT (id) DO UPDATE SET
				discount_id = EXCLUDED.discount_id,
				conference_id = EXCLUDED.conference_id,
				code_name_snapshot = EXCLUDED.code_name_snapshot,
				affiliate_email = EXCLUDED.affiliate_email,
				saved_sats = EXCLUDED.saved_sats,
				earned_sats = EXCLUDED.earned_sats,
				tickets_count = EXCLUDED.tickets_count
		`, usage.id, devAffiliateDiscountID, usage.confID, usage.saved, usage.earned, usage.tickets)
	}
}

func seedDashboardArchiveTalk(ctx context.Context, tx pgx.Tx, pastConfID string) {
	mustExec(ctx, tx, "seed dashboard archive speaker conf", `
		INSERT INTO speaker_confs (
			id, speaker_id, coming_from, availability, record_ok, visa,
			first_event, dinner_rsvp, sponsor, company, org_photo_path,
			invited_at, viewed_at, accepted_at, featured_rank
		)
		VALUES (
			$1::uuid, $2::uuid, 'Austin, TX', $3, 'RecordingOK', 'Not needed',
			false, true, false, 'bitcoin++ local dev', '', now(), now(), now(), 1
		)
		ON CONFLICT (id) DO UPDATE SET
			speaker_id = EXCLUDED.speaker_id,
			coming_from = EXCLUDED.coming_from,
			availability = EXCLUDED.availability,
			record_ok = EXCLUDED.record_ok,
			visa = EXCLUDED.visa,
			first_event = EXCLUDED.first_event,
			dinner_rsvp = EXCLUDED.dinner_rsvp,
			sponsor = EXCLUDED.sponsor,
			company = EXCLUDED.company,
			org_photo_path = EXCLUDED.org_photo_path,
			accepted_at = EXCLUDED.accepted_at,
			featured_rank = EXCLUDED.featured_rank
	`, devArchiveSpeakerConfID, devAdminID, []string{"Day 1"})

	mustExec(ctx, tx, "seed dashboard archive speaker conf conference link", `
		INSERT INTO speaker_confs_conferences (speaker_conf_id, conference_id)
		VALUES ($1::uuid, $2::uuid)
		ON CONFLICT DO NOTHING
	`, devArchiveSpeakerConfID, pastConfID)

	mustExec(ctx, tx, "seed dashboard archive proposal", `
		INSERT INTO proposals (
			id, conference_id, title, description, setup, comments, talk_type,
			status, desired_duration_min, avail_duration_min, invite_token
		)
		VALUES (
			$1::uuid, $2::uuid, 'Deterministic Fixtures for Bitcoin Apps',
			'How stable local data catches UI regressions before launch.',
			'', 'Seeded by cmd/dev-seed for dashboard archive QA.',
			'talk', 'Scheduled', 30, 30, ''
		)
		ON CONFLICT (id) DO UPDATE SET
			conference_id = EXCLUDED.conference_id,
			title = EXCLUDED.title,
			description = EXCLUDED.description,
			setup = EXCLUDED.setup,
			comments = EXCLUDED.comments,
			talk_type = EXCLUDED.talk_type,
			status = EXCLUDED.status,
			desired_duration_min = EXCLUDED.desired_duration_min,
			avail_duration_min = EXCLUDED.avail_duration_min
	`, devArchiveProposalID, pastConfID)

	mustExec(ctx, tx, "seed dashboard archive proposal speaker link", `
		INSERT INTO proposals_speaker_confs (proposal_id, speaker_conf_id)
		VALUES ($1::uuid, $2::uuid)
		ON CONFLICT DO NOTHING
	`, devArchiveProposalID, devArchiveSpeakerConfID)

	mustExec(ctx, tx, "seed dashboard archive conf talk", `
		INSERT INTO conf_talks (
			id, conference_id, proposal_id, clipart_path, scheduled_start,
			scheduled_end, production_notes, venue, section, cal_notif,
			social_card_path, archived_at
		)
		VALUES (
			$1::uuid, $2::uuid, $3::uuid, '../static/img/atx22/leading.png',
			'2024-05-01 10:00:00-05'::timestamptz,
			'2024-05-01 10:30:00-05'::timestamptz,
			'', 'main', '', '', '', NULL
		)
		ON CONFLICT (id) DO UPDATE SET
			conference_id = EXCLUDED.conference_id,
			proposal_id = EXCLUDED.proposal_id,
			clipart_path = EXCLUDED.clipart_path,
			scheduled_start = EXCLUDED.scheduled_start,
			scheduled_end = EXCLUDED.scheduled_end,
			venue = EXCLUDED.venue,
			archived_at = NULL
	`, devArchiveTalkID, pastConfID, devArchiveProposalID)

	mustExec(ctx, tx, "seed dashboard archive recording", `
		INSERT INTO recordings (
			id, conf_talk_id, talk_name, youtube_url, x_url, x_reply_url,
			file_uri, publish_at
		)
		VALUES (
			$1::uuid, $2::uuid, 'Deterministic Fixtures for Bitcoin Apps',
			'https://www.youtube.com/watch?v=dQw4w9WgXcQ', '', '',
			'spaces://dev-fixture/archive-talk.mp4',
			'2024-05-15 10:00:00-05'::timestamptz
		)
		ON CONFLICT (conf_talk_id) DO UPDATE SET
			talk_name = EXCLUDED.talk_name,
			youtube_url = EXCLUDED.youtube_url,
			x_url = EXCLUDED.x_url,
			x_reply_url = EXCLUDED.x_reply_url,
			file_uri = EXCLUDED.file_uri,
			publish_at = EXCLUDED.publish_at
	`, devArchiveRecordingID, devArchiveTalkID)
}

func seedDashboardVolunteer(ctx context.Context, tx pgx.Tx, confID string) {
	mustExec(ctx, tx, "seed dashboard job type", `
		INSERT INTO job_types (
			id, tag, display_order, title, tooltip, long_desc, show
		)
		VALUES (
			$1::uuid, 'registration', 1, 'Registration',
			'Help attendees check in.', 'Check tickets and help attendees find their badge.', true
		)
		ON CONFLICT (tag) DO UPDATE SET
			display_order = EXCLUDED.display_order,
			title = EXCLUDED.title,
			tooltip = EXCLUDED.tooltip,
			long_desc = EXCLUDED.long_desc,
			show = EXCLUDED.show
	`, devJobTypeID)

	mustExec(ctx, tx, "seed dashboard volunteer", `
		INSERT INTO volunteers (
			id, name, email, phone, signal, availability, contact_at, comments,
			discovered_via, first_event, hometown, twitter_handle, nostr,
			shirt, status, captcha, subscribe
		)
		VALUES (
			$1::uuid, 'Dev Admin', 'dev-admin@example.test', '', '', $2,
			'Email', 'Seeded volunteer fixture.', 'local dev harness',
			false, 'Austin, TX', '', '', 'M', 'Scheduled', 0, false
		)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			email = EXCLUDED.email,
			availability = EXCLUDED.availability,
			contact_at = EXCLUDED.contact_at,
			comments = EXCLUDED.comments,
			discovered_via = EXCLUDED.discovered_via,
			hometown = EXCLUDED.hometown,
			shirt = EXCLUDED.shirt,
			status = EXCLUDED.status
	`, devVolunteerID, []string{"Day 1", "Day 2"})

	mustExec(ctx, tx, "seed dashboard volunteer conference link", `
		INSERT INTO volunteers_conferences (volunteer_id, conference_id, kind)
		VALUES ($1::uuid, $2::uuid, 'schedule_for')
		ON CONFLICT DO NOTHING
	`, devVolunteerID, confID)

	mustExec(ctx, tx, "seed dashboard volunteer job preference", `
		INSERT INTO volunteers_job_types (volunteer_id, job_type_id, preference)
		VALUES ($1::uuid, $2::uuid, 'yes')
		ON CONFLICT DO NOTHING
	`, devVolunteerID, devJobTypeID)

	mustExec(ctx, tx, "seed dashboard work shift", `
		INSERT INTO work_shifts (
			id, conference_id, job_type_id, name, max_vols, shift_start,
			shift_end, priority, cal_notif
		)
		VALUES (
			$1::uuid, $2::uuid, $3::uuid, 'Registration desk',
			2, '2026-10-01 08:30:00-05'::timestamptz,
			'2026-10-01 10:30:00-05'::timestamptz, 1, ''
		)
		ON CONFLICT (id) DO UPDATE SET
			conference_id = EXCLUDED.conference_id,
			job_type_id = EXCLUDED.job_type_id,
			name = EXCLUDED.name,
			max_vols = EXCLUDED.max_vols,
			shift_start = EXCLUDED.shift_start,
			shift_end = EXCLUDED.shift_end,
			priority = EXCLUDED.priority,
			cal_notif = EXCLUDED.cal_notif
	`, devWorkShiftID, confID, devJobTypeID)

	mustExec(ctx, tx, "seed dashboard work shift assignee", `
		INSERT INTO work_shifts_volunteers (shift_id, volunteer_id, role)
		VALUES ($1::uuid, $2::uuid, 'assignee')
		ON CONFLICT DO NOTHING
	`, devWorkShiftID, devVolunteerID)

	mustExec(ctx, tx, "seed dashboard volunteer info", `
		INSERT INTO volunteer_info (
			id, conference_id, orient_link_url, orient_start, orient_end, notes
		)
		VALUES (
			$1::uuid, $2::uuid, 'https://example.test/dev26/volunteer-orientation',
			'2026-09-30 17:00:00-05'::timestamptz,
			'2026-09-30 18:00:00-05'::timestamptz,
			'Seeded orientation info for dashboard volunteer QA.'
		)
		ON CONFLICT (conference_id) DO UPDATE SET
			orient_link_url = EXCLUDED.orient_link_url,
			orient_start = EXCLUDED.orient_start,
			orient_end = EXCLUDED.orient_end,
			notes = EXCLUDED.notes
	`, devVolInfoID, confID)
}

func mustExec(ctx context.Context, tx pgx.Tx, label string, sql string, args ...any) {
	if _, err := tx.Exec(ctx, sql, args...); err != nil {
		log.Fatal(fmt.Errorf("%s: %w", label, err))
	}
}
