package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
)

type eventArtworkUpload struct {
	Original, AVIF    string
	OptimizationError error
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
