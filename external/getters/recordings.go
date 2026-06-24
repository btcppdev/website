package getters

import (
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

// RecordingPublishingUpdate mirrors final published URLs onto the Recording
// row. Workflow state (status, errors, timestamps) lives in SocialPosts.
type RecordingPublishingUpdate struct {
	YTLink     *string
	XLink      *string
	XReplyLink *string
}

func ListRecordings(ctx *config.AppContext) ([]*types.Recording, error) {
	if UsePostgresBackend(ctx) {
		return listRecordingsPostgres(ctx)
	}
	return ListRecordingsNotion(ctx)
}

// GetRecordingByConfTalk fetches the Recording row whose talk relation points
// at confTalkID.
func GetRecordingByConfTalk(ctx *config.AppContext, confTalkID string) (*types.Recording, error) {
	if UsePostgresBackend(ctx) {
		return getRecordingByConfTalkPostgres(ctx, confTalkID)
	}
	return getRecordingByConfTalkNotion(ctx, confTalkID)
}

func GetRecordingByID(ctx *config.AppContext, recordingID string) (*types.Recording, error) {
	if UsePostgresBackend(ctx) {
		return getRecordingByIDPostgres(ctx, recordingID)
	}
	return getRecordingByIDNotion(ctx, recordingID)
}

func UpdateRecordingYTLink(ctx *config.AppContext, recordingID, ytLink string) error {
	if UsePostgresBackend(ctx) {
		return updateRecordingYTLinkPostgres(ctx, recordingID, ytLink)
	}
	return updateRecordingYTLinkNotion(ctx, recordingID, ytLink)
}

func UpdateRecordingXLink(ctx *config.AppContext, recordingID, xLink string) error {
	if UsePostgresBackend(ctx) {
		return updateRecordingXLinkPostgres(ctx, recordingID, xLink)
	}
	return updateRecordingXLinkNotion(ctx, recordingID, xLink)
}

func UpdateRecordingPublishAt(ctx *config.AppContext, recordingID string, publishAt *time.Time) error {
	if UsePostgresBackend(ctx) {
		return updateRecordingPublishAtPostgres(ctx, recordingID, publishAt)
	}
	return updateRecordingPublishAtNotion(ctx, recordingID, publishAt)
}

func UpdateRecordingFileURI(ctx *config.AppContext, recordingID, fileURI string) error {
	if UsePostgresBackend(ctx) {
		return updateRecordingFileURIPostgres(ctx, recordingID, fileURI)
	}
	return updateRecordingFileURINotion(ctx, recordingID, fileURI)
}

func UpdateRecordingPublishing(ctx *config.AppContext, recordingID string, up RecordingPublishingUpdate) error {
	if UsePostgresBackend(ctx) {
		return updateRecordingPublishingPostgres(ctx, recordingID, up)
	}
	return updateRecordingPublishingNotion(ctx, recordingID, up)
}
