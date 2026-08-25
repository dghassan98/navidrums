# Configuration

Navidrums is configured via environment variables with sensible defaults. All configuration is validated at startup.

## Environment Variables

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `PORT` | `8080` | No | HTTP server port (1-65535) |
| `DB_PATH` | `navidrums.db` | No | SQLite database file path (Docker: `/data/navidrums.db`) |
| `DOWNLOADS_DIR` | `~/Downloads/navidrums` | No | Output directory for downloaded music (Docker: `/music`) |
| `SUBDIR_TEMPLATE` | `{{.AlbumArtist}}/{{.OriginalYear}} - {{.Album}}/{{.Disc}}-{{.Track}} {{.Title}}` | No | Go template for file organization |
| `QUALITY` | `LOSSLESS` | No | Audio quality preference (`LOSSLESS`, `HI_RES_LOSSLESS`, `HIGH`, `LOW`) |
| `LOG_LEVEL` | `info` | No | Logging level (`debug`, `info`, `warn`, `error`) |
| `LOG_FORMAT` | `text` | No | Log output format (`text`, `json`) |
| `NAVIDRUMS_USERNAME` | `navidrums` | No* | Username for HTTP basic authentication |
| `NAVIDRUMS_PASSWORD` | (empty) | No | Password for HTTP basic authentication (empty disables auth) |
| `CACHE_TTL` | `12h` | No | Provider response cache TTL (e.g., `1h`, `24h`, `7d`) |
| `MUSICBRAINZ_CACHE_TTL` | `7d` | No | MusicBrainz API response cache TTL (e.g., `1d`, `168h`) |
| `MUSICBRAINZ_URL` | `https://musicbrainz.org/ws/2` | No | MusicBrainz API endpoint for metadata enrichment |
| `RATE_LIMIT_REQUESTS` | `200` | No | Maximum requests per rate limit window |
| `RATE_LIMIT_WINDOW` | `1m` | No | Rate limit time window (e.g., `30s`, `1m`) |
| `RATE_LIMIT_BURST` | `10` | No | Burst requests allowed beyond rate limit |
| `SKIP_AUTH` | `false` | No | Set to `true` to disable authentication entirely |
| `THEME` | `golden` | No | Default application theme (can be overridden in Settings) |
| `FFMPEG_PATH` | (system) | No | Path to ffmpeg binary (required for MP4/M4A tagging - hi-res downloads often come as MP4) |
| `FFPROBE_PATH` | (system) | No | Path to ffprobe binary |

**Rate limiting**: The Qobuz client enforces a 500ms minimum interval between requests. The global rate limit (`RATE_LIMIT_*`) applies to inbound HTTP.

**Note:** ffmpeg is only required when tagging MP4/M4A files (common for hi-res audio). FLAC and MP3 files are tagged using native Go libraries.

\* `NAVIDRUMS_USERNAME` is required only when `NAVIDRUMS_PASSWORD` is set.

## Template Variables

The `SUBDIR_TEMPLATE` uses Go's `text/template` syntax with these available variables:

| Variable | Description | Example |
|----------|-------------|---------|
| `{{.AlbumArtist}}` | Album artist (falls back to track artist if empty) | `Pink Floyd` |
| `{{.OriginalYear}}` | Release year (integer) | `1973` |
| `{{.Album}}` | Album name | `The Dark Side of the Moon` |
| `{{.Disc}}` | Disc number, zero-padded (01, 02, etc.) | `01` |
| `{{.Track}}` | Track number, zero-padded (01, 02, etc.) | `01` |
| `{{.Title}}` | Track title | `Speak to Me` |

The file extension (`.flac`, `.mp3`, or `.mp4`) is appended automatically.

### Example

`{{.AlbumArtist}}/{{.OriginalYear}} - {{.Album}}/{{.Disc}}-{{.Track}} {{.Title}}` → `Pink Floyd/1973 - The Dark Side/01-01 Speak to Me.flac`

**Note:** Invalid filesystem characters (`<>:"/\|?*`) are automatically sanitized from paths.

