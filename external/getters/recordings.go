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

// RecordingPublishingUpdate mirrors final published URLs onto the Recording
// row. Workflow state (status, errors, timestamps) lives in SocialPosts.
type RecordingPublishingUpdate struct {
	YTLink     *string
	XLink      *string
	XReplyLink *string
}

func ListRecordings(ctx *config.AppContext) ([]*types.Recording, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, conf_talk_id::text, talk_name, youtube_url, x_url,
			x_reply_url, file_uri, publish_at
		FROM recordings
		ORDER BY publish_at NULLS LAST, talk_name, id
	`)
	if err != nil {
		return nil, fmt.Errorf("query recordings: %w", err)
	}
	defer rows.Close()

	var out []*types.Recording
	for rows.Next() {
		rec, err := scanRecording(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recordings: %w", err)
	}
	return out, nil
}

func ListRecordingsForConf(ctx *config.AppContext, confTag string) ([]*types.Recording, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	confTag = strings.TrimSpace(confTag)
	if confTag == "" {
		return nil, fmt.Errorf("conference tag is required")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT r.id::text, r.conf_talk_id::text, r.talk_name, r.youtube_url, r.x_url,
			r.x_reply_url, r.file_uri, r.publish_at
		FROM recordings r
		JOIN conf_talks ct ON ct.id = r.conf_talk_id
		JOIN conferences c ON c.id = ct.conference_id
		WHERE c.tag = $1
		ORDER BY r.publish_at NULLS LAST, r.talk_name, r.id
	`, confTag)
	if err != nil {
		return nil, fmt.Errorf("query recordings for %s: %w", confTag, err)
	}
	defer rows.Close()

	var out []*types.Recording
	for rows.Next() {
		rec, err := scanRecording(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recordings for %s: %w", confTag, err)
	}
	return out, nil
}

// ListRecordingsForConfTalks loads recording metadata for a set of talks in
// one round trip. Page renderers use this instead of issuing one lookup per
// talk, which is especially important when the database is remote.
func ListRecordingsForConfTalks(ctx *config.AppContext, confTalkIDs []string) ([]*types.Recording, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if len(confTalkIDs) == 0 {
		return nil, nil
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT id::text, conf_talk_id::text, talk_name, youtube_url, x_url,
			x_reply_url, file_uri, publish_at
		FROM recordings
		WHERE conf_talk_id::text = ANY($1::text[])
	`, confTalkIDs)
	if err != nil {
		return nil, fmt.Errorf("query recordings for conference talks: %w", err)
	}
	defer rows.Close()

	var out []*types.Recording
	for rows.Next() {
		rec, err := scanRecording(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recordings for conference talks: %w", err)
	}
	return out, nil
}

func GetRecordingByConfTalk(ctx *config.AppContext, confTalkID string) (*types.Recording, error) {
	return queryRecordingPostgres(ctx, "recording by conf talk", "WHERE conf_talk_id = $1::uuid", confTalkID)
}

func GetRecordingByID(ctx *config.AppContext, recordingID string) (*types.Recording, error) {
	return queryRecordingPostgres(ctx, "recording by id", "WHERE id = $1::uuid", recordingID)
}

func queryRecordingPostgres(ctx *config.AppContext, label string, whereSQL string, arg string) (*types.Recording, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	row := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT id::text, conf_talk_id::text, talk_name, youtube_url, x_url,
			x_reply_url, file_uri, publish_at
		FROM recordings
		`+whereSQL+`
	`, arg)
	rec, err := scanRecording(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return rec, nil
}

type recordingScanner interface {
	Scan(dest ...any) error
}

func scanRecording(row recordingScanner) (*types.Recording, error) {
	var rec types.Recording
	var publishAt pgtype.Timestamptz
	err := row.Scan(
		&rec.ID,
		&rec.ConfTalkID,
		&rec.TalkName,
		&rec.YTLink,
		&rec.XLink,
		&rec.XReplyLink,
		&rec.FileURI,
		&publishAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan recording: %w", err)
	}
	if publishAt.Valid {
		rec.PublishAt = &publishAt.Time
	}
	return &rec, nil
}

func UpdateRecordingYTLink(ctx *config.AppContext, recordingID, ytLink string) error {
	if err := updateRecordingColumnPostgres(ctx, recordingID, "youtube_url", ytLink); err != nil {
		return err
	}
	return nil
}

func UpdateRecordingXLink(ctx *config.AppContext, recordingID, xLink string) error {
	if err := updateRecordingColumnPostgres(ctx, recordingID, "x_url", xLink); err != nil {
		return err
	}
	return nil
}

func UpdateRecordingFileURI(ctx *config.AppContext, recordingID, fileURI string) error {
	if strings.TrimSpace(fileURI) == "" {
		return fmt.Errorf("FileURI is required")
	}
	if err := updateRecordingColumnPostgres(ctx, recordingID, "file_uri", fileURI); err != nil {
		return err
	}
	return nil
}

func UpdateRecordingPublishAt(ctx *config.AppContext, recordingID string, publishAt *time.Time) error {
	if err := updateRecordingColumnPostgres(ctx, recordingID, "publish_at", publishAt); err != nil {
		return err
	}
	return nil
}

func UpdateRecordingPublishing(ctx *config.AppContext, recordingID string, up RecordingPublishingUpdate) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	setParts := []string{}
	args := []any{recordingID}
	if up.YTLink != nil {
		args = append(args, *up.YTLink)
		setParts = append(setParts, fmt.Sprintf("youtube_url = $%d", len(args)))
	}
	if up.XLink != nil {
		args = append(args, *up.XLink)
		setParts = append(setParts, fmt.Sprintf("x_url = $%d", len(args)))
	}
	if up.XReplyLink != nil {
		args = append(args, *up.XReplyLink)
		setParts = append(setParts, fmt.Sprintf("x_reply_url = $%d", len(args)))
	}
	if len(setParts) == 0 {
		return nil
	}
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE recordings
		SET `+strings.Join(setParts, ", ")+`
		WHERE id = $1::uuid
	`, args...)
	if err != nil {
		return fmt.Errorf("update recording publishing fields: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("recording %s not found", recordingID)
	}
	return nil
}

func updateRecordingColumnPostgres(ctx *config.AppContext, recordingID string, column string, value any) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if !validRecordingColumn(column) {
		return fmt.Errorf("invalid recording column %q", column)
	}
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE recordings
		SET `+pgx.Identifier{column}.Sanitize()+` = $2
		WHERE id = $1::uuid
	`, recordingID, value)
	if err != nil {
		return fmt.Errorf("update recording %s: %w", column, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("recording %s not found", recordingID)
	}
	return nil
}

func validRecordingColumn(column string) bool {
	switch column {
	case "youtube_url", "x_url", "file_uri", "publish_at":
		return true
	default:
		return false
	}
}
