// Command import-devpost archives public Devpost hackathons and imports the
// reviewed archive into the btcpp PostgreSQL hackathon schema.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"btcpp-web/external/spaces"
	"btcpp-web/internal/db"
	"btcpp-web/internal/devpostimport"
	"btcpp-web/internal/envconfig"
)

type options struct {
	envPath        string
	sourceURL      string
	conferenceTag  string
	outputPath     string
	manifestPath   string
	batchPath      string
	assetsPath     string
	databaseURL    string
	visibility     string
	scrapeOnly     bool
	downloadAssets bool
	dryRun         bool
	rollback       bool
	skipUpload     bool
	createPeople   bool
	importJudges   bool
}

func main() {
	var opts options
	flag.StringVar(&opts.envPath, "env", ".env", "environment file containing DATABASE_URL and Spaces settings")
	flag.StringVar(&opts.sourceURL, "url", "", "Devpost event URL to scrape (for example https://foss.devpost.com/)")
	flag.StringVar(&opts.conferenceTag, "conference", "", "btcpp conference tag to associate with this hackathon")
	flag.StringVar(&opts.outputPath, "out", "", "JSON manifest path written after scraping")
	flag.StringVar(&opts.manifestPath, "manifest", "", "existing reviewed JSON manifest to import")
	flag.StringVar(&opts.batchPath, "batch", "", "JSON file containing [{conference_tag,url,output}, ...] to scrape/import")
	flag.StringVar(&opts.assetsPath, "assets", "", "asset directory (default: <manifest-name>-assets)")
	flag.StringVar(&opts.databaseURL, "database-url", "", "PostgreSQL URL override (default: DATABASE_URL from env)")
	flag.StringVar(&opts.visibility, "visibility", "hidden", "imported hackathon visibility: hidden or public")
	flag.BoolVar(&opts.scrapeOnly, "scrape-only", false, "write the manifest and assets without touching the database")
	flag.BoolVar(&opts.downloadAssets, "download-assets", true, "download every project image into the local archive")
	flag.BoolVar(&opts.dryRun, "dry-run", false, "validate an import against the database and roll it back without uploading")
	flag.BoolVar(&opts.rollback, "rollback", false, "perform uploads and database writes, then roll back the database transaction")
	flag.BoolVar(&opts.skipUpload, "skip-upload", false, "keep Devpost image URLs instead of mirroring originals and AVIFs to Spaces")
	flag.BoolVar(&opts.createPeople, "create-people", false, "create people records for unmatched Devpost members/judges")
	flag.BoolVar(&opts.importJudges, "import-judges", false, "add matched Devpost judges as Expo judges")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	if err := run(ctx, opts); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, opts options) error {
	env, err := envconfig.Load(opts.envPath)
	if err != nil {
		return err
	}
	if opts.databaseURL != "" {
		env.DatabaseURL = opts.databaseURL
	}
	spaces.Init(env.Spaces)

	if opts.batchPath != "" {
		if opts.sourceURL != "" || opts.manifestPath != "" {
			return fmt.Errorf("-batch cannot be combined with -url or -manifest")
		}
		events, err := loadBatch(opts.batchPath)
		if err != nil {
			return err
		}
		for _, event := range events {
			eventOpts := opts
			eventOpts.batchPath = ""
			eventOpts.sourceURL = event.URL
			eventOpts.conferenceTag = event.ConferenceTag
			eventOpts.outputPath = event.Output
			eventOpts.assetsPath = ""
			if err := processOne(ctx, env.DatabaseURL, eventOpts); err != nil {
				return fmt.Errorf("%s: %w", event.ConferenceTag, err)
			}
		}
		return nil
	}
	return processOne(ctx, env.DatabaseURL, opts)
}