> Cache TTL: `CACHE_TTL=12h`, `MUSICBRAINZ_CACHE_TTL=7d`. SQLite storage, auto-invalidated on provider change.

## Genre Map

Normalizes MusicBrainz subgenre tags → main genres. Configure in Settings UI.

- Default: Rock, Metal, Pop, Hip-Hop, R&B, Electronic, Latin, Regional Mexican, Country, Jazz, Classical, Folk, Reggae, Blues, Soundtrack
- Custom: JSON `{"dark ambient": "Electronic", ...}` — "Reset to Default" clears

## Authentication

Basic HTTP authentication is optional:
- Set `NAVIDRUMS_PASSWORD` to enable authentication
- Leave `NAVIDRUMS_PASSWORD` empty to disable authentication
- When password is set, `NAVIDRUMS_USERNAME` must also be set

## Rate Limiting

Static assets (`/static/*`) and proxied artwork (`/img`) are exempt: one page of results asks for dozens of
covers at once, and counting them starves the requests that matter. Everything else is limited per client IP.

Defaults are 600 requests/minute with a burst of 60. The burst matters more than the rate here, because the
Settings page fires around twenty requests as it loads; a burst below that makes it fail to load with 429s
rather than appear slow.

Behind a reverse proxy the client IP comes from `X-Forwarded-For` or `X-Real-IP`. Make sure the proxy sets
one, or every request is attributed to the proxy and all users share a single limit.

## Notifications

