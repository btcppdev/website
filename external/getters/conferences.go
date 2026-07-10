package getters

import (
	"context"
	"fmt"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5/pgtype"
)

type ConfDetailsInput struct {
	Description     string
	EditionType     string
	OGFlavor        string
	Emoji           string
	Tagline         string
	DateDesc        string
	StartDate       *time.Time
	EndDate         *time.Time
	Timezone        string
	Location        string
	Venue           string
	VenueMap        string
	VenueWebsite    string
	ShowHackathon   bool
	HeroTitle       string
	HeroCaption     string
	AboutTitle      string
	AboutBody       string
	AboutBody2      string
	VenueTitle      string
	VenueSubtitle   string
	VenueBody       string
	HotelsIntro     string
	LocalTicketBody string
	SpeakersTitle   string
	SpeakersBody    string
	MapEmbedURL     string
	MapLatitude     float64
	MapLongitude    float64
	MapXPercent     float64
	MapYPercent     float64
	MapLabel        string
	MapLabelSide    string
}

func ListConfs(ctx *config.AppContext) ([]*types.Conf, error) {
	confs, err := queryConferencesOnlyPostgres(ctx, "conferences", "", nil)
	if err != nil {
		return nil, err
	}

	tickets, err := listConfTicketsPostgres(ctx)
	if err != nil {
		return nil, err
	}

	confByID := make(map[string]*types.Conf, len(confs))
	for _, conf := range confs {
		if conf != nil {
			confByID[conf.Ref] = conf
		}
	}
	for _, ticket := range tickets {
		if ticket == nil {
			continue
		}
		if conf := confByID[ticket.ConfRef]; conf != nil {
			conf.Tickets = append(conf.Tickets, ticket)
		}
	}
	return confs, nil
}

func GetConfByTag(ctx *config.AppContext, tag string) (*types.Conf, error) {
	confs, err := queryConferencesOnlyPostgres(ctx, "conference by tag", "WHERE tag = $1", []any{tag})
	if err != nil || len(confs) == 0 {
		return nil, err
	}
	if err := hydrateConferenceTicketsPostgres(ctx, confs); err != nil {
		return nil, err
	}
	return confs[0], nil
}

func GetConfByRef(ctx *config.AppContext, ref string) (*types.Conf, error) {
	confs, err := queryConferencesOnlyPostgres(ctx, "conference by ref", "WHERE id::text = $1", []any{ref})
	if err != nil || len(confs) == 0 {
		return nil, err
	}
	if err := hydrateConferenceTicketsPostgres(ctx, confs); err != nil {
		return nil, err
	}
	return confs[0], nil
}

func hydrateConferenceTicketsPostgres(ctx *config.AppContext, confs []*types.Conf) error {
	refs := make([]string, 0, len(confs))
	for _, conf := range confs {
		if conf != nil {
			refs = append(refs, conf.Ref)
		}
	}
	if len(refs) == 0 {
		return nil
	}
	tickets, err := queryConfTicketsPostgres(ctx, "conference tickets for conferences", "WHERE conference_id::text = ANY($1::text[])", []any{refs})
	if err != nil {
		return err
	}
	confByID := make(map[string]*types.Conf, len(confs))
	for _, conf := range confs {
		if conf != nil {
			confByID[conf.Ref] = conf
		}
	}
	for _, ticket := range tickets {
		if ticket == nil {
			continue
		}
		if conf := confByID[ticket.ConfRef]; conf != nil {
			conf.Tickets = append(conf.Tickets, ticket)
		}
	}
	return nil
}

func listConferencesOnlyPostgres(ctx *config.AppContext) ([]*types.Conf, error) {
	return queryConferencesOnlyPostgres(ctx, "conferences", "", nil)
}

