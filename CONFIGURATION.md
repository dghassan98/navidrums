# Configuration

Navidrums is configured via environment variables with sensible defaults. All configuration is validated at startup.

## Environment Variables

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `PORT` | `8080` | No | HTTP server port (1-65535) |
| `DB_PATH` | `navidrums.db` | No | SQLite database file path (Docker: `/data/navidrums.db`) |
| `DOWNLOADS_DIR` | `~/Downloads/navidrums` | No | Output directory for downloaded music (Docker: `/music`) |
| `SUBDIR_TEMPLATE` | `{{.AlbumArtist}}/{{.OriginalYear}} - {{.Album}}/{{.Disc}}-{{.Track}} {{.Title}}` | No | Go template for file organization |
| `PROVIDER_URL` | `http://127.0.0.1:8000` | No | Default HiFi (Tidal) API URL for metadata browsing (additional providers managed via Settings UI) |
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

**Rate limiting**: Each provider enforces a 200ms minimum interval between requests. The global rate limit (`RATE_LIMIT_*`) applies across all providers.

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

## Settings Access

Set `NAVIDRUMS_ADMIN_PASSWORD` to password protect the Settings page. The gate covers the page **and** every
endpoint that reads or changes settings, so it cannot be bypassed by calling the API directly. Unlocking sets
an HttpOnly cookie valid for 12 hours; it is an HMAC keyed by the password, so changing the password
immediately invalidates every existing session. "Lock settings" in the UI ends the session early.

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

## Provider Management

Navidrums supports four provider types: **Monochrome** (see [MONOCHROME_API.md](MONOCHROME_API.md)), **Qobuz Direct** (the official Qobuz API with your own subscription, see [QOBUZ_API.md](QOBUZ_API.md)), **HiFi** (Tidal API proxy) and **Qobuz** (a shared Qobuz proxy). Each type can have multiple endpoint URLs configured as fallbacks.

**Qobuz Direct credentials** live in the environment, never the database: `QOBUZ_APP_ID`, `QOBUZ_APP_SECRET`, `QOBUZ_EMAIL` and `QOBUZ_PASSWORD` (or `QOBUZ_PASSWORD_MD5`). Without them the provider fails with a clear error instead of falling back silently. A free Qobuz account is rejected at login — file URLs need a paid subscription.

Fresh installs are seeded with the instance Monochrome itself defaults to, `https://lol.samidy.workers.dev`, and use it for all three operations. Existing installs keep whatever they were using; switch in Settings.

**Per-operation selection**: Three independent settings control which provider type is used for each operation:
- **Metadata (search/browse)**: Monochrome on fresh installs
- **Download**: Monochrome on fresh installs
- **Streaming**: Monochrome on fresh installs

**Why separate providers**: the legacy HiFi/Tidal `/track/` route frequently returns 30-second previews instead of full tracks. Monochrome serves playback from `/trackManifests/` instead, so a single instance covers browsing, downloads and streaming; Qobuz stays available if you prefer it for downloads. A preview response is treated as a failure so a 30-second clip never lands in your library.

Managing providers:
- **Primary provider**: Sets the default HiFi URL via `PROVIDER_URL` environment variable
- **Settings UI**: Add, reorder (drag), edit, delete provider URLs per type; select which provider type per operation
- **Fallback within type**: Multiple URLs of the same type are tried in position order until one succeeds
- **Cross-provider fallback**: For streaming and downloads, if the primary provider type fails all its URLs, every other configured provider type is tried in turn. When ISRC is missing from the stream request, it is enriched from track metadata before retrying the primary or falling back to the secondary provider.

## Validation

Startup validation — common errors: invalid PORT, PROVIDER_URL, QUALITY, SUBDIR_TEMPLATE, CACHE_TTL, or missing username with password set.

## Docker

Mount: `-v /host/music:/music -v /host/data:/data`. Internal paths: `/music` (downloads), `/data/navidrums.db` (db).

See [.env.sample](../.env.sample) for minimal example.