Set `NOTIFY_URL`, or fill it in under **Settings → Notifications**, to be told when downloads finish. A
Discord webhook URL works directly; anything else is treated as an [Apprise](https://github.com/caronc/apprise-api)
API endpoint, which fans out to whatever you have configured there. The shape is chosen from the URL, and a
value saved in Settings overrides the environment without a restart.

Album, playlist and artist downloads notify **once** when the whole job settles, rather than once per track,
which would flood a channel. Standalone track downloads notify individually. A webhook failure is logged and
dropped: it never fails or delays a download.

The webhook URL is a credential — anyone holding it can post to your channel — so it is masked in the UI and
is best kept in the environment on a shared machine.

## Checking the running version

`GET /version` reports the build, so a deployment can be checked against the repository:

```json
{"version": "5042db3", "commit": "5042db39...", "modified": false, "built": "...", "go": "go1.27.0"}
```

`.dockerignore` excludes `.git`, so the toolchain cannot stamp the revision inside the image. Pass it in:

```bash
VERSION=$(git rev-parse --short HEAD) docker compose up -d --build
```

Without it the image reports `dev`, which is itself a useful signal that the build was not stamped.

## Settings Access

Set `NAVIDRUMS_ADMIN_PASSWORD` to password protect the Settings page. The gate covers the page **and** every
endpoint that reads or changes settings, so it cannot be bypassed by calling the API directly. Unlocking sets
an HttpOnly session cookie, so Settings re-locks when the browser closes and again after
`NAVIDRUMS_ADMIN_SESSION_TTL` (default 30 minutes), which is enforced server side. The cookie is an HMAC keyed
by the password, so changing the password immediately invalidates every existing session. "Lock settings" in the UI ends the session early.

Leaving the variable unset leaves Settings open, which is the previous behaviour. This is separate from
`NAVIDRUMS_PASSWORD`, which protects the whole application.

## Qobuz Credentials

Qobuz credentials can come from the environment or from **Settings → Qobuz Credentials**. A value saved in
Settings overrides the matching environment variable and applies without a restart; clearing it falls back to
the environment. Values entered in the UI are stored in the Navidrums database in plain text, so prefer the
environment on a shared machine. A plaintext password is hashed to MD5 before storage.

**Test connection** reports each credential separately, because they fail independently:

| Reported | Meaning |
|---|---|
| App ID `rejected` | the app id is not accepted; re-read it from the web player bundle |
| Account `rejected` | the auth token expired, or the password changed |
| App secret `rejected` | Qobuz rotated the secret; browsing still works but downloads fail |

`app_id` and `app_secret` are read from the Qobuz web player bundle and Qobuz rotates them, and auth tokens
expire, so expect to refresh these occasionally. See [QOBUZ_API.md](QOBUZ_API.md) for where to find them.

## Catalog Provider

Qobuz is the only catalog provider. Browsing, downloads and streaming all go through the
official Qobuz API using your own subscription — see [QOBUZ_API.md](QOBUZ_API.md).

**Credentials** come from the environment or Settings: `QOBUZ_APP_ID`, `QOBUZ_APP_SECRET`, and
either `QOBUZ_AUTH_TOKEN` or `QOBUZ_EMAIL` plus `QOBUZ_PASSWORD` (or `QOBUZ_PASSWORD_MD5`).
Values saved in Settings win over the environment, so credentials can be rotated without a
restart. Without them, browsing and downloads fail with a clear error rather than silently
returning nothing. A free Qobuz account is rejected at login — file URLs need a paid
subscription.

`app_id` and `app_secret` identify the Qobuz *web player*, not you, and Qobuz rotates them.
When downloads start failing with a 400, refresh them. Settings → Qobuz status probes
`app_id`, the account and `app_secret` independently, because they fail for different reasons.

**Endpoint**: defaults to `https://www.qobuz.com/api.json/0.2`. The `qobuz_base_url` setting
overrides it; this is almost never needed.

**Caching**: catalog responses are cached for `CACHE_TTL` (default 12h). Browse data uses its
own TTLs — 24 hours for the genre tree, which is effectively static, and 1 hour for editorial
rows and label pages.

## Music Library Index (optional)

A **read-only** mirror of what your Navidrome library already holds, so browse pages can
show what is worth downloading rather than what you already own.

| Variable | Purpose |
|---|---|
| `NAVIDROME_URL` | Base URL, e.g. `https://navidrome.example.com` |
| `NAVIDROME_USER` | A Navidrome user; a plain non-admin account is enough |
| `NAVIDROME_PASSWORD` | That user's password |

Navidrums never modifies the library. Every call is a Subsonic GET, the password is sent
only as a per-request salted token, and a test in `internal/subsonic` enforces both by
asserting on the requests actually issued. The account needs no write permission.

Sync from Settings → Music Library → **Sync now**. It rebuilds the index in one pass — a
few thousand tracks takes seconds — and a failed sync leaves the previous index intact
rather than a partial one.

**Matching** is two-tier: exact on ISRC where present, then on normalised title and artist
for everything else. Normalisation lowercases, folds Latin accents, strips punctuation and
drops release qualifiers like `(Remastered 2011)`, while leaving non-Latin scripts
untouched. It errs toward keeping distinct tracks apart, because a false match means
skipping an album you do not actually have.

**Ownership is reported per track, never as a boolean per album** — an album shows
`5 of 13 tracks`. It also tracks whether the copies you hold are lossless, so an album you
own only as MP3 still reads as worth downloading.

## Discover Rows

The home page shows editorial rows from Qobuz, configured in Settings → Discover Rows. Rows
can be enabled, disabled and reordered; the stored value is a JSON array of
`{"kind": ..., "enabled": ...}`.

Valid kinds are the types `album/getFeatured` accepts — `new-releases`, `recent-releases`,
`new-releases-full`, `editor-picks`, `press-awards`, `most-streamed`, `most-featured`,
`best-sellers`, `ideal-discography`, `qobuzissims`, `harmonia-mundi`, `universal-classic`,
`universal-jazz`, `universal-jeunesse`, `universal-chanson` — plus `playlists`, which maps to
`playlist/getFeatured`. Unrecognised kinds in a stored config are ignored rather than
requested, since Qobuz answers an unknown type with a 400.

Each row loads independently over htmx, so a slow or failing row cannot block the page.

## Validation

Startup validation — common errors: invalid PORT, QUALITY, SUBDIR_TEMPLATE, CACHE_TTL, or missing username with password set.

## Docker

Mount: `-v /host/music:/music -v /host/data:/data`. Internal paths: `/music` (downloads), `/data/navidrums.db` (db).

See [.env.sample](../.env.sample) for minimal example.