func queryConferencesOnlyPostgres(ctx *config.AppContext, label string, whereSQL string, args []any) ([]*types.Conf, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	editionTypeSelect := "'global' AS edition_type"
	if postgresColumnExists(ctx, "conferences", "edition_type") {
		editionTypeSelect = "edition_type"
	}
	publicationStatusSelect := "CASE WHEN active THEN 'published' ELSE 'draft' END AS publication_status"
	hasPublicationStatus := postgresColumnExists(ctx, "conferences", "publication_status")
	if hasPublicationStatus {
		publicationStatusSelect = "publication_status"
	}
	rows, err := ctx.DB.Query(context.Background(), `
		SELECT id::text, tag, public_uid, active, `+publicationStatusSelect+`, description, `+editionTypeSelect+`, og_flavor, emoji,
			tagline, date_desc, start_date, end_date, timezone, location, venue,
			venue_map_url, venue_website_url, show_hackathon, orient_cal_notif,
			hero_title, hero_caption, about_title, about_body, about_body_2,
			venue_title, venue_subtitle, venue_body, hotels_intro, local_ticket_body,
			speakers_title, speakers_body, map_embed_url,
			map_latitude, map_longitude, map_x_percent, map_y_percent, map_label, map_label_side
		FROM conferences
		`+whereSQL+`
		ORDER BY start_date NULLS LAST, tag
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", label, err)
	}
	defer rows.Close()

	var confs []*types.Conf
	for rows.Next() {
		var conf types.Conf
		var publicUID *int64
		var startDate pgtype.Timestamptz
		var endDate pgtype.Timestamptz
		err := rows.Scan(
			&conf.Ref,
			&conf.Tag,
			&publicUID,
			&conf.Active,
			&conf.PublicationStatus,
			&conf.Desc,
			&conf.EditionType,
			&conf.OGFlavor,
			&conf.Emoji,
			&conf.Tagline,
			&conf.DateDesc,
			&startDate,
			&endDate,
			&conf.Timezone,
			&conf.Location,
			&conf.Venue,
			&conf.VenueMap,
			&conf.VenueWebsite,
			&conf.ShowHackathon,
			&conf.OrientCalNotif,
			&conf.HeroTitle,
			&conf.HeroCaption,
			&conf.AboutTitle,
			&conf.AboutBody,
			&conf.AboutBody2,
			&conf.VenueTitle,
			&conf.VenueSubtitle,
			&conf.VenueBody,
			&conf.HotelsIntro,
			&conf.LocalTicketBody,
			&conf.SpeakersTitle,
			&conf.SpeakersBody,
			&conf.MapEmbedURL,
			&conf.MapLatitude,
			&conf.MapLongitude,
			&conf.MapXPercent,
			&conf.MapYPercent,
			&conf.MapLabel,
			&conf.MapLabelSide,
		)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", label, err)
		}
		if publicUID != nil {
			conf.UID = uint64(*publicUID)
		}
		if conf.Timezone != "" {
			if loc, err := time.LoadLocation(conf.Timezone); err == nil {
				conf.TZ = loc
			}
		}
		if startDate.Valid {
			conf.StartDate = startDate.Time.In(conf.Loc())
		}
		if endDate.Valid {
			conf.EndDate = endDate.Time.In(conf.Loc())
		}
		if conf.PublicationStatus != "published" {
			conf.PublicationStatus = "draft"
		}
		if hasPublicationStatus {
			conf.Active = conf.IsCurrentlyActive()
		}
		confs = append(confs, &conf)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", label, err)
	}
	return confs, nil
}

func postgresColumnExists(ctx *config.AppContext, tableName, columnName string) bool {
	if ctx == nil || ctx.DB == nil {
		return false
	}
	var exists bool
	err := ctx.DB.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = $1
			  AND column_name = $2
		)
	`, tableName, columnName).Scan(&exists)
	return err == nil && exists
}

func listConfTicketsPostgres(ctx *config.AppContext) ([]*types.ConfTicket, error) {
	return queryConfTicketsPostgres(ctx, "conference tickets", "", nil)
}

