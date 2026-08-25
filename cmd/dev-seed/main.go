package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"btcpp-web/internal/db"
	"btcpp-web/internal/envconfig"
	conferencemissives "btcpp-web/templates/missives"

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

	devEarlyTixID            = "00000000-0000-4000-8000-000000000021"
	devGeneralTixID          = "00000000-0000-4000-8000-000000000022"
	devAdminID               = "00000000-0000-4000-8000-000000000031"
	devArchiveSpeakerConfID  = "00000000-0000-4000-8000-000000000033"
	devArchiveProposalID     = "00000000-0000-4000-8000-000000000034"
	devArchiveTalkID         = "00000000-0000-4000-8000-000000000035"
	devArchiveRecordingID    = "00000000-0000-4000-8000-000000000036"
	devAffiliateDiscountID   = "00000000-0000-4000-8000-000000000037"
	devVolunteerID           = "00000000-0000-4000-8000-000000000038"
	devJobTypeID             = "00000000-0000-4000-8000-000000000039"
	devWorkShiftID           = "00000000-0000-4000-8000-000000000040"
	devVolInfoID             = "00000000-0000-4000-8000-000000000041"
	devHomepagePerson7ID     = "00000000-0000-4000-8000-000000000042"
	devHomepagePerson8ID     = "00000000-0000-4000-8000-000000000043"
	devVolLoginMissiveID     = "00000000-0000-4000-8000-000000000044"
	devVolAppMissiveID       = "00000000-0000-4000-8000-000000000045"
	devVolSignupMissiveID    = "00000000-0000-4000-8000-000000000046"
	devVolShiftsMissiveID    = "00000000-0000-4000-8000-000000000047"
	devMerchProduct1ID       = "00000000-0000-4000-8000-000000000051"
	devMerchProduct2ID       = "00000000-0000-4000-8000-000000000052"
	devMerchProduct3ID       = "00000000-0000-4000-8000-000000000053"
	devMerchProduct4ID       = "00000000-0000-4000-8000-000000000054"
	devMerchProduct5ID       = "00000000-0000-4000-8000-000000000055"
	devMerchVariant1ID       = "00000000-0000-4000-8000-000000000061"
	devMerchVariant2ID       = "00000000-0000-4000-8000-000000000062"
	devMerchVariant3ID       = "00000000-0000-4000-8000-000000000063"
	devMerchVariant4ID       = "00000000-0000-4000-8000-000000000064"
	devMerchVariant5ID       = "00000000-0000-4000-8000-000000000065"
	devShopOrderPickupID     = "00000000-0000-4000-8000-000000000071"
	devShopOrderShipID       = "00000000-0000-4000-8000-000000000072"
	devShopOrderPendingID    = "00000000-0000-4000-8000-000000000073"
	devShopOrderRefundID     = "00000000-0000-4000-8000-000000000074"
	devShopOrderMixedID      = "00000000-0000-4000-8000-000000000075"
	devShopItemPickupID      = "00000000-0000-4000-8000-000000000081"
	devShopItemShipID        = "00000000-0000-4000-8000-000000000082"
	devShopItemPendingID     = "00000000-0000-4000-8000-000000000083"
	devShopItemRefundID      = "00000000-0000-4000-8000-000000000084"
	devShopItemMixedMerchID  = "00000000-0000-4000-8000-000000000085"
	devShopItemMixedTixID    = "00000000-0000-4000-8000-000000000086"
	devShopPickupReadyID     = "00000000-0000-4000-8000-000000000087"
	devShopPickupDoneID      = "00000000-0000-4000-8000-000000000088"
	devShopRefundID          = "00000000-0000-4000-8000-000000000089"
	devCheckInGenpopID       = "00000000-0000-4000-8000-000000000a01"
	devCheckInLocalID        = "00000000-0000-4000-8000-000000000a02"
	devCheckInSponsorID      = "00000000-0000-4000-8000-000000000a03"
	devCheckInVolunteerID    = "00000000-0000-4000-8000-000000000a04"
	devCheckInSpeakerID      = "00000000-0000-4000-8000-000000000a05"
	devCheckInSponsorOrder   = "00000000-0000-4000-8000-000000000a21"
	devCheckInSpeakerOrder   = "00000000-0000-4000-8000-000000000a22"
	devCheckInSponsorItem    = "00000000-0000-4000-8000-000000000a31"
	devCheckInSpeakerItem1   = "00000000-0000-4000-8000-000000000a32"
	devCheckInSpeakerItem2   = "00000000-0000-4000-8000-000000000a33"
	devCheckInSponsorPickup  = "00000000-0000-4000-8000-000000000a41"
	devCheckInSpeakerPickup1 = "00000000-0000-4000-8000-000000000a42"
	devCheckInSpeakerPickup2 = "00000000-0000-4000-8000-000000000a43"
	devNewsletterCompetition = "00000000-0000-4000-8000-000000000b01"
	devNewsletterProject1    = "00000000-0000-4000-8000-000000000b02"
	devNewsletterProject2    = "00000000-0000-4000-8000-000000000b03"
	devNewsletterAward1      = "00000000-0000-4000-8000-000000000b11"
	devNewsletterAward2      = "00000000-0000-4000-8000-000000000b12"
	devNewsletterAward3      = "00000000-0000-4000-8000-000000000b13"
	devNewsletterRecording   = "00000000-0000-4000-8000-000000000b21"
	devNewsletterRecording2  = "00000000-0000-4000-8000-000000000b22"
	devNewsletterRecording3  = "00000000-0000-4000-8000-000000000b23"
	devNewsletterRecording4  = "00000000-0000-4000-8000-000000000b24"
	devSponsorAward          = "00000000-0000-4000-8000-000000000b31"
	devSponsorPrize          = "00000000-0000-4000-8000-000000000b32"
	devSponsorConsentEvent   = "00000000-0000-4000-8000-000000000b33"
	devPastSponsorship       = "00000000-0000-4000-8000-000000000606"
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
	company, twitter, github, leetcode, website string
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
	id, name, url, img, hotelType, desc, priceRange string
	order                                           int
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
		leetcode:      "mara-signet",
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
		start:         "2026-10-03 16:00:00-05",
		end:           "2026-10-03 17:00:00-05",
		venue:         "one",
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
		id:         "00000000-0000-4000-8000-000000000701",
		name:       "The Annex Hotel",
		url:        "https://example.test/dev26/hotels/annex",
		img:        "static/img/toronto/sonder.webp",
		hotelType:  "Hotel",
		desc:       "Walkable rooms near the venue with enough desk space to keep hacking after the talks.",
		priceRange: "$180–$240/night",
		order:      1,
	},
	{
		id:         "00000000-0000-4000-8000-000000000702",
		name:       "Congress House",
		url:        "https://example.test/dev26/hotels/congress",
		img:        "static/img/palmer_night.jpg",
		hotelType:  "Boutique",
		desc:       "A smaller stay option close to food, coffee, and the evening satellite events.",
		priceRange: "$140–$190/night",
		order:      2,
	},
	{
		id:         "00000000-0000-4000-8000-000000000703",
		name:       "Node Hostel",
		url:        "https://example.test/dev26/hotels/node-hostel",
		img:        "static/img/selina.webp",
		hotelType:  "Hostel",
		desc:       "Budget-friendly shared rooms for attendees who want to spend more on hardware and less on lodging.",
		priceRange: "$45–$85/night",
		order:      3,
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
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
	newsletterSendAt := nextWeeklyNewsletterSendAt(time.Now())
	seedConferenceDays(ctx, tx, confID)
	seedTickets(ctx, tx, confID, newsletterSendAt)
	seedAdmin(ctx, tx)
	seedProgram(ctx, tx, confID)
	seedMerch(ctx, tx, confID)
	seedWeeklyNewsletterFixtures(ctx, tx, confID, newsletterSendAt)
	seedSponsors(ctx, tx, confID, pastConfID)
	seedHotels(ctx, tx, confID)
	seedSatelliteEvents(ctx, tx, confID)
	seedHomepageFeaturedSpeakers(ctx, tx)
	seedDashboardFixtures(ctx, tx, confID, pastConfID)
	seedCheckInPreviews(ctx, tx, confID)
	seedMissives(ctx, tx)

	if err := tx.Commit(ctx); err != nil {
		log.Fatal(err)
	}

	log.Printf("seeded local dev conferences and dashboard fixture for dev-admin@example.test")
}