func processOne(ctx context.Context, databaseURL string, opts options) error {
	manifestPath := strings.TrimSpace(opts.manifestPath)
	var manifest *devpostimport.Manifest
	if manifestPath != "" {
		loaded, err := loadManifest(manifestPath)
		if err != nil {
			return err
		}
		manifest = loaded
		if opts.conferenceTag != "" {
			manifest.ConferenceTag = opts.conferenceTag
		}
	} else {
		if opts.sourceURL == "" || opts.conferenceTag == "" {
			return fmt.Errorf("scraping requires both -url and -conference")
		}
		manifestPath = opts.outputPath
		if manifestPath == "" {
			manifestPath = filepath.Join("imports", "devpost", opts.conferenceTag+".json")
		}
		scraper := devpostimport.NewScraper()
		scraper.Logf = log.Printf
		log.Printf("scraping %s", opts.sourceURL)
		parsed, err := scraper.Scrape(ctx, opts.sourceURL, opts.conferenceTag)
		if err != nil {
			return err
		}
		manifest = parsed
		if opts.downloadAssets {
			assetRoot := opts.assetsPath
			if assetRoot == "" {
				assetRoot = strings.TrimSuffix(manifestPath, filepath.Ext(manifestPath)) + "-assets"
			}
			relative, err := filepath.Rel(filepath.Dir(manifestPath), assetRoot)
			if err != nil {
				return err
			}
			manifest.AssetsDir = filepath.ToSlash(relative)
			log.Printf("downloading project images to %s", assetRoot)
			if err := scraper.DownloadAssets(ctx, manifest, assetRoot); err != nil {
				return err
			}
		}
		if err := writeManifest(manifestPath, manifest); err != nil {
			return err
		}
		log.Printf("wrote %s (%d projects, %d awards, %d judges)", manifestPath, len(manifest.Projects), len(manifest.Awards), len(manifest.Judges))
	}

	if opts.scrapeOnly {
		return nil
	}
	if strings.TrimSpace(databaseURL) == "" {
		return fmt.Errorf("DATABASE_URL is required for import; use -scrape-only to archive without importing")
	}
	if manifest.ConferenceTag == "" {
		return fmt.Errorf("manifest conference_tag is empty; set it in JSON or pass -conference")
	}
	assetRoot := opts.assetsPath
	if assetRoot == "" {
		assetRoot = filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.AssetsDir))
	}
	if opts.dryRun {
		opts.skipUpload = true
	}
	if err := devpostimport.MirrorProjectImages(manifest, devpostimport.MirrorOptions{
		ManifestDir: assetRoot,
		SkipUpload:  opts.skipUpload,
		Logf:        log.Printf,
	}); err != nil {
		return err
	}

	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	summary, err := devpostimport.Import(ctx, tx, manifest, devpostimport.ImportOptions{
		Visibility:   opts.visibility,
		CreatePeople: opts.createPeople,
		ImportJudges: opts.importJudges,
	})
	if err != nil {
		return err
	}
	if opts.dryRun || opts.rollback {
		if err := tx.Rollback(ctx); err != nil {
			return err
		}
		log.Printf("database import rolled back: %+v", *summary)
		return nil
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if manifestPath != "" {
		if err := writeManifest(manifestPath, manifest); err != nil {
			return fmt.Errorf("database committed but updated image URLs could not be saved to manifest: %w", err)
		}
	}
	log.Printf("import complete: competition=%s projects=%d awards=%d assignments=%d judges=%d people_created=%d members_skipped=%d judges_skipped=%d",
		summary.CompetitionID, summary.Projects, summary.Awards, summary.Assignments, summary.Judges, summary.PeopleCreated, summary.MembersSkipped, summary.JudgesSkipped)
	return nil
}

func loadBatch(path string) ([]devpostimport.BatchEvent, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var events []devpostimport.BatchEvent
	if err := json.Unmarshal(raw, &events); err != nil {
		return nil, fmt.Errorf("parse batch %s: %w", path, err)
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("batch %s contains no events", path)
	}
	for index := range events {
		if events[index].ConferenceTag == "" || events[index].URL == "" {
			return nil, fmt.Errorf("batch event %d requires conference_tag and url", index+1)
		}
		if events[index].Output == "" {
			events[index].Output = filepath.Join("imports", "devpost", events[index].ConferenceTag+".json")
		}
	}
	return events, nil
}

func loadManifest(path string) (*devpostimport.Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest devpostimport.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	return &manifest, nil
}

func writeManifest(path string, manifest *devpostimport.Manifest) error {
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
