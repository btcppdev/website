// backfill-speaker-manifest hashes existing speakers/* objects in
// Spaces and writes speakers/_manifest.json. This lets media-card
// hashing detect speaker-photo content changes without keeping
// static/img/speakers in the repo.
//
// Usage:
//
//	go run ./cmd/backfill-speaker-manifest
//	go run ./cmd/backfill-speaker-manifest -dry-run
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"log"
	"path"
	"strings"

	"btcpp-web/external/spaces"
	"btcpp-web/internal/envconfig"
)

const (
	keyPrefix = "speakers/"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "Hash objects and report changes without writing the manifest")
	flag.Parse()

	c, err := envconfig.Load(".env")
	if err != nil {
		log.Fatal(err)
	}
	spaces.Init(c.Spaces)
	if !spaces.IsConfigured() {
		log.Fatal("spaces not configured (check SPACES_* env vars)")
	}

	manifest, err := spaces.LoadJSONMap(spaces.SpeakerManifestKey)
	if err != nil {
		log.Fatalf("load speaker manifest: %s", err)
	}
	log.Printf("loaded manifest: %d entries", len(manifest))

	keys, err := spaces.ListKeys(keyPrefix)
	if err != nil {
		log.Fatalf("list %s: %s", keyPrefix, err)
	}

	var scanned, changed, skippedManifest, failed int
	for _, key := range keys {
		name := strings.TrimPrefix(key, keyPrefix)
		if name == "" || name == path.Base(spaces.SpeakerManifestKey) || strings.HasSuffix(key, "/") {
			skippedManifest++
			continue
		}
		body, err := spaces.Get(key)
		if err != nil {
			log.Printf("get %s: %s", key, err)
			failed++
			continue
		}
		scanned++
		hash := contentHash(body)
		if manifest[name] == hash {
			continue
		}
		manifest[name] = hash
		changed++
		log.Printf("manifest %s -> %s", name, hash)
	}

	if changed > 0 && !*dryRun {
		if err := spaces.SaveJSONMap(spaces.SpeakerManifestKey, manifest); err != nil {
			log.Fatalf("save speaker manifest: %s", err)
		}
		log.Printf("manifest updated: %d entries -> %s", len(manifest), spaces.SpeakerManifestKey)
	}

	log.Printf("done: scanned=%d changed=%d skipped-manifest=%d failed=%d dry-run=%t",
		scanned, changed, skippedManifest, failed, *dryRun)
}

func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