func seedCheckInPreviews(ctx context.Context, tx pgx.Tx, confID string) {
	people := []struct {
		id, name, email, tshirt string
	}{
		{devCheckInGenpopID, "Gina General", "dev-checkin-genpop@example.test", "MM"},
		{devCheckInLocalID, "Luca Local", "dev-checkin-local@example.test", ""},
		{devCheckInSponsorID, "Sam Sponsor", "dev-checkin-sponsor@example.test", "LS"},
		{devCheckInVolunteerID, "Val Volunteer", "dev-checkin-volunteer@example.test", "ML"},
		{devCheckInSpeakerID, "Sasha Speaker", "dev-checkin-speaker@example.test", "MXL"},
	}
	for _, person := range people {
		mustExec(ctx, tx, "seed check-in preview person", `
			INSERT INTO people (id, name, tshirt)
			VALUES ($1::uuid, $2, $3)
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				tshirt = EXCLUDED.tshirt
		`, person.id, person.name, person.tshirt)
		seedPersonEmail(ctx, tx, person.id, person.email)
	}

	registrations := []struct {
		id, ref, ticketType, email, item, checkedInAt, shirtPickedUpAt string
	}{
		{devCheckInGenpopID, "dev-checkin-genpop", "genpop", people[0].email, "General Admission", "", ""},
		{devCheckInLocalID, "dev-checkin-local", "local", people[1].email, "Local Pass", "2026-10-01 09:02:00-05", ""},
		{devCheckInSponsorID, "dev-checkin-sponsor", "sponsor", people[2].email, "Sponsor Pass", "2026-10-01 09:03:00-05", "2026-10-01 09:05:00-05"},
		{devCheckInVolunteerID, "dev-checkin-volunteer", "volunteer", people[3].email, "Volunteer Pass", "2026-10-01 08:30:00-05", ""},
		{devCheckInSpeakerID, "dev-checkin-speaker", "speaker", people[4].email, "Speaker Pass", "2026-10-01 08:45:00-05", ""},
	}
	for _, registration := range registrations {
		mustExec(ctx, tx, "seed check-in preview registration", `
			INSERT INTO registrations (
				id, ref_id, conference_id, type, email, person_id, item_bought, platform,
				registered_at, checked_in_at, revoked,
				conference_shirt_picked_up_at, conference_shirt_picked_up_by
			)
			VALUES (
				$1::uuid, $2, $3::uuid, $4, $5::citext, $1::uuid, $6, 'dev-checkin-preview',
				'2026-09-01 12:00:00-05'::timestamptz,
				NULLIF($7, '')::timestamptz, false,
				NULLIF($8, '')::timestamptz,
				CASE WHEN $8 = '' THEN NULL ELSE 'dev-volunteer@example.test'::citext END
			)
			ON CONFLICT (ref_id) DO UPDATE SET
				conference_id = EXCLUDED.conference_id,
				type = EXCLUDED.type,
				email = EXCLUDED.email,
				person_id = EXCLUDED.person_id,
				item_bought = EXCLUDED.item_bought,
				platform = EXCLUDED.platform,
				checked_in_at = EXCLUDED.checked_in_at,
				revoked = false,
				conference_shirt_picked_up_at = EXCLUDED.conference_shirt_picked_up_at,
				conference_shirt_picked_up_by = EXCLUDED.conference_shirt_picked_up_by
		`, registration.id, registration.ref, confID, registration.ticketType, registration.email,
			registration.item, registration.checkedInAt, registration.shirtPickedUpAt)
	}

	orderIDs := []string{devCheckInSponsorOrder, devCheckInSpeakerOrder}
	itemIDs := []string{devCheckInSponsorItem, devCheckInSpeakerItem1, devCheckInSpeakerItem2}
	mustExec(ctx, tx, "reset check-in preview events", `
		DELETE FROM shop_events
		WHERE order_id::text = ANY($1::text[]) OR order_item_id::text = ANY($2::text[])
	`, orderIDs, itemIDs)
	mustExec(ctx, tx, "reset check-in preview inventory events", `
		DELETE FROM merch_inventory_events WHERE order_item_id::text = ANY($1::text[])
	`, itemIDs)

	orders := []struct {
		id, publicID, email, name string
		total                     int
	}{
		{devCheckInSponsorOrder, "dev-checkin-sponsor-order", people[2].email, people[2].name, 3500},
		{devCheckInSpeakerOrder, "dev-checkin-speaker-order", people[4].email, people[4].name, 4300},
	}
	for _, order := range orders {
		mustExec(ctx, tx, "seed check-in preview order", `
			INSERT INTO shop_orders (
				id, public_id, buyer_email, buyer_name, status, source, checkout_kind,
				payment_provider, payment_provider_id, subtotal_cents, total_cents, paid_at
			)
			VALUES (
				$1::uuid, $2, $3::citext, $4, 'paid', 'online', 'merch',
				'stripe', $2, $5, $5, '2026-09-20 12:00:00-05'::timestamptz
			)
			ON CONFLICT (id) DO UPDATE SET
				buyer_email = EXCLUDED.buyer_email,
				buyer_name = EXCLUDED.buyer_name,
				status = 'paid',
				subtotal_cents = EXCLUDED.subtotal_cents,
				total_cents = EXCLUDED.total_cents,
				paid_at = EXCLUDED.paid_at
		`, order.id, order.publicID, order.email, order.name, order.total)
	}

	items := []struct {
		id, orderID, productID, variantID, name, variant, sku, status string
		quantity, fulfilled, unitPrice, total                         int
	}{
		{devCheckInSponsorItem, devCheckInSponsorOrder, devMerchProduct1ID, devMerchVariant1ID, "Core Hat", "Default", "MERCH-CORE-HAT", "fulfilled", 1, 1, 3500, 3500},
		{devCheckInSpeakerItem1, devCheckInSpeakerOrder, devMerchProduct4ID, devMerchVariant4ID, "Node Runner Tee", "Default", "MERCH-NODE-TEE", "ready", 1, 0, 3200, 3200},
		{devCheckInSpeakerItem2, devCheckInSpeakerOrder, devMerchProduct5ID, devMerchVariant5ID, "Signet Sticker Pack", "Default", "MERCH-SIGNET-STICKERS", "ready", 2, 0, 550, 1100},
	}
	for _, item := range items {
		mustExec(ctx, tx, "seed check-in preview item", `
			INSERT INTO shop_order_items (
				id, order_id, product_id, variant_id, quantity, fulfilled_quantity,
				unit_price_cents, line_total_cents, product_tag_snapshot,
				product_name_snapshot, variant_label_snapshot, sku_snapshot,
				fulfillment_method, sale_conference_id, pickup_conference_id, status
			)
			VALUES (
				$1::uuid, $2::uuid, $3::uuid, $4::uuid, $9, $10, $11, $12,
				lower(replace($5, ' ', '-')), $5, $6, $7,
				'event_pickup', $8::uuid, $8::uuid, $13
			)
			ON CONFLICT (id) DO UPDATE SET
				order_id = EXCLUDED.order_id,
				product_id = EXCLUDED.product_id,
				variant_id = EXCLUDED.variant_id,
				quantity = EXCLUDED.quantity,
				fulfilled_quantity = EXCLUDED.fulfilled_quantity,
				unit_price_cents = EXCLUDED.unit_price_cents,
				line_total_cents = EXCLUDED.line_total_cents,
				product_name_snapshot = EXCLUDED.product_name_snapshot,
				variant_label_snapshot = EXCLUDED.variant_label_snapshot,
				sku_snapshot = EXCLUDED.sku_snapshot,
				fulfillment_method = EXCLUDED.fulfillment_method,
				sale_conference_id = EXCLUDED.sale_conference_id,
				pickup_conference_id = EXCLUDED.pickup_conference_id,
				status = EXCLUDED.status
		`, item.id, item.orderID, item.productID, item.variantID, item.name, item.variant,
			item.sku, confID, item.quantity, item.fulfilled, item.unitPrice, item.total, item.status)
	}

	pickups := []struct {
		id, itemID string
		pickedUp   bool
	}{
		{devCheckInSponsorPickup, devCheckInSponsorItem, true},
		{devCheckInSpeakerPickup1, devCheckInSpeakerItem1, false},
		{devCheckInSpeakerPickup2, devCheckInSpeakerItem2, false},
	}
	for _, pickup := range pickups {
		mustExec(ctx, tx, "seed check-in preview pickup", `
			INSERT INTO shop_item_pickups (
				id, order_item_id, conference_id, quantity, picked_up_at, picked_up_by, notes
			)
			SELECT $1::uuid, item.id, $3::uuid, item.quantity,
				CASE WHEN $4 THEN '2026-10-01 09:05:00-05'::timestamptz ELSE NULL END,
				CASE WHEN $4 THEN 'dev-volunteer@example.test' ELSE '' END,
				'dev check-in preview'
			FROM shop_order_items item WHERE item.id = $2::uuid
			ON CONFLICT (id) DO UPDATE SET
				order_item_id = EXCLUDED.order_item_id,
				conference_id = EXCLUDED.conference_id,
				quantity = EXCLUDED.quantity,
				picked_up_at = EXCLUDED.picked_up_at,
				picked_up_by = EXCLUDED.picked_up_by,
				notes = EXCLUDED.notes
		`, pickup.id, pickup.itemID, confID, pickup.pickedUp)
	}
}

