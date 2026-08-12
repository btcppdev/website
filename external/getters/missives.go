package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/mtypes"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"strings"
	"time"
)

type MissiveInput struct {
	Title        string
	Markdown     string
	SendAt       string
	Newsletters  []string
	OnlyFor      string
	Expiry       *time.Time
	DedupeKey    string
	ConferenceID string
}

type AdminSubscriberSummary struct {
	TotalStored      int
	ActiveAny        int
	NewsletterActive int
	Inactive         int
}

type AdminSubscriberListCount struct {
	Name  string
	Count int
}

type AdminSubscriberRow struct {
	Email         string
	Subscriptions []string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type AdminSubscriberResult struct {
	Summary       AdminSubscriberSummary
	ListCounts    []AdminSubscriberListCount
	Subscribers   []AdminSubscriberRow
	TotalFiltered int
}

// ListAdminSubscribers returns aggregate counts plus one filtered page for the
// global-admin subscriber browser. Subscriber rows are retained after their
// final unsubscribe, so TotalStored and Inactive intentionally remain distinct
// from active audience counts.
func ListAdminSubscribers(ctx *config.AppContext, search, list, status string, limit, offset int) (*AdminSubscriberResult, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	search = strings.TrimSpace(search)
	list = strings.TrimSpace(list)
	status = strings.TrimSpace(status)
	if status != "active" && status != "inactive" {
		status = "all"
	}
	if limit < 1 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	result := &AdminSubscriberResult{}
	summary, err := GetAdminSubscriberSummary(ctx)
	if err != nil {
		return nil, err
	}
	result.Summary = summary

	listRows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT name, COUNT(*)::int
		FROM subscriber_subscriptions
		GROUP BY name
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("query subscriber list counts: %w", err)
	}
	defer listRows.Close()
	for listRows.Next() {
		var item AdminSubscriberListCount
		if err := listRows.Scan(&item.Name, &item.Count); err != nil {
			return nil, fmt.Errorf("scan subscriber list count: %w", err)
		}
		result.ListCounts = append(result.ListCounts, item)
	}
	if err := listRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscriber list counts: %w", err)
	}

	const filteredWhere = `
		FROM subscribers s
		WHERE ($1 = '' OR s.email ILIKE '%' || $1 || '%')
			AND ($2 = '' OR EXISTS (
				SELECT 1 FROM subscriber_subscriptions selected
				WHERE selected.subscriber_id = s.id AND selected.name = $2
			))
			AND (
				$3 = 'all'
				OR ($3 = 'active' AND EXISTS (
					SELECT 1 FROM subscriber_subscriptions active WHERE active.subscriber_id = s.id
				))
				OR ($3 = 'inactive' AND NOT EXISTS (
					SELECT 1 FROM subscriber_subscriptions inactive WHERE inactive.subscriber_id = s.id
				))
			)
	`
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `SELECT COUNT(*)::int `+filteredWhere, search, list, status).Scan(&result.TotalFiltered); err != nil {
		return nil, fmt.Errorf("count filtered subscribers: %w", err)
	}

	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT s.email, s.created_at, s.updated_at,
			ARRAY(
				SELECT membership.name
				FROM subscriber_subscriptions membership
				WHERE membership.subscriber_id = s.id
				ORDER BY membership.name
			)
		`+filteredWhere+`
		ORDER BY s.created_at DESC, s.email
		LIMIT $4 OFFSET $5
	`, search, list, status, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query admin subscribers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var subscriber AdminSubscriberRow
		if err := rows.Scan(&subscriber.Email, &subscriber.CreatedAt, &subscriber.UpdatedAt, &subscriber.Subscriptions); err != nil {
			return nil, fmt.Errorf("scan admin subscriber: %w", err)
		}
		result.Subscribers = append(result.Subscribers, subscriber)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin subscribers: %w", err)
	}
	return result, nil
}

func GetAdminSubscriberSummary(ctx *config.AppContext) (AdminSubscriberSummary, error) {
	if ctx == nil || ctx.DB == nil {
		return AdminSubscriberSummary{}, fmt.Errorf("database is not configured")
	}
	var summary AdminSubscriberSummary
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE EXISTS (
				SELECT 1 FROM subscriber_subscriptions ss WHERE ss.subscriber_id = s.id
			))::int,
			COUNT(*) FILTER (WHERE EXISTS (
				SELECT 1 FROM subscriber_subscriptions ss
				WHERE ss.subscriber_id = s.id AND ss.name = 'newsletter'
			))::int,
			COUNT(*) FILTER (WHERE NOT EXISTS (
				SELECT 1 FROM subscriber_subscriptions ss WHERE ss.subscriber_id = s.id
			))::int
		FROM subscribers s
	`).Scan(
		&summary.TotalStored,
		&summary.ActiveAny,
		&summary.NewsletterActive,
		&summary.Inactive,
	); err != nil {
		return AdminSubscriberSummary{}, fmt.Errorf("query subscriber summary: %w", err)
	}
	return summary, nil
}

