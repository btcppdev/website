package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"

	"btcpp-web/external/spaces"
	"btcpp-web/internal/config"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/speakerphotos"
)

// photoSpaces is the subset of the spaces package the photo pipeline needs.
// Production wraps the package-level functions; tests pass a fake.
type photoSpaces interface {
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

// photoPipeline carries the side-effecting collaborators used by the photo
// upload methods. Production code calls newPhotoPipeline; tests construct
// the struct directly with fakes.
type photoPipeline struct {
	spaces   photoSpaces
	makeAVIF func(data []byte, size int) ([]byte, error)
	logf     func(format string, args ...interface{})
}

func newPhotoPipeline(ctx *config.AppContext) photoPipeline {
	return photoPipeline{
		spaces:   spacesPkgAdapter{},
		makeAVIF: imgproc.MakeAVIF,
		logf:     ctx.Err.Printf,
	}
}

// mirrorPicToSpaces uploads the original cropped speaker photo plus 800px
// and 400px AVIF derivatives. The Spaces key is a content-derived short ID
// so identical photos dedupe across applicants. Layout:
//
//	speakers/{shortID}{ext}        (original)
//	speakers/{shortID}-800.avif    (display size)
//	speakers/{shortID}-400.avif    (thumbnail — Speaker.Photo points here)
//
// Designed to be called from a goroutine; failures are logged and do not
// abort. Each upload is gated by spaces.Exists so re-runs are idempotent.
func (p photoPipeline) mirrorPicToSpaces(raw []byte, contentType, ext string) {
	_ = speakerphotos.Pipeline{
		Spaces:              speakerPhotoSpacesAdapter{p.spaces},
		MakeAVIF:            p.makeAVIF,
		Logf:                p.logf,
		AfterManifestUpdate: InvalidateSpeakerManifest,
	}.Mirror(raw, contentType, ext)
}

type speakerPhotoSpacesAdapter struct{ photoSpaces }

// mirrorOrgLogoToSpaces uploads the org-logo as-is (no crop, no resize, no
// AVIF derivatives) under sponsors/{shortID}{ext}. Idempotent via
// spaces.Exists.
func (p photoPipeline) mirrorOrgLogoToSpaces(raw []byte, contentType, ext string) {
	if !p.spaces.IsConfigured() {
		return
	}
	shortID := imgproc.ShortID(raw)
	key := "sponsors/" + shortID + ext
	if !p.spaces.Exists(key) {
		if _, err := p.spaces.Upload(key, raw, contentType, ""); err != nil {
			p.logf("org logo spaces upload failed (%s): %s", shortID, err)
			return
		}
	}
	p.updateOrgLogoManifest(key, raw)
}

func (p photoPipeline) updateOrgLogoManifest(key string, raw []byte) {
	manifest, err := p.spaces.LoadJSONMap(spaces.SponsorManifestKey)
	if err != nil {
		p.logf("org logo manifest load failed: %s", err)
		return
	}
	sum := sha256.Sum256(raw)
	manifest[path.Base(key)] = hex.EncodeToString(sum[:])
	if err := p.spaces.SaveJSONMap(spaces.SponsorManifestKey, manifest); err != nil {
		p.logf("org logo manifest save failed: %s", err)
		return
	}
	InvalidateSponsorManifest()
}

// uploadSatelliteImage stores the original satellite image. When the upload is
// a PNG, it also generates an AVIF derivative and returns that AVIF key as the
// canonical display asset so public pages serve the converted image.
func (p photoPipeline) uploadSatelliteImage(confTag string, kind string, raw []byte, contentType string, ext string) (string, string, error) {
	if !p.spaces.IsConfigured() {
		return "", "", fmt.Errorf("spaces not configured")
	}

	shortID := imgproc.ShortID(raw)
	origKey := fmt.Sprintf("%s/satellites/%s-%s%s", confTag, kind, shortID, ext)
	displayKey := origKey
	displayData := raw
	displayType := contentType

	if contentType == "image/png" {
		displayKey = fmt.Sprintf("%s/satellites/%s-%s.avif", confTag, kind, shortID)
		displayType = "image/avif"
		if !p.spaces.Exists(displayKey) {
			avif, err := p.makeAVIF(raw, 0)
			if err != nil {
				return "", "", fmt.Errorf("satellite avif encode: %w", err)
			}
			displayData = avif
		} else {
			displayData = nil
		}
	}

	if !p.spaces.Exists(origKey) {
		if _, err := p.spaces.Upload(origKey, raw, contentType, ""); err != nil {
			return "", "", fmt.Errorf("satellite orig upload: %w", err)
		}
	}
	if displayData != nil && !p.spaces.Exists(displayKey) {
		if _, err := p.spaces.Upload(displayKey, displayData, displayType, ""); err != nil {
			return "", "", fmt.Errorf("satellite display upload: %w", err)
		}
	}

	return displayKey, p.spaces.PublicURL(displayKey), nil
}