func seedMissives(ctx context.Context, tx pgx.Tx) {
	missives := []struct {
		id, onlyFor, title, markdown string
		uid                          int
	}{
		{devVolLoginMissiveID, "vollogin", "Your bitcoin++ sign-in link", `Hi,

Use this secure link to sign in to your bitcoin++ account:

[Sign in to bitcoin++]({{ .VolShiftLink }})

If you did not ask for this link, you can ignore this email.

— bitcoin++`, 260044},
		{devVolAppMissiveID, "volapp", "We received your volunteer application", `Hi {{ .Volunteer.Name }},

Your application to volunteer at {{ .Conf.Desc }} is confirmed. A volunteer coordinator will review it and contact you with next steps.

[View your volunteer dashboard]({{ .VolShiftLink }})

— bitcoin++`, 260045},
		{devVolSignupMissiveID, "volsignup", "Choose your volunteer shifts", `Hi {{ .Volunteer.Name }},

Your application to volunteer at {{ .Conf.Desc }} is moving forward. Please choose the shifts that work for you.

[Choose volunteer shifts]({{ .VolShiftLink }})

— bitcoin++`, 260046},
		{devVolShiftsMissiveID, "volshifts", "Your volunteer shifts are confirmed", `Hi {{ .Volunteer.Name }},

You're scheduled to volunteer at {{ .Conf.Desc }}. Your dashboard has your current shifts and event details.

[View your volunteer dashboard]({{ .VolShiftLink }})

— bitcoin++`, 260047},
	}

	for _, missive := range missives {
		mustExec(ctx, tx, "seed "+missive.onlyFor+" missive", `
			INSERT INTO missives (
				id, public_uid, title, newsletters, only_for, markdown, send_at_expr, expiry
			)
			VALUES (
				$1::uuid, $2, $3, '{}'::text[], $4, $5, '', NULL
			)
			ON CONFLICT (id) DO UPDATE SET
				public_uid = EXCLUDED.public_uid,
				title = EXCLUDED.title,
				newsletters = EXCLUDED.newsletters,
				only_for = EXCLUDED.only_for,
				markdown = EXCLUDED.markdown,
				send_at_expr = EXCLUDED.send_at_expr,
				expiry = EXCLUDED.expiry
		`, missive.id, missive.uid, missive.title, missive.onlyFor, missive.markdown)
	}
	conferenceTemplates, err := conferencemissives.Definitions()
	if err != nil {
		log.Fatal(err)
	}
	for i, missive := range conferenceTemplates {
		mustExec(ctx, tx, "seed "+missive.OnlyFor+" missive", `
			INSERT INTO missives (
				public_uid, title, newsletters, only_for, markdown, send_at_expr, expiry, dedupe_key
			)
			VALUES ($1, $2, '{}'::text[], $3, $4, '', NULL, $5)
			ON CONFLICT (dedupe_key) WHERE dedupe_key IS NOT NULL DO UPDATE SET
				title = EXCLUDED.title,
				newsletters = EXCLUDED.newsletters,
				only_for = EXCLUDED.only_for,
				markdown = EXCLUDED.markdown,
				send_at_expr = EXCLUDED.send_at_expr,
				expiry = EXCLUDED.expiry
		`, 260060+i, missive.Title, missive.OnlyFor, missive.Markdown, "conference-template:"+missive.Kind)
	}
}

func seedMerch(ctx context.Context, tx pgx.Tx, confID string) {
	type merchSeed struct {
		productID, variantID, tag, slug, name, subtitle, desc string
		category, sku, image                                  string
		price, stock, weight                                  int
	}
	items := []merchSeed{
		{
			devMerchProduct1ID, devMerchVariant1ID, "core-hat", "core-hat", "Core Hat",
			"Washed rust corduroy, blackletter core.", "Six-panel washed corduroy in a sun-faded rust. Blackletter core embroidered low-profile on the front, bitcoin++ woven tag on the back strap.",
			"apparel", "MERCH-CORE-HAT", "/static/img/merch/core-hat.avif", 3500, 22, 120,
		},
		{
			devMerchProduct2ID, devMerchVariant2ID, "libbit-hat", "libbitcoin-hat", "Libbitcoin Hat",
			"Black twill, pixel-font libbitcoin.", "Structured black twill cap with a pixel-font libbitcoin across the front. Flat-ish brim, snapback closure.",
			"apparel", "MERCH-LIBBIT-HAT", "/static/img/merch/libbit-hat.avif", 3500, 4, 120,
		},
		{
			devMerchProduct3ID, devMerchVariant3ID, "bpp-hat", "bitcoin-hat", "bitcoin++ Hat",
			"Faded denim blue, logo on the back.", "Unstructured faded-denim dad hat. Small ++ on the front, full bitcoin++ wordmark on the back. Curved brim, buckle strap.",
			"apparel", "MERCH-BPP-HAT", "/static/img/merch/librerelay-hat.avif", 3000, 40, 120,
		},
		{
			devMerchProduct4ID, devMerchVariant4ID, "node-tee", "node-runner-tee", "Node Runner Tee",
			"Heavyweight tee, run your own node.", "Heavyweight cotton tee with a full node topology print and RUN YOUR OWN NODE across the back.",
			"apparel", "MERCH-NODE-TEE", "", 3200, 18, 220,
		},
		{
			devMerchProduct5ID, devMerchVariant5ID, "signet-stickers", "signet-sticker-pack", "Signet Sticker Pack",
			"Six die-cut vinyl stickers.", "Six weatherproof die-cut vinyl stickers: ++, mempool, node, lightning bolt, block, and a tiny villain mark. Laptop-lid ready.",
			"stickers", "MERCH-SIGNET-STICKERS", "", 800, 120, 40,
		},
	}

	for _, item := range items {
		mustExec(ctx, tx, "seed merch product", `
			INSERT INTO merch_products (
				id, tag, slug, name, subtitle, description, status, product_type,
				base_price_cents, currency, symbol, requires_shipping, allow_event_pickup,
				stripe_tax_code, easyship_category, country_of_origin
			)
			VALUES (
				$1::uuid, $2, $3, $4, $5, $6, 'published', $7,
				$8, 'USD', '$', true, true, 'txcd_99999999', 'fashion', 'US'
			)
			ON CONFLICT (id) DO UPDATE SET
				tag = EXCLUDED.tag,
				slug = EXCLUDED.slug,
				name = EXCLUDED.name,
				subtitle = EXCLUDED.subtitle,
				description = EXCLUDED.description,
				status = EXCLUDED.status,
				product_type = EXCLUDED.product_type,
				base_price_cents = EXCLUDED.base_price_cents,
				requires_shipping = EXCLUDED.requires_shipping,
				allow_event_pickup = EXCLUDED.allow_event_pickup
		`, item.productID, item.tag, item.slug, item.name, item.subtitle, item.desc, item.category, item.price)

		mustExec(ctx, tx, "seed merch variant", `
			INSERT INTO merch_variants (
				id, product_id, sku, label, weight_grams, inventory_policy, status
			)
			VALUES ($1::uuid, $2::uuid, $3, 'Default', $4, 'deny', 'active')
			ON CONFLICT (id) DO UPDATE SET
				product_id = EXCLUDED.product_id,
				sku = EXCLUDED.sku,
				label = EXCLUDED.label,
				weight_grams = EXCLUDED.weight_grams,
				inventory_policy = EXCLUDED.inventory_policy,
				status = EXCLUDED.status
		`, item.variantID, item.productID, item.sku, item.weight)

		mustExec(ctx, tx, "seed merch initial stock", `
			INSERT INTO merch_inventory_events (
				variant_id, event_type, quantity_delta, actor_email, notes
			)
			SELECT $1::uuid, 'initial', $2, 'dev-admin@example.test', 'seed inventory'
			WHERE NOT EXISTS (
				SELECT 1 FROM merch_inventory_events
				WHERE variant_id = $1::uuid AND event_type = 'initial' AND notes = 'seed inventory'
			)
		`, item.variantID, item.stock)

		if item.image != "" {
			mustExec(ctx, tx, "seed merch image", `
				INSERT INTO merch_product_images (
					product_id, object_key, alt_text, display_order, is_primary
				)
				VALUES ($1::uuid, $2, $3, 0, true)
				ON CONFLICT DO NOTHING
			`, item.productID, item.image, item.name)
		}
	}
	for order, productID := range []string{devMerchProduct1ID, devMerchProduct2ID, devMerchProduct5ID} {
		mustExec(ctx, tx, "seed conference merch upsell", `
			INSERT INTO conference_merch_upsells (conference_id, product_id, display_order)
			VALUES ($1::uuid, $2::uuid, $3)
			ON CONFLICT (conference_id, product_id) DO UPDATE SET
				display_order = EXCLUDED.display_order
		`, confID, productID, order)
	}
	seedMerchPurchases(ctx, tx, confID)
}