func FindSubscriber(ctx *config.AppContext, email string) (*mtypes.Subscriber, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, nil
	}

	var subscriberID string
	var storedEmail string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text, email
		FROM subscribers
		WHERE email = $1
	`, email).Scan(&subscriberID, &storedEmail)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query subscriber %q: %w", email, err)
	}

	subs, err := subscriberSubscriptionsPostgres(ctx, subscriberID)
	if err != nil {
		return nil, err
	}
	return &mtypes.Subscriber{
		Email: storedEmail,
		Subs:  subs,
		Pages: []string{subscriberID},
	}, nil
}

func ListSubscribersFor(ctx *config.AppContext, newsletters []string) ([]*mtypes.Subscriber, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	include, exclude := splitNewsletterFilters(newsletters)
	if len(include) == 0 {
		return nil, fmt.Errorf("Must have at least 1 !!newsletter %v", newsletters)
	}

	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT s.id::text, s.email
		FROM subscribers s
		WHERE EXISTS (
			SELECT 1
			FROM subscriber_subscriptions ss
			WHERE ss.subscriber_id = s.id
				AND ss.name = ANY($1::text[])
		)
		AND NOT EXISTS (
			SELECT 1
			FROM subscriber_subscriptions ss
			WHERE ss.subscriber_id = s.id
				AND ss.name = ANY($2::text[])
		)
		ORDER BY s.email
	`, include, exclude)
	if err != nil {
		return nil, fmt.Errorf("query subscribers: %w", err)
	}
	defer rows.Close()

	return scanSubscribersPostgres(ctx, rows)
}

func IsSubscribedTo(ctx *config.AppContext, email, newsletter string) (bool, error) {
	if ctx == nil || ctx.DB == nil {
		return false, fmt.Errorf("database is not configured")
	}
	if strings.TrimSpace(email) == "" || strings.TrimSpace(newsletter) == "" {
		return false, nil
	}

	var exists bool
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT EXISTS (
			SELECT 1
			FROM subscribers s
			JOIN subscriber_subscriptions ss ON ss.subscriber_id = s.id
			WHERE s.email = $1
				AND ss.name = $2
		)
	`, strings.TrimSpace(email), strings.TrimSpace(newsletter)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("query subscription %q/%q: %w", email, newsletter, err)
	}
	return exists, nil
}

func ListSubscribers(ctx *config.AppContext, newsletter string) ([]*mtypes.Subscriber, error) {
	return ListSubscribersFor(ctx, []string{newsletter})
}

func NewSubscriber(ctx *config.AppContext, email, newsletter string) (*mtypes.Subscriber, error) {
	return NewSubscriberList(ctx, email, []string{newsletter})
}

func NewSubscriberList(ctx *config.AppContext, email string, newsletters []string) (*mtypes.Subscriber, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, fmt.Errorf("subscriber email is empty")
	}

	var subscriberID string
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO subscribers (email)
		VALUES ($1)
		ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
		RETURNING id::text
	`, email).Scan(&subscriberID); err != nil {
		return nil, fmt.Errorf("insert subscriber %q: %w", email, err)
	}

	sub := &mtypes.Subscriber{Email: email, Pages: []string{subscriberID}}
	sub.AddSublist(newsletters)
	if err := UpdateSubs(ctx, sub); err != nil {
		return nil, err
	}
	return FindSubscriber(ctx, email)
}

