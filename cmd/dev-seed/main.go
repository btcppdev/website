package main

import (
	"context"
	"fmt"
	"log"

	"btcpp-web/internal/db"
	"btcpp-web/internal/envconfig"
)

const (
	devConfID  = "00000000-0000-4000-8000-000000000001"
	devDayID   = "00000000-0000-4000-8000-000000000002"
	devTixID   = "00000000-0000-4000-8000-000000000003"
	devAdminID = "00000000-0000-4000-8000-000000000004"
)

func main() {
	env, err := envconfig.Load(".env")
	if err != nil {
		log.Fatal(err)
	}
	if env.Prod {
		log.Fatal("refusing to seed while PROD=true")
	}

	pool, err := db.Open(context.Background(), env.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	tx, err := pool.Begin(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(context.Background())

	var confID string
	err = tx.QueryRow(context.Background(), `
		INSERT INTO conferences (
			id, tag, public_uid, active, description, og_flavor, emoji, tagline,
			date_desc, start_date, end_date, timezone, location, venue,
			venue_map_url, venue_website_url, show_hackathon, orient_cal_notif
		)
		VALUES (
			$1::uuid, 'dev26', 260001, true, 'Local Dev Conf',
			'A minimal local event seeded for development.', '++', 'local development',
			'Oct 1-2, 2026', '2026-10-01 09:00:00-05', '2026-10-02 17:00:00-05',
			'America/Chicago', 'Austin, TX', 'Localhost Hall',
			'https://maps.example.test/local-dev', 'https://example.test/local-dev',
			false, ''
		)
		ON CONFLICT (tag) DO UPDATE SET
			active = EXCLUDED.active,
			description = EXCLUDED.description,
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
			show_hackathon = EXCLUDED.show_hackathon
		RETURNING id::text
	`, devConfID).Scan(&confID)
	if err != nil {
		log.Fatal(fmt.Errorf("seed conference: %w", err))
	}

	_, err = tx.Exec(context.Background(), `
		INSERT INTO conference_days (
			id, conference_id, day_number, doors_start, doors_end,
			lunch_start, lunch_end, venues
		)
		VALUES (
			$1::uuid, $2::uuid, 1, '09:00', '17:00', '12:00', '13:00',
			ARRAY['Main Stage']
		)
		ON CONFLICT (conference_id, day_number) DO UPDATE SET
			doors_start = EXCLUDED.doors_start,
			doors_end = EXCLUDED.doors_end,
			lunch_start = EXCLUDED.lunch_start,
			lunch_end = EXCLUDED.lunch_end,
			venues = EXCLUDED.venues
	`, devDayID, confID)
	if err != nil {
		log.Fatal(fmt.Errorf("seed conference day: %w", err))
	}

	_, err = tx.Exec(context.Background(), `
		INSERT INTO conference_tickets (
			id, conference_id, ticket_key, tier, local_price, btc_price, usd_price,
			expires_start, max_count, currency, symbol, post_symbol
		)
		VALUES (
			$1::uuid, $2::uuid, 'general', 'General Admission', 25, 25, 25,
			'2027-01-01 00:00:00+00', 100, 'usd', '$', ''
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
	`, devTixID, confID)
	if err != nil {
		log.Fatal(fmt.Errorf("seed ticket: %w", err))
	}

	_, err = tx.Exec(context.Background(), `
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
	if err != nil {
		log.Fatal(fmt.Errorf("seed admin person: %w", err))
	}

	_, err = tx.Exec(context.Background(), `
		INSERT INTO people_roles (person_id, scope, position)
		VALUES ($1::uuid, 'global', 'admin')
		ON CONFLICT DO NOTHING
	`, devAdminID)
	if err != nil {
		log.Fatal(fmt.Errorf("seed admin role: %w", err))
	}

	if err := tx.Commit(context.Background()); err != nil {
		log.Fatal(err)
	}

	log.Printf("seeded local dev conference %s and admin dev-admin@example.test", confID)
}