func seedMerchPurchases(ctx context.Context, tx pgx.Tx, confID string) {
	orderIDs := []string{
		devShopOrderPickupID,
		devShopOrderShipID,
		devShopOrderPendingID,
		devShopOrderRefundID,
		devShopOrderMixedID,
	}
	itemIDs := []string{
		devShopItemPickupID,
		devShopItemShipID,
		devShopItemPendingID,
		devShopItemRefundID,
		devShopItemMixedMerchID,
		devShopItemMixedTixID,
	}

	mustExec(ctx, tx, "clear dev shop tax transactions", `
		DELETE FROM tax_transactions WHERE order_id::text = ANY($1::text[])
	`, orderIDs)
	mustExec(ctx, tx, "clear dev shop tax quotes", `
		DELETE FROM tax_quotes WHERE order_id::text = ANY($1::text[])
	`, orderIDs)
	mustExec(ctx, tx, "clear dev shop shipments", `
		DELETE FROM shipments WHERE order_id::text = ANY($1::text[])
	`, orderIDs)
	mustExec(ctx, tx, "clear dev shop shipping quotes", `
		DELETE FROM shipping_rate_quotes WHERE order_id::text = ANY($1::text[])
	`, orderIDs)
	mustExec(ctx, tx, "clear dev shop refunds", `
		DELETE FROM refunds WHERE order_id::text = ANY($1::text[])
	`, orderIDs)
	mustExec(ctx, tx, "clear dev shop pickups", `
		DELETE FROM shop_item_pickups WHERE order_item_id::text = ANY($1::text[])
	`, itemIDs)
	mustExec(ctx, tx, "clear dev shop events", `
		DELETE FROM shop_events WHERE order_id::text = ANY($1::text[]) OR order_item_id::text = ANY($2::text[])
	`, orderIDs, itemIDs)
	mustExec(ctx, tx, "clear dev shop inventory events", `
		DELETE FROM merch_inventory_events
		WHERE order_item_id::text = ANY($1::text[])
			AND notes LIKE 'dev seed%'
	`, itemIDs)

	orders := []struct {
		id, publicID, email, name, status, source, kind, provider, providerID string
		subtotal_cents, discount, shipping, tax, total_cents                  int
		paidAt, cancelledAt, createdAt                                        string
	}{
		{devShopOrderPickupID, "dev-shop-pickup-ready", "dev-admin@example.test", "Dev Admin", "paid", "online", "merch", "stripe", "cs_test_dev_pickup", 3500, 0, 0, 0, 3500, "2026-07-02 09:00:00-05", "", "2026-07-02 08:58:00-05"},
		{devShopOrderShipID, "dev-shop-shipped", "dev-admin@example.test", "Dev Admin", "paid", "online", "merch", "stripe", "cs_test_dev_ship", 3500, 0, 1000, 300, 4800, "2026-07-03 11:00:00-05", "", "2026-07-03 10:55:00-05"},
		{devShopOrderPendingID, "dev-shop-pending-card", "dev-admin@example.test", "Dev Admin", "pending", "online", "merch", "stripe", "", 800, 0, 500, 0, 1300, "", "", "2026-07-04 14:00:00-05"},
		{devShopOrderRefundID, "dev-shop-partial-refund", "dev-admin@example.test", "Dev Admin", "partially_refunded", "online", "merch", "stripe", "cs_test_dev_refund", 3000, 0, 0, 200, 3200, "2026-07-05 10:00:00-05", "", "2026-07-05 09:55:00-05"},
		{devShopOrderMixedID, "dev-shop-mixed-ticket-merch", "dev-admin@example.test", "Dev Admin", "paid", "online", "mixed", "stripe", "cs_test_dev_mixed", 18500, 0, 0, 300, 18800, "2026-07-06 16:00:00-05", "", "2026-07-06 15:55:00-05"},
	}
	for _, order := range orders {
		mustExec(ctx, tx, "seed shop order", `
			INSERT INTO shop_orders (
				id, public_id, buyer_email, buyer_name, status, source, checkout_kind,
				payment_provider, payment_provider_id, currency, subtotal_cents, discount_amount_cents,
				shipping_amount_cents, sales_tax_amount_cents, total_cents, paid_at, cancelled_at, created_at
			)
			VALUES (
				$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, 'USD', $10, $11,
				$12, $13, $14, NULLIF($15, '')::timestamptz, NULLIF($16, '')::timestamptz,
				$17::timestamptz
			)
			ON CONFLICT (id) DO UPDATE SET
				public_id = EXCLUDED.public_id,
				buyer_email = EXCLUDED.buyer_email,
				buyer_name = EXCLUDED.buyer_name,
				status = EXCLUDED.status,
				source = EXCLUDED.source,
				checkout_kind = EXCLUDED.checkout_kind,
				payment_provider = EXCLUDED.payment_provider,
				payment_provider_id = EXCLUDED.payment_provider_id,
				currency = EXCLUDED.currency,
				subtotal_cents = EXCLUDED.subtotal_cents,
				discount_amount_cents = EXCLUDED.discount_amount_cents,
				shipping_amount_cents = EXCLUDED.shipping_amount_cents,
				sales_tax_amount_cents = EXCLUDED.sales_tax_amount_cents,
				total_cents = EXCLUDED.total_cents,
				paid_at = EXCLUDED.paid_at,
				cancelled_at = EXCLUDED.cancelled_at,
				created_at = EXCLUDED.created_at
		`, order.id, order.publicID, order.email, order.name, order.status, order.source,
			order.kind, order.provider, order.providerID, order.subtotal_cents, order.discount,
			order.shipping, order.tax, order.total_cents, order.paidAt, order.cancelledAt, order.createdAt)
	}

	items := []struct {
		id, orderID, productID, variantID, name, variantLabel, sku, fulfillment, saleConfID, pickupConfID, status string
		quantity, fulfilled, refunded, unitPrice, discount, tax, lineTotalCents                                   int
	}{
		{devShopItemPickupID, devShopOrderPickupID, devMerchProduct1ID, devMerchVariant1ID, "Core Hat", "Default", "MERCH-CORE-HAT", "event_pickup", confID, confID, "ready", 1, 0, 0, 3500, 0, 0, 3500},
		{devShopItemShipID, devShopOrderShipID, devMerchProduct2ID, devMerchVariant2ID, "Libbitcoin Hat", "Default", "MERCH-LIBBIT-HAT", "ship", "", "", "pending", 1, 0, 0, 3500, 0, 300, 3500},
		{devShopItemPendingID, devShopOrderPendingID, devMerchProduct5ID, devMerchVariant5ID, "Signet Sticker Pack", "Default", "MERCH-SIGNET-STICKERS", "ship", "", "", "pending", 1, 0, 0, 800, 0, 0, 800},
		{devShopItemRefundID, devShopOrderRefundID, devMerchProduct3ID, devMerchVariant3ID, "bitcoin++ Hat", "Default", "MERCH-BPP-HAT", "pos_takeaway", confID, "", "partially_refunded", 1, 1, 1, 3000, 0, 200, 3000},
		{devShopItemMixedMerchID, devShopOrderMixedID, devMerchProduct1ID, devMerchVariant1ID, "Core Hat", "Default", "MERCH-CORE-HAT", "event_pickup", confID, confID, "fulfilled", 1, 1, 0, 3500, 0, 300, 3500},
		{devShopItemMixedTixID, devShopOrderMixedID, "", "", "General Admission", "Ticket", "DEV26-GENERAL", "pos_takeaway", confID, "", "fulfilled", 1, 1, 0, 15000, 0, 0, 15000},
	}
	for _, item := range items {
		mustExec(ctx, tx, "seed shop order item", `
			INSERT INTO shop_order_items (
				id, order_id, product_id, variant_id, quantity, fulfilled_quantity,
				refunded_quantity, unit_price_cents, discount_amount_cents, tax_amount_cents, line_total_cents,
				product_tag_snapshot, product_name_snapshot, variant_label_snapshot,
				sku_snapshot, fulfillment_method, sale_conference_id, pickup_conference_id,
				status, created_at
			)
			VALUES (
				$1::uuid, $2::uuid, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid,
				$5, $6, $7, $8, $9, $10, $11, lower(replace($12, ' ', '-')), $12,
				$13, $14, $15, NULLIF($16, '')::uuid, NULLIF($17, '')::uuid,
				$18, now()
			)
			ON CONFLICT (id) DO UPDATE SET
				order_id = EXCLUDED.order_id,
				product_id = EXCLUDED.product_id,
				variant_id = EXCLUDED.variant_id,
				quantity = EXCLUDED.quantity,
				fulfilled_quantity = EXCLUDED.fulfilled_quantity,
				refunded_quantity = EXCLUDED.refunded_quantity,
				unit_price_cents = EXCLUDED.unit_price_cents,
				discount_amount_cents = EXCLUDED.discount_amount_cents,
				tax_amount_cents = EXCLUDED.tax_amount_cents,
				line_total_cents = EXCLUDED.line_total_cents,
				product_tag_snapshot = EXCLUDED.product_tag_snapshot,
				product_name_snapshot = EXCLUDED.product_name_snapshot,
				variant_label_snapshot = EXCLUDED.variant_label_snapshot,
				sku_snapshot = EXCLUDED.sku_snapshot,
				fulfillment_method = EXCLUDED.fulfillment_method,
				sale_conference_id = EXCLUDED.sale_conference_id,
				pickup_conference_id = EXCLUDED.pickup_conference_id,
				status = EXCLUDED.status
		`, item.id, item.orderID, item.productID, item.variantID, item.quantity, item.fulfilled,
			item.refunded, item.unitPrice, item.discount, item.tax, item.lineTotalCents, item.name,
			item.variantLabel, item.sku, item.fulfillment, item.saleConfID, item.pickupConfID, item.status)
	}

	mustExec(ctx, tx, "seed pending shop pickup", `
		INSERT INTO shop_item_pickups (id, order_item_id, conference_id, quantity, picked_up_at, picked_up_by, notes)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 1, NULL, '', 'ready for dev pickup')
		ON CONFLICT (id) DO UPDATE SET
			order_item_id = EXCLUDED.order_item_id,
			conference_id = EXCLUDED.conference_id,
			quantity = EXCLUDED.quantity,
			picked_up_at = NULL,
			picked_up_by = '',
			notes = EXCLUDED.notes
	`, devShopPickupReadyID, devShopItemPickupID, confID)
	mustExec(ctx, tx, "seed completed shop pickup", `
		INSERT INTO shop_item_pickups (id, order_item_id, conference_id, quantity, picked_up_at, picked_up_by, notes)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 1, '2026-07-06 16:20:00-05'::timestamptz, 'dev-volunteer@example.test', 'dev seed completed pickup')
		ON CONFLICT (id) DO UPDATE SET
			order_item_id = EXCLUDED.order_item_id,
			conference_id = EXCLUDED.conference_id,
			quantity = EXCLUDED.quantity,
			picked_up_at = EXCLUDED.picked_up_at,
			picked_up_by = EXCLUDED.picked_up_by,
			notes = EXCLUDED.notes
	`, devShopPickupDoneID, devShopItemMixedMerchID, confID)

	mustExec(ctx, tx, "seed shipping rate quote", `
		INSERT INTO shipping_rate_quotes (
			order_id, provider, provider_quote_id, destination_country, destination_region,
			destination_postal_code, courier_name, service_name, amount_cents, currency,
			estimated_min_days, estimated_max_days, raw_response, expires_at
		)
		VALUES (
			$1::uuid, 'easyship', 'rate_dev_standard', 'US', 'TX', '78701',
			'USPS', 'Ground Advantage', 1000, 'USD', 3, 5,
			'{"fixture": true, "provider": "easyship"}'::jsonb,
			'2026-07-04 11:00:00-05'::timestamptz
		)
	`, devShopOrderShipID)
	mustExec(ctx, tx, "seed shipment", `
		INSERT INTO shipments (
			order_id, provider, provider_shipment_id, provider_label_id, courier_name,
			service_name, tracking_number, tracking_url, label_url, status, raw_response,
			shipped_at
		)
		VALUES (
			$1::uuid, 'easyship', 'ship_dev_001', 'label_dev_001', 'USPS',
			'Ground Advantage', 'DEVTRACK123', 'https://example.test/tracking/DEVTRACK123',
			'https://example.test/labels/DEVTRACK123.pdf', 'label_created',
			'{"fixture": true, "provider": "easyship"}'::jsonb,
			'2026-07-03 15:00:00-05'::timestamptz
		)
	`, devShopOrderShipID)
	mustExec(ctx, tx, "seed tax quote", `
		INSERT INTO tax_quotes (
			order_id, sales_tax_provider, sales_tax_amount_cents, import_provider,
			import_duty_amount_cents, import_tax_amount_cents, incoterm, destination_country,
			destination_region, destination_postal_code, raw_tax_response, raw_import_response,
			expires_at
		)
		VALUES (
			$1::uuid, 'stripe', 300, 'easyship', 0, 0, 'DDU', 'US', 'TX', '78701',
			'{"fixture": true, "provider": "stripe"}'::jsonb,
			'{"fixture": true, "provider": "easyship"}'::jsonb,
			'2026-07-04 11:00:00-05'::timestamptz
		)
	`, devShopOrderShipID)
	mustExec(ctx, tx, "seed tax transaction", `
		INSERT INTO tax_transactions (
			order_id, provider, provider_transaction_id, sales_tax_amount_cents, status, raw_response
		)
		VALUES (
			$1::uuid, 'stripe', 'tax_dev_ship', 300, 'recorded',
			'{"fixture": true, "provider": "stripe"}'::jsonb
		)
	`, devShopOrderShipID)
	mustExec(ctx, tx, "seed shop refund", `
		INSERT INTO refunds (
			id, order_id, provider, provider_refund_id, amount_cents, currency, reason,
			status, requested_by, raw_response, completed_at
		)
		VALUES (
			$1::uuid, $2::uuid, 'stripe', 're_dev_partial', 3200, 'USD',
			'dev fixture partial return', 'succeeded', 'dev-admin@example.test',
			'{"fixture": true, "provider": "stripe"}'::jsonb,
			'2026-07-05 12:00:00-05'::timestamptz
		)
		ON CONFLICT (id) DO UPDATE SET
			order_id = EXCLUDED.order_id,
			provider = EXCLUDED.provider,
			provider_refund_id = EXCLUDED.provider_refund_id,
			amount_cents = EXCLUDED.amount_cents,
			currency = EXCLUDED.currency,
			reason = EXCLUDED.reason,
			status = EXCLUDED.status,
			requested_by = EXCLUDED.requested_by,
			raw_response = EXCLUDED.raw_response,
			completed_at = EXCLUDED.completed_at
	`, devShopRefundID, devShopOrderRefundID)
	mustExec(ctx, tx, "seed shop refund item", `
		INSERT INTO refund_items (refund_id, order_item_id, quantity, amount_cents, restock)
		VALUES ($1::uuid, $2::uuid, 1, 3200, true)
		ON CONFLICT (refund_id, order_item_id) DO UPDATE SET
			quantity = EXCLUDED.quantity,
			amount_cents = EXCLUDED.amount_cents,
			restock = EXCLUDED.restock
	`, devShopRefundID, devShopItemRefundID)

	inventoryEvents := []struct {
		variantID, eventType, itemID, note string
		delta                              int
	}{
		{devMerchVariant1ID, "sale", devShopItemPickupID, "dev seed paid pickup sale", -1},
		{devMerchVariant2ID, "sale", devShopItemShipID, "dev seed shipped sale", -1},
		{devMerchVariant5ID, "reservation", devShopItemPendingID, "dev seed pending card reservation", -1},
		{devMerchVariant3ID, "sale", devShopItemRefundID, "dev seed refunded sale", -1},
		{devMerchVariant3ID, "refund", devShopItemRefundID, "dev seed refund restock", 1},
		{devMerchVariant1ID, "sale", devShopItemMixedMerchID, "dev seed mixed sale", -1},
		{devMerchVariant1ID, "pickup", devShopItemMixedMerchID, "dev seed completed pickup", 0},
	}
	for _, event := range inventoryEvents {
		mustExec(ctx, tx, "seed merch inventory event", `
			INSERT INTO merch_inventory_events (
				variant_id, event_type, quantity_delta, order_item_id, conference_id,
				actor_email, notes
			)
			VALUES (
				$1::uuid, $2, $3, $4::uuid, $5::uuid,
				'dev-admin@example.test', $6
			)
		`, event.variantID, event.eventType, event.delta, event.itemID, confID, event.note)
	}

	for _, orderID := range orderIDs {
		mustExec(ctx, tx, "seed shop order event", `
			INSERT INTO shop_events (event_type, actor_type, actor_email, entity_type, entity_id, order_id, metadata)
			VALUES ('order.seeded', 'system', 'dev-admin@example.test', 'shop_order', $1::uuid, $1::uuid, '{"fixture": true}'::jsonb)
		`, orderID)
	}
}

