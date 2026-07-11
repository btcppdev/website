package speakerphotos

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"btcpp-web/external/spaces"
	"btcpp-web/internal/imgproc"
)

// Spaces is the subset of the spaces package the speaker-photo pipeline needs.
type Spaces interface {
	IsConfigured() bool
	Exists(key string) bool
	Upload(key string, data []byte, contentType, hash string) (string, error)
	PublicURL(key string) string
	LoadJSONMap(key string) (map[string]string, error)
	SaveJSONMap(key string, m map[string]string) error
}

type spacesPkgAdapter struct{}

func (spacesPkgAdapter) IsConfigured() bool     { return spaces.IsConfigured() }
func (spacesPkgAdapter) Exists(key string) bool { return spaces.Exists(key) }
func (spacesPkgAdapter) Upload(key string, data []byte, ct, hash string) (string, error) {
	return spaces.Upload(key, data, ct, hash)
}
func (spacesPkgAdapter) PublicURL(key string) string { return spaces.PublicURL(key) }
func (spacesPkgAdapter) LoadJSONMap(key string) (map[string]string, error) {
	return spaces.LoadJSONMap(key)
}
func (spacesPkgAdapter) SaveJSONMap(key string, m map[string]string) error {
	return spaces.SaveJSONMap(key, m)
}

// Pipeline uploads speaker-photo originals and display derivatives.
type Pipeline struct {
	Spaces              Spaces
	MakeAVIF            func(data []byte, size int) ([]byte, error)
	Logf                func(format string, args ...interface{})
	AfterManifestUpdate func()
}

func New(logf func(format string, args ...interface{}), afterManifestUpdate func()) Pipeline {
	return Pipeline{
		Spaces:              spacesPkgAdapter{},
		MakeAVIF:            imgproc.MakeAVIF,
		Logf:                logf,
		AfterManifestUpdate: afterManifestUpdate,
	}
}

// CanonicalPhotoFilename is the value stored in people.norm_photo_path.
func CanonicalPhotoFilename(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	return imgproc.ShortID(raw) + "-400.avif"
}

// Mirror uploads the original speaker photo plus 800px and 400px AVIF
// derivatives under speakers/. Object keys are content-derived, so reruns are
// idempotent and identical photos dedupe.
func (p Pipeline) Mirror(raw []byte, contentType, ext string) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty speaker photo")
	}
	if p.Spaces == nil || !p.Spaces.IsConfigured() {
		return fmt.Errorf("spaces not configured")
	}
	if p.MakeAVIF == nil {
		p.MakeAVIF = imgproc.MakeAVIF
	}
	if p.Logf == nil {
		p.Logf = func(string, ...interface{}) {}
	}
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		ext = ".jpg"
	}

	shortID := imgproc.ShortID(raw)
	origName := shortID + ext
	origKey := "speakers/" + origName

	if !p.Spaces.Exists(origKey) {
		if _, err := p.Spaces.Upload(origKey, raw, contentType, ""); err != nil {
			return fmt.Errorf("speaker pic spaces orig upload %s: %w", shortID, err)
		}
	}
	p.updateManifest(origName, raw)

	for _, size := range []int{800, 400} {
		key := fmt.Sprintf("speakers/%s-%d.avif", shortID, size)
		filename := fmt.Sprintf("%s-%d.avif", shortID, size)
		if p.Spaces.Exists(key) {
			p.updateManifest(filename, raw)
			continue
		}
		avif, err := p.MakeAVIF(raw, size)
		if err != nil {
			p.Logf("speaker pic avif%d generation failed (%s): %s", size, shortID, err)
			continue
		}
		if _, err := p.Spaces.Upload(key, avif, "image/avif", ""); err != nil {
			p.Logf("speaker pic spaces avif%d upload failed (%s): %s", size, shortID, err)
			continue
		}
		p.updateManifest(filename, raw)
	}
	return nil
}

func (p Pipeline) updateManifest(filename string, raw []byte) {
	filename = strings.TrimSpace(filename)
	if filename == "" || len(raw) == 0 || p.Spaces == nil || !p.Spaces.IsConfigured() {
		return
	}
	manifest, err := p.Spaces.LoadJSONMap(spaces.SpeakerManifestKey)
	if err != nil {
		return
	}
	sum := sha256.Sum256(raw)
	manifest[filename] = hex.EncodeToString(sum[:])
	if err := p.Spaces.SaveJSONMap(spaces.SpeakerManifestKey, manifest); err != nil {
		return
	}
	if p.AfterManifestUpdate != nil {
		p.AfterManifestUpdate()
	}
}
