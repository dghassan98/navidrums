## Monochrome API (External Service)

**Status**: Preferred provider for browsing, downloading and streaming. Monochrome
([monochrome-music/monochrome](https://github.com/monochrome-music/monochrome)) dropped its Qobuz proxy —
an instance now serves catalog metadata **and** full-length playback on its own, so Navidrums no longer
needs a Qobuz provider to download reliably.

### Base URL

Configurable per instance in **Settings → MONOCHROME Instances**. Fresh installs are seeded with the
instance Monochrome itself defaults to:

```
https://lol.samidy.workers.dev
```

That URL is a load balancer: `GET /` reports the upstream instances behind it and their health. Add more
instances to the list to get fallbacks — Navidrums tries them in order until one succeeds.

Other instances are listed in Monochrome's [INSTANCES.md](https://github.com/monochrome-music/monochrome/blob/main/INSTANCES.md).
Instances vary in what they can serve: the accounts behind an instance decide whether you get full tracks
or 30-second previews, and whether hi-res is available at all.

### Endpoints

Monochrome serves the same catalog routes as the hifi-api described in [HIFI_API.md](HIFI_API.md), so
search, artist, album, playlist, track info and recommendations are shared with the HiFi provider. The
differences that matter:

#### Playback — `GET /trackManifests/`

Replaces the legacy `GET /track/` route, which now returns 30-second previews.

| Param | Value |
|---|---|
| `id` | track ID |
| `quality` | `HI_RES_LOSSLESS`, `LOSSLESS`, `HIGH`, `LOW` |
| `adaptive` | `false` (Navidrums always requests a fixed quality) |
| `formats` | stream format, repeatable |

Quality maps onto `formats` as: `HI_RES_LOSSLESS` → `FLAC_HIRES`, `LOSSLESS` → `FLAC`, `HIGH` → `AACLC`,
`LOW` → `HEAACV1`.

The response is a JSON:API resource, **not** a manifest:

```json
{"version":"2.10","data":{"data":{"id":"1550546","type":"trackManifests","attributes":{
  "trackPresentation":"FULL",
  "uri":"https://im-cf.manifest.tidal.com/1/manifests/….mpd?Expires=…&Signature=…",
  "hash":"…","formats":["FLAC"],
  "trackAudioNormalizationData":{"replayGain":-5.83,"peakAmplitude":0.979767}}}}}
```

`attributes.uri` is a **time-limited signed URL** that must be fetched separately; it returns the actual
manifest (`application/dash+xml`). Only `manifestType=MPEG_DASH` is served, so lossless audio arrives as
FLAC inside a segmented MP4 and is remuxed to `.flac` after download.

`trackPresentation` is `FULL` or `PREVIEW`. A `PREVIEW` response also carries `previewReason` (e.g.
`FULL_REQUIRES_SUBSCRIPTION`). Navidrums treats a preview as a failure rather than saving a 30-second clip,
so the next configured instance gets its chance at the full track.

#### ISRC lookup — `GET /search/?i={isrc}`

Exact ISRC lookup, returning `data.items` in the same shape as `/search/?s=`. Used to map a track from
another provider onto this instance's track ID. Older instances without the route fall back to the
free-text `s=` search, where a result is only trusted when its ISRC matches exactly.

### Design Notes

- Metadata parsing is shared with the HiFi provider — same TIDAL-derived response shapes.
- `/search/?q=` (combined search) is **not** supported; use `s`, `a`, `al`, `p` or `i`.
- `/lyrics/` is not served by every instance; Navidrums falls back to LRCLIB (`LYRICS_FALLBACK_URL`).
- A rejected hi-res request is retried once at `LOSSLESS`, since most instances lack a hi-res tier.
- Requests are throttled to two per second per instance.

See `api-examples/monochrome-api/` for captured responses.
