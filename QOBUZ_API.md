## Qobuz API

Qobuz is the only catalog provider. Navidrums calls the official API with your own
subscription.

---

## Credentials and endpoints

Navidrums calls the official Qobuz API as **you**, using your own paid subscription.

### Base URL

`https://www.qobuz.com/api.json/0.2` — the default. Override with `qobuz_base_url` in Settings;
almost never needed.

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
- **Album** — `GET /album/get?album_id=`
- **Track** — `GET /track/get?track_id=`
- **Artist** — `GET /artist/get?artist_id=&extra=albums`, and `artist/page` for the richer shape
- **Playlist** — `GET /playlist/get?playlist_id=&extra=tracks`
- **File URL** — `GET /track/getFileUrl?request_ts=&request_sig=&track_id=&format_id=&intent=stream`

### Browse endpoints

- **Featured** — `GET /album/getFeatured?type=&genre_id=&limit=&offset=`. An unknown `type` returns a 400
  whose message enumerates the valid set, which is where this list came from:
  `most-streamed`, `best-sellers`, `new-releases`, `press-awards`, `editor-picks`, `most-featured`,
  `harmonia-mundi`, `universal-classic`, `universal-jazz`, `universal-jeunesse`, `universal-chanson`,
  `new-releases-full`, `recent-releases`, `ideal-discography`, `qobuzissims`.
- **Featured playlists** — `GET /playlist/getFeatured?type=editor-picks&genre_ids=&limit=&offset=`. Note
  the parameter is `genre_ids`, plural, unlike `album/getFeatured`. Playlist objects here differ from the
  ones search returns: the title is `name`, and images arrive as parallel arrays (`images`, `images150`,
  `images300`) rather than an object.
- **Genres** — `GET /genre/list` returns the 13 top level genres only. `genre/get?extra=subgenres`
  **ignores the extra** and returns the same flat object, so children come from
  `GET /genre/list?parent_id={id}` — one call per top level genre, which is why the tree is cached for 24
  hours.
- **Label** — `GET /label/get?label_id=&extra=albums&limit=&offset=`. There is no sort parameter, so
  ordering can only be applied to the page that comes back.

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

### Checking credential health

`GET /htmx/qobuz-status` probes Qobuz and reports `app_id`, `account` and `app_secret` independently, which
matters because they fail for different reasons. Two probes are used:

1. `favorite/getUserFavorites` — needs a valid auth token but **no** signature, so a 401 here isolates an
   expired token. This matters because a token supplied via `QOBUZ_AUTH_TOKEN` is otherwise never exercised
   until a real request fails.
2. `track/getFileUrl` on a fixed catalogue track — a 400 means the signature did not verify, i.e. the app
   secret has been rotated. Nothing is downloaded; only the status code is read.

### Design Notes

- The auth token is cached in memory and refreshed automatically on a 401.
- ISRC lookups go through `track/search` and are only trusted on an exact ISRC match.
- Using the web player's `app_id`/`app_secret` this way is outside Qobuz's terms of service, even with a
  paid account.