func seedConference(ctx context.Context, tx pgx.Tx) string {
	var confID string
	err := tx.QueryRow(ctx, `
		INSERT INTO conferences (
			id, tag, public_uid, active, publication_status, description, edition_type, og_flavor, emoji, tagline,
			date_desc, start_date, end_date, timezone, location, venue,
			venue_map_url, venue_website_url,
			pickup_address_line1, pickup_address_line2, pickup_address_city,
			pickup_address_region, pickup_address_postal_code, pickup_address_country,
			show_hackathon, orient_cal_notif,
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
			'900 Olive St', '', 'Austin', 'TX', '78702', 'US',
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
			pickup_address_line1 = EXCLUDED.pickup_address_line1,
			pickup_address_line2 = EXCLUDED.pickup_address_line2,
			pickup_address_city = EXCLUDED.pickup_address_city,
			pickup_address_region = EXCLUDED.pickup_address_region,
			pickup_address_postal_code = EXCLUDED.pickup_address_postal_code,
			pickup_address_country = EXCLUDED.pickup_address_country,
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

func seedTickets(ctx context.Context, tx pgx.Tx, confID string, newsletterSendAt time.Time) {
	for _, tix := range devTickets {
		salesEndAt := tix.expiresStart
		if tix.id == devEarlyTixID {
			salesEndAt = newsletterSendAt.AddDate(0, 0, 4).Format(time.RFC3339)
		}
		mustExec(ctx, tx, "seed ticket", `
			INSERT INTO conference_tickets (
				id, conference_id, ticket_key, tier, local_price, btc_price, usd_price,
				base_price, card_surcharge_bps, sales_end_at, max_count, currency, symbol, post_symbol,
				stripe_tax_code
			)
			VALUES (
				$1::uuid, $2::uuid, $3, $4, $5, $6, $7,
				$6, 1000, $8::timestamptz, $9, $10, $11, $12, 'txcd_00000000'
			)
			ON CONFLICT (conference_id, ticket_key) DO UPDATE SET
				tier = EXCLUDED.tier,
				local_price = EXCLUDED.local_price,
				btc_price = EXCLUDED.btc_price,
				usd_price = EXCLUDED.usd_price,
				base_price = EXCLUDED.base_price,
				card_surcharge_bps = EXCLUDED.card_surcharge_bps,
				sales_end_at = EXCLUDED.sales_end_at,
				max_count = EXCLUDED.max_count,
				currency = EXCLUDED.currency,
				symbol = EXCLUDED.symbol,
				post_symbol = EXCLUDED.post_symbol,
				stripe_tax_code = EXCLUDED.stripe_tax_code
		`, tix.id, confID, tix.key, tix.tier, tix.local, tix.btc, tix.usd,
			salesEndAt, tix.max, tix.currency, tix.symbol, tix.postSymbol)
	}
}

func nextWeeklyNewsletterSendAt(now time.Time) time.Time {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		loc = time.FixedZone("America/Chicago", -6*60*60)
	}
	localNow := now.In(loc)
	days := (int(time.Tuesday) - int(localNow.Weekday()) + 7) % 7
	candidate := time.Date(localNow.Year(), localNow.Month(), localNow.Day()+days, 10, 0, 0, 0, loc)
	if !candidate.After(localNow) {
		candidate = candidate.AddDate(0, 0, 7)
	}
	return candidate
}

func seedAdmin(ctx context.Context, tx pgx.Tx) {
	mustExec(ctx, tx, "seed admin person", `
		INSERT INTO people (
			id, name, norm_photo_path, phone, signal, telegram, twitter_handle,
			nostr, github_url, instagram, linkedin, website_url, company,
			org_logo_path, avail_to_hire, looking_to_hire, tshirt
		)
		VALUES (
			$1::uuid, 'Dev Admin', '', '', '', '', '',
			'', '', '', '', '', 'bitcoin++ local dev', '', false, false, 'MM'
		)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			company = EXCLUDED.company,
			tshirt = EXCLUDED.tshirt
	`, devAdminID)
	seedPersonEmail(ctx, tx, devAdminID, "dev-admin@example.test")

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
				id, name, norm_photo_path, phone, signal, telegram, twitter_handle,
				nostr, github_url, instagram, linkedin, leetcode, website_url, company,
				org_logo_path, avail_to_hire, looking_to_hire, tshirt
			)
			VALUES (
				$1::uuid, $2, $3, '', '', '', $4,
				'', $5, '', '', $6, $7, $8, '', false, false, ''
			)
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				norm_photo_path = EXCLUDED.norm_photo_path,
				twitter_handle = EXCLUDED.twitter_handle,
				github_url = EXCLUDED.github_url,
				leetcode = EXCLUDED.leetcode,
				website_url = EXCLUDED.website_url,
				company = EXCLUDED.company
		`, sp.personID, sp.name, sp.photo, sp.twitter, sp.github, sp.leetcode, sp.website, sp.company)
		seedPersonEmail(ctx, tx, sp.personID, sp.email)

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

func seedWeeklyNewsletterFixtures(ctx context.Context, tx pgx.Tx, confID string, newsletterSendAt time.Time) {
	// Give the weekly draft one newly published product and one genuine manual
	// restock inside its lookback window. Keep the other seeded products old so
	// repeated seed runs produce a stable, readable merch update list.
	mustExec(ctx, tx, "age newsletter merch publication dates", `
		UPDATE merch_products
		SET published_at = $1::timestamptz - interval '30 days'
		WHERE id::text = ANY($2::text[])
	`, newsletterSendAt, []string{devMerchProduct1ID, devMerchProduct2ID, devMerchProduct3ID, devMerchProduct4ID, devMerchProduct5ID})
	mustExec(ctx, tx, "seed newly published newsletter merch", `
		UPDATE merch_products
		SET published_at = $1::timestamptz - interval '3 days'
		WHERE id = $2::uuid
	`, newsletterSendAt, devMerchProduct4ID)
	mustExec(ctx, tx, "reset newsletter merch restock", `
		DELETE FROM merch_inventory_events
		WHERE variant_id = $1::uuid AND notes = 'weekly newsletter restock fixture'
	`, devMerchVariant1ID)
	mustExec(ctx, tx, "seed newsletter merch restock", `
		INSERT INTO merch_inventory_events (
			variant_id, event_type, quantity_delta, actor_email, notes, occurred_at
		) VALUES (
			$1::uuid, 'increase', 12, 'dev-admin@example.test',
			'weekly newsletter restock fixture', $2::timestamptz - interval '2 days'
		)
	`, devMerchVariant1ID, newsletterSendAt)

	// Keep these inside the newsletter's seven-day lookback so the generated
	// draft demonstrates the broadcasts section using already-public recordings.
	recordings := []struct {
		id, talkID, title, slug string
		publishedDaysAgo        int
	}{
		{devNewsletterRecording, devSpeakers[len(devSpeakers)-1].talkID, "Async Payments and Mobile Reliability", "async-payments-panel", 1},
		{devNewsletterRecording2, devSpeakers[0].talkID, "Package Relay in Practice", "package-relay", 2},
		{devNewsletterRecording3, devSpeakers[1].talkID, "Building a Signet-First Release Pipeline", "signet-release-pipeline", 3},
		{devNewsletterRecording4, devSpeakers[2].talkID, "Designing Good Failure Modes for Lightning Apps", "lightning-failure-modes", 4},
	}
	for _, recording := range recordings {
		publishAt := newsletterSendAt.AddDate(0, 0, -recording.publishedDaysAgo)
		mustExec(ctx, tx, "seed newsletter recording", `
		INSERT INTO recordings (
			id, conf_talk_id, talk_name, youtube_url, x_url, x_reply_url,
			file_uri, publish_at
		)
		VALUES (
			$1::uuid, $2::uuid, $3,
			'https://www.youtube.com/watch?v=dev-' || $4, '', '',
			'spaces://dev-fixture/' || $4 || '.mp4', $5::timestamptz
		)
		ON CONFLICT (id) DO UPDATE SET
			conf_talk_id = EXCLUDED.conf_talk_id,
			talk_name = EXCLUDED.talk_name,
			youtube_url = EXCLUDED.youtube_url,
			x_url = EXCLUDED.x_url,
			x_reply_url = EXCLUDED.x_reply_url,
			file_uri = EXCLUDED.file_uri,
			publish_at = EXCLUDED.publish_at
	`, recording.id, recording.talkID, recording.title, recording.slug, publishAt)
	}

	var competitionID string
	err := tx.QueryRow(ctx, `
		INSERT INTO competitions (
			id, conference_id, title, description, description_format, visibility,
			lifecycle_override, public_gallery_enabled, public_tables_enabled,
			public_gallery_at, hacking_starts_at, hacking_ends_at,
			results_finalized_at, results_finalized_by
		)
		VALUES (
			$1::uuid, $2::uuid, 'Signet Builders Sprint',
			'A compact development hackathon fixture with public, finalized results.',
			'markdown', 'public', 'closed', true, true,
			now() - interval '1 day', now() - interval '4 days', now() - interval '2 days',
			now(), $3::uuid
		)
		ON CONFLICT (conference_id) DO UPDATE SET
			title = EXCLUDED.title,
			description = EXCLUDED.description,
			description_format = EXCLUDED.description_format,
			visibility = EXCLUDED.visibility,
			lifecycle_override = EXCLUDED.lifecycle_override,
			public_gallery_enabled = EXCLUDED.public_gallery_enabled,
			public_tables_enabled = EXCLUDED.public_tables_enabled,
			public_gallery_at = EXCLUDED.public_gallery_at,
			hacking_starts_at = EXCLUDED.hacking_starts_at,
			hacking_ends_at = EXCLUDED.hacking_ends_at,
			results_finalized_at = EXCLUDED.results_finalized_at,
			results_finalized_by = EXCLUDED.results_finalized_by
		RETURNING id::text
	`, devNewsletterCompetition, confID, devAdminID).Scan(&competitionID)
	if err != nil {
		log.Fatal(fmt.Errorf("seed newsletter competition: %w", err))
	}

	type newsletterProjectSeed struct {
		id, ownerPersonID, slug, title, shortDescription, description, githubURL string
		number                                                                   int
	}
	projects := []newsletterProjectSeed{
		{
			id:               devNewsletterProject1,
			ownerPersonID:    devSpeakers[0].personID,
			slug:             "mempool-observatory",
			title:            "Mempool Observatory",
			shortDescription: "A live package-relay and fee-pressure explorer for Signet.",
			description:      "Turns package relay behavior into a visual timeline so developers can inspect propagation, replacement, and fee-bumping decisions.",
			githubURL:        "https://github.com/example/mempool-observatory",
			number:           7,
		},
		{
			id:               devNewsletterProject2,
			ownerPersonID:    devSpeakers[2].personID,
			slug:             "signet-arcade",
			title:            "Signet Arcade",
			shortDescription: "Collaborative protocol puzzles backed by reproducible Signet fixtures.",
			description:      "A playful browser lab for learning transaction construction, policy failures, and recovery paths with teammates.",
			githubURL:        "https://github.com/example/signet-arcade",
			number:           12,
		},
	}
	projectIDs := make(map[string]string, len(projects))
	for _, project := range projects {
		var projectID string
		err := tx.QueryRow(ctx, `
			INSERT INTO projects (
				id, competition_id, created_by_person_id, slug, title,
				short_description, description, description_format, github_url,
				project_number, status, tags, submitted_at
			)
			VALUES (
				$1::uuid, $2::uuid, $3::uuid, $4, $5,
				$6, $7, 'markdown', $8, $9, 'submitted',
				ARRAY['signet', 'developer-tools'], now() - interval '2 days'
			)
			ON CONFLICT (competition_id, slug) DO UPDATE SET
				created_by_person_id = EXCLUDED.created_by_person_id,
				title = EXCLUDED.title,
				short_description = EXCLUDED.short_description,
				description = EXCLUDED.description,
				description_format = EXCLUDED.description_format,
				github_url = EXCLUDED.github_url,
				project_number = EXCLUDED.project_number,
				status = EXCLUDED.status,
				tags = EXCLUDED.tags,
				submitted_at = EXCLUDED.submitted_at
			RETURNING id::text
		`, project.id, competitionID, project.ownerPersonID, project.slug,
			project.title, project.shortDescription, project.description,
			project.githubURL, project.number).Scan(&projectID)
		if err != nil {
			log.Fatal(fmt.Errorf("seed newsletter project %s: %w", project.slug, err))
		}
		mustExec(ctx, tx, "seed newsletter project owner", `
			INSERT INTO project_members (project_id, person_id, role)
			VALUES ($1::uuid, $2::uuid, 'owner')
			ON CONFLICT (project_id, person_id) DO UPDATE SET role = EXCLUDED.role
		`, projectID, project.ownerPersonID)
		projectIDs[project.slug] = projectID
	}

	type newsletterAwardSeed struct {
		id, title, description, projectSlug string
		rank                                int
	}
	awards := []newsletterAwardSeed{
		{devNewsletterAward1, "Best Overall", "The strongest complete project from the sprint.", "mempool-observatory", 1},
		{devNewsletterAward2, "Best Developer Tool", "The tool most likely to improve a Bitcoin developer's daily workflow.", "mempool-observatory", 2},
		{devNewsletterAward3, "Community Choice", "The project selected by sprint participants.", "signet-arcade", 3},
	}
	for _, award := range awards {
		mustExec(ctx, tx, "seed newsletter award", `
			INSERT INTO awards (
				id, competition_id, title, description, max_awardees,
				status, award_type, award_rank
			)
			VALUES ($1::uuid, $2::uuid, $3, $4, 1, 'awarded', 'normal', $5)
			ON CONFLICT (id) DO UPDATE SET
				competition_id = EXCLUDED.competition_id,
				title = EXCLUDED.title,
				description = EXCLUDED.description,
				max_awardees = EXCLUDED.max_awardees,
				status = EXCLUDED.status,
				award_type = EXCLUDED.award_type,
				award_rank = EXCLUDED.award_rank,
				archived_at = NULL
		`, award.id, competitionID, award.title, award.description, award.rank)
		mustExec(ctx, tx, "seed newsletter project award", `
			INSERT INTO project_awards (project_id, award_id, awarded_at)
			VALUES ($1::uuid, $2::uuid, now())
			ON CONFLICT (project_id, award_id) DO UPDATE SET
				awarded_at = EXCLUDED.awarded_at
		`, projectIDs[award.projectSlug], award.id)
	}
}

func seedSponsors(ctx context.Context, tx pgx.Tx, confID, pastConfID string) {
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
	mustExec(ctx, tx, "seed past sponsor trophy", `
		INSERT INTO sponsorships (
			id, organization_id, name, level, label, status, notes
		) VALUES (
			$1::uuid, $2::uuid, 'Signet Systems @ Satoshi', 'Satoshi',
			'Satoshi Sponsors', 'Paid', 'Local dev past sponsor trophy fixture.'
		)
		ON CONFLICT (id) DO UPDATE SET
			organization_id = EXCLUDED.organization_id,
			name = EXCLUDED.name,
			level = EXCLUDED.level,
			label = EXCLUDED.label,
			status = EXCLUDED.status,
			archived_at = NULL
	`, devPastSponsorship, devOrgs[0].id)
	mustExec(ctx, tx, "seed past sponsor conference link", `
		INSERT INTO sponsorships_conferences (sponsorship_id, conference_id)
		VALUES ($1::uuid, $2::uuid)
		ON CONFLICT DO NOTHING
	`, devPastSponsorship, pastConfID)
	mustExec(ctx, tx, "seed past sponsor entitlement", `
		INSERT INTO sponsorship_entitlements (
			sponsorship_id, conference_id, ticket_allocation,
			sponsor_award_limit, can_edit_organization
		) VALUES ($1::uuid, $2::uuid, 10, 1, false)
		ON CONFLICT (sponsorship_id, conference_id) DO UPDATE SET
			ticket_allocation = EXCLUDED.ticket_allocation,
			sponsor_award_limit = EXCLUDED.sponsor_award_limit,
			can_edit_organization = EXCLUDED.can_edit_organization
	`, devPastSponsorship, pastConfID)

	// Mara and the local admin exercise a multi-manager sponsor workspace.
	for _, member := range []struct {
		personID, role string
	}{
		{devSpeakers[0].personID, "owner"},
		{devAdminID, "manager"},
	} {
		mustExec(ctx, tx, "seed sponsor organization membership", `
			INSERT INTO organization_memberships (
				organization_id, person_id, role, status, invited_by_person_id
			) VALUES ($1::uuid, $2::uuid, $3, 'active', $4::uuid)
			ON CONFLICT (organization_id, person_id) DO UPDATE SET
				role = EXCLUDED.role,
				status = EXCLUDED.status,
				invited_by_person_id = EXCLUDED.invited_by_person_id
		`, devOrgs[0].id, member.personID, member.role, devAdminID)
	}

	for _, entitlement := range []struct {
		sponsorshipID                                      string
		tickets, awards                                    int
		contacts, exportContacts, judges, editOrganization bool
	}{
		{devSponsorships[0].id, 20, 2, true, false, true, true},
		{devSponsorships[1].id, 12, 1, false, false, true, true},
		{devSponsorships[2].id, 8, 0, false, false, false, true},
		{devSponsorships[3].id, 10, 1, true, false, true, true},
		{devSponsorships[4].id, 2, 0, false, false, false, true},
	} {
		mustExec(ctx, tx, "seed sponsorship entitlement", `
			INSERT INTO sponsorship_entitlements (
				sponsorship_id, conference_id, ticket_allocation,
				sponsor_award_limit, participant_contact_access,
				participant_contact_export, can_manage_award_judges,
				can_edit_organization
			) VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (sponsorship_id, conference_id) DO UPDATE SET
				ticket_allocation = EXCLUDED.ticket_allocation,
				sponsor_award_limit = EXCLUDED.sponsor_award_limit,
				participant_contact_access = EXCLUDED.participant_contact_access,
				participant_contact_export = EXCLUDED.participant_contact_export,
				can_manage_award_judges = EXCLUDED.can_manage_award_judges,
				can_edit_organization = EXCLUDED.can_edit_organization
		`, entitlement.sponsorshipID, confID, entitlement.tickets,
			entitlement.awards, entitlement.contacts, entitlement.exportContacts,
			entitlement.judges, entitlement.editOrganization)
	}

	// A sponsor award and participant preference make the consent-aware prize
	// path visible in the seeded hackathon.
	mustExec(ctx, tx, "seed sponsor award", `
		INSERT INTO awards (
			id, competition_id, sponsored_by_org_id, award_type, title,
			description, judging_instructions, max_awardees,
			opt_in_required, status
		) VALUES (
			$1::uuid, $2::uuid, $3::uuid, 'challenge',
			'Best Signet Infrastructure',
			'For the project that makes reproducible bitcoin testing easier.',
			'Prefer tools with a documented path from local fixtures to Signet.',
			1, true, 'awarded'
		)
		ON CONFLICT (id) DO UPDATE SET
			sponsored_by_org_id = EXCLUDED.sponsored_by_org_id,
			title = EXCLUDED.title,
			description = EXCLUDED.description,
			judging_instructions = EXCLUDED.judging_instructions,
			max_awardees = EXCLUDED.max_awardees,
			opt_in_required = EXCLUDED.opt_in_required,
			status = EXCLUDED.status,
			archived_at = NULL
	`, devSponsorAward, devNewsletterCompetition, devOrgs[0].id)
	mustExec(ctx, tx, "seed sponsor prize", `
		INSERT INTO prizes (
			id, award_id, prize_type, title, description, value_text, status
		) VALUES (
			$1::uuid, $2::uuid, 'sats', 'Signet Systems prize',
			'Paid directly to the winning project team.', '1000000', 'awarded'
		)
		ON CONFLICT (id) DO UPDATE SET
			title = EXCLUDED.title,
			description = EXCLUDED.description,
			value_text = EXCLUDED.value_text,
			status = EXCLUDED.status
	`, devSponsorPrize, devSponsorAward)
	mustExec(ctx, tx, "seed sponsor award opt in", `
		INSERT INTO project_award_opt_ins (project_id, award_id)
		VALUES ($1::uuid, $2::uuid)
		ON CONFLICT DO NOTHING
	`, devNewsletterProject1, devSponsorAward)
	mustExec(ctx, tx, "seed sponsor project award", `
		INSERT INTO project_awards (project_id, award_id, awarded_at)
		VALUES ($1::uuid, $2::uuid, now())
		ON CONFLICT (project_id, award_id) DO UPDATE SET awarded_at = EXCLUDED.awarded_at
	`, devNewsletterProject1, devSponsorAward)
	mustExec(ctx, tx, "seed sponsor contact consent", `
		INSERT INTO hackathon_sponsor_contact_consents (
			competition_id, person_id, all_hackathon_sponsors,
			entered_award_sponsors
		) VALUES ($1::uuid, $2::uuid, false, true)
		ON CONFLICT (competition_id, person_id) DO UPDATE SET
			all_hackathon_sponsors = EXCLUDED.all_hackathon_sponsors,
			entered_award_sponsors = EXCLUDED.entered_award_sponsors
	`, devNewsletterCompetition, devSpeakers[0].personID)
	mustExec(ctx, tx, "seed sponsor contact consent event", `
		INSERT INTO hackathon_sponsor_contact_consent_events (
			id, competition_id, person_id, all_hackathon_sponsors,
			entered_award_sponsors, policy_version, source
		) VALUES ($1::uuid, $2::uuid, $3::uuid, false, true, 'sponsor-contact-v1', 'dev-seed')
		ON CONFLICT (id) DO UPDATE SET
			all_hackathon_sponsors = EXCLUDED.all_hackathon_sponsors,
			entered_award_sponsors = EXCLUDED.entered_award_sponsors,
			policy_version = EXCLUDED.policy_version,
			source = EXCLUDED.source
	`, devSponsorConsentEvent, devNewsletterCompetition, devSpeakers[0].personID)
}

func seedHotels(ctx context.Context, tx pgx.Tx, confID string) {
	for _, hotel := range devHotels {
		mustExec(ctx, tx, "seed hotel", `
			INSERT INTO hotels (
				id, conference_id, name, url, img_path, type, description, price_range,
				display_order, archived_at
			)
			VALUES (
				$1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, NULL
			)
			ON CONFLICT (id) DO UPDATE SET
				conference_id = EXCLUDED.conference_id,
				name = EXCLUDED.name,
				url = EXCLUDED.url,
				img_path = EXCLUDED.img_path,
				type = EXCLUDED.type,
				description = EXCLUDED.description,
				price_range = EXCLUDED.price_range,
				display_order = EXCLUDED.display_order,
				archived_at = NULL
		`, hotel.id, confID, hotel.name, hotel.url, hotel.img, hotel.hotelType, hotel.desc, hotel.priceRange, hotel.order)
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
				id, name, norm_photo_path, phone, signal, telegram, twitter_handle,
				nostr, github_url, instagram, linkedin, website_url, company,
				org_logo_path, avail_to_hire, looking_to_hire, tshirt
			)
			VALUES (
				$1::uuid, $2, $3, '', '', '', '',
				'', '', '', '', '', $4, '', false, false, ''
			)
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				norm_photo_path = EXCLUDED.norm_photo_path,
				company = EXCLUDED.company
		`, person.id, person.name, person.photo, person.company)
		seedPersonEmail(ctx, tx, person.id, person.email)
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
			id, person_id, availability, contact_at, comments, discovered_via,
			first_event, hometown, status, captcha, subscribe
		)
		VALUES (
			$1::uuid, $2::uuid, $3, 'Email', 'Seeded volunteer fixture.',
			'local dev harness', false, 'Austin, TX', 'Scheduled', 0, false
		)
		ON CONFLICT (id) DO UPDATE SET
			person_id = EXCLUDED.person_id,
			availability = EXCLUDED.availability,
			contact_at = EXCLUDED.contact_at,
			comments = EXCLUDED.comments,
			discovered_via = EXCLUDED.discovered_via,
			hometown = EXCLUDED.hometown,
			status = EXCLUDED.status
	`, devVolunteerID, devAdminID, []string{"Day 1", "Day 2"})

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

func seedPersonEmail(ctx context.Context, tx pgx.Tx, personID, email string) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return
	}
	mustExec(ctx, tx, "seed person email", `
		INSERT INTO person_emails (person_id, email, is_primary, verified_at)
		VALUES ($1::uuid, $2::citext, true, now())
		ON CONFLICT (email) DO UPDATE SET
			person_id = EXCLUDED.person_id,
			is_primary = true,
			verified_at = EXCLUDED.verified_at,
			updated_at = now()
	`, personID, email)
}