func SubscribeEmailList(ctx *config.AppContext, email string, newsletters []string) (*mtypes.Subscriber, error) {
	subscriber, err := FindSubscriber(ctx, email)
	if err != nil {
		return nil, err
	}
	if subscriber == nil {
		return NewSubscriberList(ctx, email, newsletters)
	}
	for _, nl := range newsletters {
		subscriber.AddSubscription(nl)
	}
	if err := UpdateSubs(ctx, subscriber); err != nil {
		return nil, err
	}
	return FindSubscriber(ctx, email)
}

func SubscribeEmail(ctx *config.AppContext, email, newsletter string) (*mtypes.Subscriber, error) {
	return SubscribeEmailList(ctx, email, []string{newsletter})
}

func UpdateSubs(ctx *config.AppContext, sub *mtypes.Subscriber) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if sub == nil {
		return fmt.Errorf("subscriber is nil")
	}

	subscriberID, err := subscriberIDPostgres(ctx, sub)
	if err != nil {
		return err
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return fmt.Errorf("begin subscriber update: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())

	if _, err := tx.Exec(ctx.DatabaseContext(), `DELETE FROM subscriber_subscriptions WHERE subscriber_id = $1`, subscriberID); err != nil {
		return fmt.Errorf("clear subscriber subscriptions %q: %w", sub.Email, err)
	}
	for _, subscription := range sub.Subs {
		if subscription == nil || strings.TrimSpace(subscription.Name) == "" {
			continue
		}
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			INSERT INTO subscriber_subscriptions (subscriber_id, name)
			VALUES ($1, $2)
			ON CONFLICT (subscriber_id, name) DO NOTHING
		`, subscriberID, strings.TrimSpace(subscription.Name)); err != nil {
			return fmt.Errorf("insert subscriber subscription %q/%q: %w", sub.Email, subscription.Name, err)
		}
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return fmt.Errorf("commit subscriber update: %w", err)
	}
	return nil
}

func GetLetter(ctx *config.AppContext, uniqueID uint64) (*mtypes.Letter, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	row := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text, public_uid, title, newsletters, only_for, markdown,
			send_at_expr, sent_at, expiry
		FROM missives
		WHERE public_uid = $1
	`, uniqueID)
	letter, err := scanLetterPostgres(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("Couldn't find missive with UID#%d", uniqueID)
		}
		return nil, fmt.Errorf("query missive %d: %w", uniqueID, err)
	}
	return letter, nil
}

