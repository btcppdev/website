# Devpost hackathon importer

`cmd/import-devpost` archives a public Devpost hackathon and imports the reviewed data into the btcpp hackathon schema. It handles paginated galleries, project stories, technology tags, team members, screenshots, prizes, winners, and judges.

The workflow is intentionally split into scrape and import phases. Scraping never connects to PostgreSQL. Importing always runs in a transaction and is idempotent by conference, project slug, award title, and Devpost profile URL.

## 1. Scrape and archive

```sh
nix develop -c go run ./cmd/import-devpost \
  -url https://foss.devpost.com/ \
  -conference nairobi \
  -out imports/devpost/nairobi.json \
  -scrape-only
```

This writes:

- `imports/devpost/nairobi.json`: reviewable event/project/award metadata.
- `imports/devpost/nairobi-assets/`: all publicly exposed project image originals, plus the event and judge source images.

Review the JSON before importing. In particular, verify `conference_tag`, award titles, winner mappings, team-member names, and parsed satoshi values. Devpost does not expose team-member email addresses, so people matching uses Devpost profile URL first and a unique case-insensitive name second.

## 2. Validate against a database

```sh
nix develop -c go run ./cmd/import-devpost \
  -manifest imports/devpost/nairobi.json \
  -database-url "$DATABASE_URL" \
  -dry-run
```

Dry-run mode skips Spaces uploads, executes all PostgreSQL work, and rolls the transaction back.

## 3. Import and mirror images

```sh
nix develop -c go run ./cmd/import-devpost \
  -env .env.prod \
  -manifest imports/devpost/nairobi.json \
  -visibility public \
  -create-people \
  -import-judges
```

Unless `-skip-upload` is provided, every project image is uploaded twice:

- Original: `hackathons/{conference}/projects/{project}/{content-hash}.{ext}`
- AVIF: `hackathons/{conference}/projects/{project}/{content-hash}.avif`

The database uses the AVIF URLs, while the manifest retains both URLs. Content hashes make reruns safe and avoid duplicate objects.

`-create-people` is opt-in because Devpost does not publish email addresses and duplicate names can be ambiguous. Without it, projects are still imported; only uniquely matched existing people are attached as team members. `-import-judges` is also opt-in and imports matched judges as Expo judges, never as coordinators.

## Batch imports

Create a JSON file:

```json
[
  {
    "conference_tag": "nairobi",
    "url": "https://foss.devpost.com/",
    "output": "imports/devpost/nairobi.json"
  },
  {
    "conference_tag": "riga25",
    "url": "https://privacy.devpost.com/",
    "output": "imports/devpost/riga25.json"
  }
]
```

Then archive every event without database writes:

```sh
nix develop -c go run ./cmd/import-devpost -batch imports/devpost/events.json -scrape-only
```

After reviewing the manifests, import each one with `-manifest`. A batch can also be imported directly, but keeping production imports one event at a time makes review and rollback simpler.

## Operational flags

- `-rollback`: upload images and execute SQL, but roll back the database transaction.
- `-skip-upload`: retain Devpost-hosted image URLs and do not require Spaces credentials.
- `-assets PATH`: override the local asset archive directory.
- `-database-url URL`: override `DATABASE_URL` from the selected env file.
- `-download-assets=false`: metadata-only scrape; it cannot later mirror images unless the manifest is rescraped or edited with local paths.

The scraper uses a bounded HTTP client, a descriptive user agent, sequential project requests, and a delay between requests. Devpost HTML is external and may change; parser tests cover the currently observed structure, and manifests should always be reviewed before import.
