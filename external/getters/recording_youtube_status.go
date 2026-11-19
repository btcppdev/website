package getters

import (
	"btcpp-web/internal/config"
	"fmt"
)

func CacheRecordingYouTubeStatus(app *config.AppContext, id, videoURL, privacy string) error {
	if app == nil || app.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	_, err := app.DB.Exec(app.DatabaseContext(), `INSERT INTO recording_youtube_status(recording_id,youtube_url,privacy_status) SELECT id,youtube_url,$3 FROM recordings WHERE id=$1 AND youtube_url=$2 ON CONFLICT(recording_id) DO UPDATE SET youtube_url=EXCLUDED.youtube_url,privacy_status=EXCLUDED.privacy_status,checked_at=now()`, id, videoURL, privacy)
	return err
}