func GetLetterFor(ctx *config.AppContext, onlyfor string) (*mtypes.Letter, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	row := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text, public_uid, title, newsletters, only_for, markdown,
			send_at_expr, sent_at, expiry
		FROM missives
		WHERE only_for = $1
		ORDER BY public_uid DESC NULLS LAST, created_at DESC
		LIMIT 1
	`, onlyfor)
	letter, err := scanLetterPostgres(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("Couldn't find missive OnlyFor %s", onlyfor)
		}
		return nil, fmt.Errorf("query missive only_for %q: %w", onlyfor, err)
	}
	return letter, nil
}

func GetLetters(ctx *config.AppContext, newsletter string) ([]*mtypes.Letter, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	query := `
		SELECT id::text, public_uid, title, newsletters, only_for, markdown,
			send_at_expr, sent_at, expiry
		FROM missives m
		WHERE ($1 = 'all' OR $1 = ANY(newsletters))
			AND NOT EXISTS (
				SELECT 1 FROM conference_email_occurrences occurrence
				WHERE occurrence.missive_id = m.id
			)
		ORDER BY public_uid NULLS LAST, created_at
	`
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), query, newsletter)
	if err != nil {
		return nil, fmt.Errorf("query missives: %w", err)
	}
	defer rows.Close()

	var letters []*mtypes.Letter
	for rows.Next() {
		letter, err := scanLetterPostgres(rows)
		if err != nil {
			return nil, err
		}
		if newsletter != "all" && !letter.HasNewsletter(newsletter) {
			continue
		}
		letters = append(letters, letter)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate missives: %w", err)
	}
	return letters, nil
}

func ListOnlyForLetters(ctx *config.AppContext) ([]*mtypes.Letter, error) {
	return listLettersByOnlyForPostgres(ctx, `only_for <> ''`)
}

func ListTemplatedLetters(ctx *config.AppContext) ([]*mtypes.Letter, error) {
	return listLettersByOnlyForPostgres(ctx, `only_for = '`+mtypes.OnlyForTemplated+`'`)
}

// ListAdminEditableLetters returns newsletter-builder missives plus the active
// version of every reusable one-shot template. GetLetterFor resolves the
// highest-UID row for an only_for key, so older shadowed versions must not be
// offered for editing as if they still controlled outgoing mail.
func ListAdminEditableLetters(ctx *config.AppContext) ([]*mtypes.Letter, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, public_uid, title, newsletters, only_for, markdown,
			send_at_expr, sent_at, expiry
		FROM missives m
		WHERE (m.only_for = $1 AND m.conference_id IS NULL)
			OR m.id IN (
				SELECT DISTINCT ON (only_for) id
				FROM missives
				WHERE only_for <> '' AND only_for <> $1
				ORDER BY only_for, public_uid DESC NULLS LAST, created_at DESC
			)
		ORDER BY public_uid DESC NULLS LAST, created_at DESC
	`, mtypes.OnlyForTemplated)
	if err != nil {
		return nil, fmt.Errorf("query admin editable missives: %w", err)
	}
	defer rows.Close()
	var letters []*mtypes.Letter
	for rows.Next() {
		letter, err := scanLetterPostgres(rows)
		if err != nil {
			return nil, err
		}
		letters = append(letters, letter)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin editable missives: %w", err)
	}
	return letters, nil
}

func CreateTemplatedMissive(ctx *config.AppContext, in MissiveInput) (*mtypes.Letter, error) {
	in.OnlyFor = mtypes.OnlyForTemplated
	return insertMissivePostgres(ctx, in)
}

// CreateWeeklyNewsletterMissive saves the generated issue and its selected
// Talk of the Week atomically. Keeping the relation separate from the rendered
// Markdown gives future issues reliable no-repeat and diversity signals while
// leaving the draft copy fully editable.
func CreateWeeklyNewsletterMissive(ctx *config.AppContext, in MissiveInput, featuredTalkID string) (*mtypes.Letter, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	in.OnlyFor = mtypes.OnlyForTemplated
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return nil, fmt.Errorf("begin weekly newsletter missive: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())

	row := tx.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO missives (public_uid, title, markdown, send_at_expr, newsletters, only_for, expiry, dedupe_key, conference_id)
		VALUES ((SELECT COALESCE(max(public_uid), 0) + 1 FROM missives), $1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, '')::uuid)
		RETURNING id::text, public_uid, title, newsletters, only_for, markdown,
			send_at_expr, sent_at, expiry
	`, in.Title, in.Markdown, in.SendAt, in.Newsletters, in.OnlyFor, in.Expiry, strings.TrimSpace(in.DedupeKey), strings.TrimSpace(in.ConferenceID))
	letter, err := scanLetterPostgres(row)
	if err != nil {
		return nil, fmt.Errorf("insert weekly newsletter missive %q: %w", in.Title, err)
	}
	if featuredTalkID = strings.TrimSpace(featuredTalkID); featuredTalkID != "" {
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			INSERT INTO weekly_newsletter_featured_talks (missive_id, conf_talk_id)
			VALUES ($1::uuid, $2::uuid)
		`, letter.PageID, featuredTalkID); err != nil {
			return nil, fmt.Errorf("record weekly newsletter featured talk: %w", err)
		}
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return nil, fmt.Errorf("commit weekly newsletter missive: %w", err)
	}
	return letter, nil
}

