package devpostimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"btcpp-web/external/spaces"
	"btcpp-web/internal/imgproc"
)

const maxRemoteImageBytes = 24 << 20

func (s *Scraper) DownloadAssets(ctx context.Context, manifest *Manifest, root string) error {
	if manifest == nil {
		return fmt.Errorf("manifest is required")
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return fmt.Errorf("asset directory is required")
	}
	client := s.Client
	if client == nil {
		client = NewScraper().Client
	}
	download := func(asset *Asset, relativeDir, label string) error {
		if asset == nil || asset.SourceURL == "" || asset.LocalPath != "" {
			return nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.SourceURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "btcpp-devpost-importer/1.0 (+https://btcpp.dev)")
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("download %s: %w", asset.SourceURL, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("download %s: status %d", asset.SourceURL, resp.StatusCode)
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteImageBytes+1))
		if err != nil {
			return err
		}
		if len(raw) == 0 || len(raw) > maxRemoteImageBytes {
			return fmt.Errorf("download %s: invalid size %d", asset.SourceURL, len(raw))
		}
		contentType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
		if !strings.HasPrefix(contentType, "image/") {
			contentType = http.DetectContentType(raw)
		}
		if !strings.HasPrefix(contentType, "image/") {
			return fmt.Errorf("download %s: unsupported content type %s", asset.SourceURL, contentType)
		}
		ext := imageExtension(contentType, asset.SourceURL)
		sum := sha256.Sum256(raw)
		name := fmt.Sprintf("%s-%s%s", slugify(label), hex.EncodeToString(sum[:6]), ext)
		relativePath := filepath.Join(relativeDir, name)
		fullPath := filepath.Join(root, relativePath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(fullPath, raw, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", fullPath, err)
		}
		asset.LocalPath = filepath.ToSlash(relativePath)
		asset.ContentType = contentType
		return nil
	}

	if err := download(&manifest.Competition.HeroImage, "event", "hero"); err != nil {
		return err
	}
	for projectIndex := range manifest.Projects {
		project := &manifest.Projects[projectIndex]
		for imageIndex := range project.Images {
			if err := download(&project.Images[imageIndex], filepath.Join("projects", project.Slug), fmt.Sprintf("image-%02d", imageIndex+1)); err != nil {
				return err
			}
		}
	}
	for judgeIndex := range manifest.Judges {
		if err := download(&manifest.Judges[judgeIndex].Photo, "judges", fmt.Sprintf("judge-%02d", judgeIndex+1)); err != nil {
			return err
		}
	}
	return nil
}

type MirrorOptions struct {
	ManifestDir string
	SkipUpload  bool
	Logf        func(string, ...any)
}

func MirrorProjectImages(manifest *Manifest, opts MirrorOptions) error {
	if manifest == nil {
		return fmt.Errorf("manifest is required")
	}
	if opts.SkipUpload {
		for projectIndex := range manifest.Projects {
			for imageIndex := range manifest.Projects[projectIndex].Images {
				asset := &manifest.Projects[projectIndex].Images[imageIndex]
				asset.AVIFURL = asset.SourceURL
				asset.OriginalURL = asset.SourceURL
			}
		}
		return nil
	}
	if !spaces.IsConfigured() {
		return fmt.Errorf("Spaces is not configured")
	}
	conf := slugify(manifest.ConferenceTag)
	if conf == "" {
		return fmt.Errorf("conference tag is required before uploading images")
	}
	for projectIndex := range manifest.Projects {
		project := &manifest.Projects[projectIndex]
		for imageIndex := range project.Images {
			asset := &project.Images[imageIndex]
			if asset.LocalPath == "" {
				return fmt.Errorf("project %s image %d has no local_path; scrape with asset downloads enabled", project.Slug, imageIndex+1)
			}
			raw, err := os.ReadFile(filepath.Join(opts.ManifestDir, filepath.FromSlash(asset.LocalPath)))
			if err != nil {
				return fmt.Errorf("read project image %s: %w", asset.LocalPath, err)
			}
			ext := imageExtension(asset.ContentType, asset.LocalPath)
			shortID := imgproc.ShortID(raw)
			prefix := fmt.Sprintf("hackathons/%s/projects/%s/%s", conf, project.Slug, shortID)
			originalKey := prefix + ext
			avifKey := prefix + ".avif"
			if opts.Logf != nil {
				opts.Logf("mirror %s image %d", project.Slug, imageIndex+1)
			}
			if !spaces.Exists(originalKey) {
				asset.OriginalURL, err = spaces.Upload(originalKey, raw, asset.ContentType, "")
				if err != nil {
					return err
				}
			} else {
				asset.OriginalURL = spaces.PublicURL(originalKey)
			}
			if !spaces.Exists(avifKey) {
				avif, err := imgproc.MakeAVIF(raw, 0)
				if err != nil {
					return fmt.Errorf("make AVIF for %s: %w", asset.LocalPath, err)
				}
				asset.AVIFURL, err = spaces.Upload(avifKey, avif, "image/avif", "")
				if err != nil {
					return err
				}
			} else {
				asset.AVIFURL = spaces.PublicURL(avifKey)
			}
		}
	}
	return nil
}

func imageExtension(contentType, rawURL string) string {
	contentType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	if extensions, _ := mime.ExtensionsByType(contentType); len(extensions) > 0 {
		for _, ext := range extensions {
			if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" || ext == ".gif" || ext == ".avif" {
				if ext == ".jpeg" {
					return ".jpg"
				}
				return ext
			}
		}
	}
	parsed, _ := url.Parse(rawURL)
	ext := strings.ToLower(filepath.Ext(parsed.Path))
	if ext == ".jpeg" {
		return ".jpg"
	}
	if ext != "" && len(ext) <= 6 {
		return ext
	}
	return ".img"
}

func NewAssetHTTPClient() *http.Client {
	return &http.Client{Timeout: 45 * time.Second}
}
