package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
)

type eventArtworkUpload struct {
	Original, AVIF    string
	OptimizationError error
}

func validateEventArtwork(field, filename string, raw []byte) (string, string, error) {
	logo := field == "black" || field == "white"
	mime := detectedImageContentType(raw, filename, logo)
	if logo && mime == "image/svg+xml" {
		return mime, "svg", nil
	}
	ext := map[string]string{"image/png": "png", "image/jpeg": "jpg"}[mime]
	if ext == "" {
		if logo {
			return "", "", fmt.Errorf("use a PNG, JPEG, or SVG logo")
		}
		return "", "", fmt.Errorf("use a PNG or JPEG image")
	}
	dimensions, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || dimensions.Width < 1 || dimensions.Height < 1 || int64(dimensions.Width)*int64(dimensions.Height) > 40000000 {
		return "", "", fmt.Errorf("use a valid image with at most 40 million pixels")
	}
	return mime, ext, nil
}

// Store originals first. A conversion failure must not discard a valid upload.
// Transparent artwork stays in its original format to preserve alpha exactly.
func storeEventArtwork(confID, field, mime, ext string, raw []byte,
	encode func([]byte) ([]byte, error),
	upload func(string, []byte, string, string) (string, error),
) (eventArtworkUpload, error) {
	var result eventArtworkUpload
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	prefix := "events/" + confID + "/" + field + "/" + digest[:20]
	var err error
	result.Original, err = upload(prefix+"."+ext, raw, mime, digest)
	if err != nil {
		return result, err
	}
	if field != "leading" {
		return result, nil
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		result.OptimizationError = err
		return result, nil
	}
	if opacity, ok := decoded.(interface{ Opaque() bool }); !ok || !opacity.Opaque() {
		return result, nil
	}
	avif, err := encode(raw)
	if err != nil {
		result.OptimizationError = err
		return result, nil
	}
	if len(avif) == 0 || len(avif) >= len(raw) {
		return result, nil
	}
	// Version the encoding recipe independently from the original bytes.
	avifHash := sha256.Sum256(avif)
	result.AVIF, result.OptimizationError = upload(prefix+"-web-v1.avif", avif, "image/avif", hex.EncodeToString(avifHash[:]))
	if result.OptimizationError != nil {
		result.AVIF = ""
	}
	return result, nil
}