func GetTemplatedLetterByDedupeKey(ctx *config.AppContext, dedupeKey string) (*mtypes.Letter, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	dedupeKey = strings.TrimSpace(dedupeKey)
	if dedupeKey == "" {
		return nil, nil
	}
	row := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text, public_uid, title, newsletters, only_for, markdown,
			send_at_expr, sent_at, expiry
		FROM missives
		WHERE only_for = $1 AND dedupe_key = $2
	`, mtypes.OnlyForTemplated, dedupeKey)
	letter, err := scanLetterPostgres(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query templated missive dedupe key %q: %w", dedupeKey, err)
	}
	return letter, nil
}

func UpdateTemplatedMissive(ctx *config.AppContext, pageID string, in MissiveInput) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	in.OnlyFor = mtypes.OnlyForTemplated
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE missives
		SET title = $2,
			markdown = $3,
			send_at_expr = $4,
			newsletters = $5,
			only_for = $6,
			expiry = $7
		WHERE id = $1
	`, pageID, in.Title, in.Markdown, in.SendAt, in.Newsletters, in.OnlyFor, in.Expiry)
	if err != nil {
		return fmt.Errorf("update templated missive %q: %w", pageID, err)
	}
	return nil
}

func UpdateOnlyForMissive(ctx *config.AppContext, pageID, title, markdown string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	result, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE missives
		SET title = $2, markdown = $3
		WHERE id = $1::uuid
			AND only_for <> ''
			AND only_for <> $4
	`, pageID, strings.TrimSpace(title), markdown, mtypes.OnlyForTemplated)
	if err != nil {
		return fmt.Errorf("update reusable missive %q: %w", pageID, err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("reusable missive %q not found", pageID)
	}
	return nil
}

// DeleteTemplatedDraft deletes an unsent templated missive by public UID. The
// sent_at and only_for predicates are enforced in the delete itself so this
// remains safe if the editor is stale or the missive was sent concurrently.
func DeleteTemplatedDraft(ctx *config.AppContext, uid uint64) (bool, error) {
	if ctx == nil || ctx.DB == nil {
		return false, fmt.Errorf("database is not configured")
	}
	result, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		DELETE FROM missives
		WHERE public_uid = $1
			AND only_for = $2
			AND sent_at IS NULL
	`, uid, mtypes.OnlyForTemplated)
	if err != nil {
		return false, fmt.Errorf("delete templated draft %d: %w", uid, err)
	}
	return result.RowsAffected() == 1, nil
}

func CreateMissive(ctx *config.AppContext, title, markdown, sendAt string, newsletters []string) error {
	_, err := insertMissivePostgres(ctx, MissiveInput{
		Title:       title,
		Markdown:    markdown,
		SendAt:      sendAt,
		Newsletters: newsletters,
	})
	return err
}

func MarkLetterSent(ctx *config.AppContext, letter *mtypes.Letter, sentAt time.Time) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if letter == nil {
		return fmt.Errorf("letter is nil")
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE missives
		SET sent_at = $2
		WHERE id = $1
	`, letter.PageID, sentAt)
	if err != nil {
		return fmt.Errorf("mark missive sent %q: %w", letter.PageID, err)
	}
	return nil
}

type letterScanner interface {
	Scan(dest ...any) error
}

func scanLetterPostgres(row letterScanner) (*mtypes.Letter, error) {
	var letter mtypes.Letter
	var publicUID *int64
	var sentAt pgtype.Timestamptz
	var expiry pgtype.Timestamptz
	err := row.Scan(
		&letter.PageID,
		&publicUID,
		&letter.Title,
		&letter.Newsletters,
		&letter.OnlyFor,
		&letter.Markdown,
		&letter.SendAt,
		&sentAt,
		&expiry,
	)
	if err != nil {
		return nil, err
	}
	if publicUID != nil {
		letter.UID = uint64(*publicUID)
	}
	if sentAt.Valid {
		letter.SentAt = &sentAt.Time
	}
	if expiry.Valid {
		letter.Expiry = &expiry.Time
	}
	return &letter, nil
}

func subscriberSubscriptionsPostgres(ctx *config.AppContext, subscriberID string) ([]*mtypes.Subscription, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT name
		FROM subscriber_subscriptions
		WHERE subscriber_id = $1
		ORDER BY name
	`, subscriberID)
	if err != nil {
		return nil, fmt.Errorf("query subscriber subscriptions %q: %w", subscriberID, err)
	}
	defer rows.Close()

	var subs []*mtypes.Subscription
	for rows.Next() {
		var sub mtypes.Subscription
		if err := rows.Scan(&sub.Name); err != nil {
			return nil, fmt.Errorf("scan subscriber subscription: %w", err)
		}
		subs = append(subs, &sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscriber subscriptions: %w", err)
	}
	return subs, nil
}

