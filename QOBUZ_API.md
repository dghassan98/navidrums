## Qobuz API

Two Qobuz provider types exist: **`qobuz`**, which calls a shared third-party proxy, and
**`qobuz-direct`**, which calls the official Qobuz API with your own subscription.

---

## Qobuz Proxy (`qobuz`)

Base: `https://qobuz.kennyy.com.br/api`

### Endpoints

**Search** — `GET /get-music?q={query}&offset=0` — returns albums, tracks, artists, playlists, stories, most_popular (each: total + items).

**Album** — `GET /get-album?album_id={uuid}` — metadata + embedded tracks with audio_info, performers, isrc, maximum_bit_depth, hires.

**Artist** — `GET /get-artist?artist_id={int}` — metadata, albums (grouped by type), toptracks, similar_artists, playlists.

**Track** — `GET /get-track?isrc={int}` — **param is track ID**, not ISRC code. Returns track + embedded album.

**Download** — `GET /download-music?track_id={int}&quality={int}` — returns signed Akamai URL (time-limited). Quality: `6` = LOSSLESS.

### Data Types

**Album**: id (UUID), title, artist, artists[], label, upc, genre, image, release_date_original, tracks_count, tracks.items[], copyright, parental_warning.

**Track**: id, title, track_number, media_number (disc), duration, isrc, performers, composer, performer, audio_info, copyright, parental_warning, version.

**Audio**: maximum_bit_depth, maximum_sampling_rate, hires, hires_streamable, audio_info.replaygain_*.

### Design Notes

- Album IDs are UUIDs (strings), not integers
- Tracks embedded in album response
- `/get-track` param `isrc` is misnamed — takes track ID
- Download returns time-limited signed CDN URL
- Search returns all types at once
- Cover art: `static.qobuz.com` at `_50.jpg`, `_230.jpg`, `_600.jpg`

See `api-examples/qobuz-api/` for JSON examples.
---

## Qobuz Direct (your own subscription)

The provider type above talks to a third-party proxy. `qobuz-direct` instead calls the official Qobuz API
as **you**, using your own paid subscription.

### Base URL

`https://www.qobuz.com/api.json/0.2` (seeded automatically; not usually changed)

### Credentials

Qobuz has no self-serve developer program for this, so there is no API key to request. Four values are
needed, all supplied through the environment — none are stored in the database:

| Variable | What it is |
|---|---|
| `QOBUZ_APP_ID` | 9-digit application id from the Qobuz web player bundle |
| `QOBUZ_APP_SECRET` | 32-character secret from the same bundle |
| `QOBUZ_EMAIL` | your Qobuz account email |
| `QOBUZ_PASSWORD` | your password; hashed to MD5 at startup |
| `QOBUZ_PASSWORD_MD5` | pre-hashed alternative, wins over `QOBUZ_PASSWORD` |

`app_id`/`app_secret` identify the *application*, not you, and Qobuz rotates them; when downloads start
failing with a 400, refresh them.

### Endpoints

All requests carry `app_id` (query and `X-App-Id`) and, after login, `X-User-Auth-Token`.

- **Login** — `GET /user/login?email=&password={md5}&app_id=` → `user_auth_token`.
  A `credential.parameters` of `null` means a free account, which cannot stream; Navidrums reports this as
  `ErrQobuzIneligible` rather than failing obscurely later.
- **Search** — `GET /{album,track,artist,playlist}/search?query=&limit=` — there is no combined search.
- **Album** — `GET /album/get?album_id=` — same object the proxy wraps, so the DTOs are shared.
- **Track** — `GET /track/get?track_id=`
- **Artist** — `GET /artist/get?artist_id=&extra=albums` — note `name` is a plain string here, unlike the
  `artist/page` shape the proxy returns.
- **Playlist** — `GET /playlist/get?playlist_id=&extra=tracks`
- **File URL** — `GET /track/getFileUrl?request_ts=&request_sig=&track_id=&format_id=&intent=stream`

### Request signing

`track/getFileUrl` must be signed. The signature is the MD5 of the endpoint, then each parameter name and
value in alphabetical order, then the timestamp and the app secret:

```
md5("trackgetFileUrl" + "format_id" + {format_id} + "intentstream" + "track_id" + {track_id} + {ts} + {app_secret})
```

The same `{ts}` goes out as `request_ts`. An unsigned or stale request gets a 400.

### Format IDs

| Quality | format_id | Result |
|---|---|---|
| `HI_RES_LOSSLESS` | 27 | FLAC up to 24-bit/192kHz |
| `LOSSLESS` | 6 | FLAC 16-bit/44.1kHz |
| `HIGH` / `LOW` | 5 | MP3 320 |

Qobuz downgrades on its own when a release is not available at the requested tier. A track that cannot be
streamed at all comes back with an empty `url` and a `restrictions[].code`, which Navidrums surfaces as
`ErrQobuzNotStreamable`.

### Design Notes

- The auth token is cached in memory and refreshed automatically on a 401.
- Metadata DTOs are shared with the proxy provider — the proxy simply wraps these objects in
  `{success, data}`.
- ISRC lookups go through `track/search` and are only trusted on an exact ISRC match.
- Using the web player's `app_id`/`app_secret` this way is outside Qobuz's terms of service, even with a
  paid account.