func queryConfTicketsPostgres(ctx *config.AppContext, label string, whereSQL string, args []any) ([]*types.ConfTicket, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(context.Background(), `
		SELECT id::text, conference_id::text, tier, local_price, btc_price, usd_price,
			expires_start, expires_end, max_count, currency, symbol, post_symbol
		FROM conference_tickets
		`+whereSQL+`
		ORDER BY expires_start NULLS LAST, tier
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", label, err)
	}
	defer rows.Close()

	var tickets []*types.ConfTicket
	for rows.Next() {
		var ticket types.ConfTicket
		var expiresStart pgtype.Timestamptz
		var expiresEnd pgtype.Timestamptz
		var localPrice, btcPrice, usdPrice, maxCount int64
		err := rows.Scan(
			&ticket.ID,
			&ticket.ConfRef,
			&ticket.Tier,
			&localPrice,
			&btcPrice,
			&usdPrice,
			&expiresStart,
			&expiresEnd,
			&maxCount,
			&ticket.Currency,
			&ticket.Symbol,
			&ticket.PostSymbol,
		)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", label, err)
		}
		ticket.Local = uint(localPrice)
		ticket.BTC = uint(btcPrice)
		ticket.USD = uint(usdPrice)
		ticket.Max = uint(maxCount)
		ticket.Expires = &types.Times{}
		if expiresStart.Valid {
			ticket.Expires.Start = expiresStart.Time
		}
		if expiresEnd.Valid {
			ticket.Expires.End = &expiresEnd.Time
		}
		tickets = append(tickets, &ticket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", label, err)
	}
	return tickets, nil
}

func ConfUpdateOrientCalNotif(ctx *config.AppContext, confRef string, calnotif string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	commandTag, err := ctx.DB.Exec(context.Background(), `
		UPDATE conferences
		SET orient_cal_notif = $2
		WHERE id = $1
	`, confRef, calnotif)
	if err != nil {
		return fmt.Errorf("update conference %s orientation cal notif: %w", confRef, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("conference %s not found", confRef)
	}
	return nil
}

func normalizeEditionType(v string) string {
	if v == "local" {
		return "local"
	}
	return "global"
}

func UpdateConfActive(ctx *config.AppContext, confRef string, active bool) error {
	status := "draft"
	if active {
		status = "published"
	}
	return UpdateConfPublicationStatus(ctx, confRef, status)
}

func UpdateConfPublicationStatus(ctx *config.AppContext, confRef string, status string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if status != "published" {
		status = "draft"
	}
	if postgresColumnExists(ctx, "conferences", "publication_status") {
		commandTag, err := ctx.DB.Exec(context.Background(), `
			UPDATE conferences
			SET publication_status = $2,
				active = $2 = 'published' AND (end_date IS NULL OR end_date >= now())
			WHERE id = $1
		`, confRef, status)
		if err != nil {
			return fmt.Errorf("update conference %s publication status: %w", confRef, err)
		}
		if commandTag.RowsAffected() == 0 {
			return fmt.Errorf("conference %s not found", confRef)
		}
		return nil
	}
	active := status == "published"
	commandTag, err := ctx.DB.Exec(context.Background(), `
		UPDATE conferences
		SET active = $2
		WHERE id = $1
	`, confRef, active)
	if err != nil {
		return fmt.Errorf("update conference %s active: %w", confRef, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("conference %s not found", confRef)
	}
	return nil
}

func UpdateConfDetails(ctx *config.AppContext, confRef string, in ConfDetailsInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	commandTag, err := ctx.DB.Exec(context.Background(), `
		UPDATE conferences
		SET description = $2,
			og_flavor = $3,
			emoji = $4,
			tagline = $5,
			date_desc = $6,
			start_date = $7,
			end_date = $8,
			timezone = $9,
			location = $10,
			venue = $11,
			venue_map_url = $12,
			venue_website_url = $13,
			show_hackathon = $14,
			hero_title = $15,
			hero_caption = $16,
			about_title = $17,
			about_body = $18,
			about_body_2 = $19,
			venue_title = $20,
			venue_subtitle = $21,
			venue_body = $22,
			hotels_intro = $23,
			local_ticket_body = $24,
			speakers_title = $25,
			speakers_body = $26,
			map_embed_url = $27,
			map_latitude = $28,
			map_longitude = $29,
			map_x_percent = $30,
			map_y_percent = $31,
			map_label = $32,
			map_label_side = $33
		WHERE id = $1
	`, confRef, in.Description, in.OGFlavor, in.Emoji, in.Tagline, in.DateDesc,
		in.StartDate, in.EndDate, in.Timezone, in.Location, in.Venue,
		in.VenueMap, in.VenueWebsite, in.ShowHackathon, in.HeroTitle,
		in.HeroCaption, in.AboutTitle, in.AboutBody, in.AboutBody2,
		in.VenueTitle, in.VenueSubtitle, in.VenueBody, in.HotelsIntro,
		in.LocalTicketBody, in.SpeakersTitle, in.SpeakersBody, in.MapEmbedURL,
		in.MapLatitude, in.MapLongitude, in.MapXPercent, in.MapYPercent,
		in.MapLabel, in.MapLabelSide)
	if err != nil {
		return fmt.Errorf("update conference %s details: %w", confRef, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("conference %s not found", confRef)
	}
	if postgresColumnExists(ctx, "conferences", "edition_type") {
		if _, err := ctx.DB.Exec(context.Background(), `
			UPDATE conferences
			SET edition_type = $2
			WHERE id = $1
		`, confRef, normalizeEditionType(in.EditionType)); err != nil {
			return fmt.Errorf("update conference %s edition type: %w", confRef, err)
		}
	}
	if postgresColumnExists(ctx, "conferences", "publication_status") {
		if _, err := ctx.DB.Exec(context.Background(), `
			UPDATE conferences
			SET active = publication_status = 'published' AND (end_date IS NULL OR end_date >= now())
			WHERE id = $1
		`, confRef); err != nil {
			return fmt.Errorf("update conference %s active lifecycle: %w", confRef, err)
		}
	}
	return nil
}