func subscriberIDPostgres(ctx *config.AppContext, sub *mtypes.Subscriber) (string, error) {
	if len(sub.Pages) > 0 && strings.TrimSpace(sub.Pages[0]) != "" {
		return strings.TrimSpace(sub.Pages[0]), nil
	}
	if strings.TrimSpace(sub.Email) == "" {
		return "", fmt.Errorf("subscriber email is empty")
	}
	var subscriberID string
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO subscribers (email)
		VALUES ($1)
		ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
		RETURNING id::text
	`, strings.TrimSpace(sub.Email)).Scan(&subscriberID); err != nil {
		return "", fmt.Errorf("upsert subscriber %q: %w", sub.Email, err)
	}
	sub.Pages = []string{subscriberID}
	return subscriberID, nil
}

func splitNewsletterFilters(newsletters []string) ([]string, []string) {
	include := make([]string, 0, len(newsletters))
	exclude := make([]string, 0, len(newsletters))
	for _, newsletter := range newsletters {
		newsletter = strings.TrimSpace(newsletter)
		if newsletter == "" {
			continue
		}
		if strings.HasPrefix(newsletter, "!") {
			exclude = append(exclude, strings.TrimPrefix(newsletter, "!"))
			continue
		}
		include = append(include, newsletter)
	}
	return include, exclude
}

func scanSubscribersPostgres(ctx *config.AppContext, rows pgx.Rows) ([]*mtypes.Subscriber, error) {
	var subscribers []*mtypes.Subscriber
	for rows.Next() {
		var subscriberID string
		var email string
		if err := rows.Scan(&subscriberID, &email); err != nil {
			return nil, fmt.Errorf("scan subscriber: %w", err)
		}
		subs, err := subscriberSubscriptionsPostgres(ctx, subscriberID)
		if err != nil {
			return nil, err
		}
		subscribers = append(subscribers, &mtypes.Subscriber{
			Email: email,
			Subs:  subs,
			Pages: []string{subscriberID},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscribers: %w", err)
	}
	return subscribers, nil
}

func listLettersByOnlyForPostgres(ctx *config.AppContext, condition string) ([]*mtypes.Letter, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, public_uid, title, newsletters, only_for, markdown,
			send_at_expr, sent_at, expiry
		FROM missives
		WHERE `+condition+`
		ORDER BY public_uid NULLS LAST, created_at
	`)
	if err != nil {
		return nil, fmt.Errorf("query only_for missives: %w", err)
	}
	defer rows.Close()

	var letters []*mtypes.Letter
	for rows.Next() {
		letter, err := scanLetterPostgres(rows)
		if err != nil {
			return nil, err
		}
		letters = append(letters, letter)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate only_for missives: %w", err)
	}
	return letters, nil
}

func insertMissivePostgres(ctx *config.AppContext, in MissiveInput) (*mtypes.Letter, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	row := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO missives (public_uid, title, markdown, send_at_expr, newsletters, only_for, expiry, dedupe_key, conference_id)
		VALUES ((SELECT COALESCE(max(public_uid), 0) + 1 FROM missives), $1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, '')::uuid)
		RETURNING id::text, public_uid, title, newsletters, only_for, markdown,
			send_at_expr, sent_at, expiry
	`, in.Title, in.Markdown, in.SendAt, in.Newsletters, in.OnlyFor, in.Expiry, strings.TrimSpace(in.DedupeKey), strings.TrimSpace(in.ConferenceID))
	letter, err := scanLetterPostgres(row)
	if err != nil {
		return nil, fmt.Errorf("insert missive %q: %w", in.Title, err)
	}
	return letter, nil
}
