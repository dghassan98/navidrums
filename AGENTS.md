## What this project is

Navidrums is a Go download orchestrator and metadata browser. NOT a streaming server — all downloads are async via background workers.

---

## Build/Test/Lint/Dev Commands

```bash
go build -o navidrums ./cmd/server      # Build
go run ./cmd/server                      # Run
air                                      # Hot reload (builds to ./tmp/main)
go test ./...                            # Test
go test -race ./...                      # CI runs with -race
golangci-lint run                        # Lint (uses .golangci.yml v2 config)
go fmt ./...                             # Format
```

**Lint notes**: `navidrome_data/` is excluded from linting. Test files skip `errcheck`, `ineffassign`, `unused`, and `gosec`.

---

## Testing

- **stdlib only** — no testify, no assert/require. Use `if got != want { t.Errorf(...) }`.
- **Table-driven** with `t.Run()` sub-tests everywhere.
- **`setupTestDB(t)` helper** returns `(*sql.DB, func())` — duplicated identically in `internal/store/db_test.go` and `internal/app/job_service_test.go`. Use it for any test that hits SQLite.
- **No `t.Parallel()`** — config tests mutate real env vars via `os.Setenv`/`os.Unsetenv` (not `t.Setenv()`). Adding `t.Parallel()` would race.

---

## Key Architecture Facts

**Entry point**: `cmd/server/main.go`. Module: `github.com/cesargomez89/navidrums`. Go 1.25.6.

**Tech stack**: `chi/v5` router, `modernc.org/sqlite` (pure Go, no CGO), HTMX frontend with embedded templates (`web/embed.go` `//go:embed`), `go-playground/form/v4`.

**Dependency flow**:
```
handlers → services → repository, providers, storage
worker → services, providers, storage, tagging
```

**Forbidden**: reverse directions above, plus downloads/goroutines in handlers, DB access outside store, file writes outside `internal/storage`.

**Tagging**: FLAC and MP3 are tagged via Go libraries (`go-flac`, `id3v2/v2`). MP4/M4A requires external `ffmpeg` binary — verify it exists before relying on MP4 tagging.

**Implementation order**: services (`internal/app`) → repository (`internal/store`) → worker (`internal/downloader`) → handlers LAST (`internal/http`).

**Releases**: tag `v*` on main triggers goreleaser (binary-only format) + Docker push to GHCR. Docker uses `CGO_ENABLED=0`.

---

## Job Lifecycle

```
queued → running → [decomposed] → completed | failed | cancelled
```

Container jobs (album/playlist/artist/discography): fetch tracks → create Track records → create child track jobs → **decomposed** → completed when all children finish.

Track jobs: lookup stored Track → check downloaded → download → tag → save art → update Track.

**Rules**: Cancelled must stop work. No return to queued. Workers persist all transitions. Stuck `running` jobs reset to `queued` on startup.

---

## Data Invariants

- Track file must exist before tagging
- `provider_id` UNIQUE in tracks prevents duplicate downloads
- Container jobs decompose into track jobs via Track records
- Deleting a job does not delete the downloaded files
- `Job.SourceID` links to `Track.ProviderID`
- Tracks = full metadata, Jobs = minimal state

---

## Critical Don'ts

- no downloads in http
- no goroutines in http
- no DB access outside store
- no blocking requests in handlers
- no provider calls from UI code
- no file writes outside `internal/storage`
- no job mutation outside app services

---

## Key Env Vars

See `CONFIGURATION.md` / `README.md` for the full list. These are the ones agents most often miss:

| Variable | Default | Note |
|---|---|---|
| `PLAY_QUALITY` | HIGH | Streaming preview quality |
| `THEME` | golden | UI theme |
| `SKIP_AUTH` | false | Behind reverse proxy? set true |
| `NAVIDRUMS_ADMIN_PASSWORD` | (empty) | Gates the Settings page and every settings endpoint |
| `DISABLE_RATE_LIMIT` | false | Behind Cloudflare? set true |

---

## Code Style (non-obvious)

**Imports**: stdlib → third-party → internal (blank-line separated groups).

**Errors**: `Err` prefix for exported sentinels, `fmt.Errorf("failed to X: %w")` in services, `http.Error()` in handlers, `defer recover()` in workers.

**Formatting**: tabs, aim 100 chars, hard limit 120.

---

## Reference Docs

- `DOMAIN.md` — full domain model field specifications (Job, Track, CatalogTrack, etc.)
- `ARCHITECTURE.md` — package structure, layer flow, metadata enrichment, sync jobs
- `CONFIGURATION.md` — all env vars, template examples, genre mapping
- `DESIGN_SYSTEM.md` — CSS/HTML patterns for UI work
- `MONOCHROME_API.md` — default catalog/playback API: `/trackManifests/`, preview handling, instances
- `HIFI_API.md` — legacy catalog API endpoints (shared metadata routes)
- `QOBUZ_API.md` — both Qobuz providers: the shared proxy and `qobuz-direct` (login, request signing, format ids)
