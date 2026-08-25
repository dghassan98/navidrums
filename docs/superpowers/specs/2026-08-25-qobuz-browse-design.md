# Qobuz browse: genre tree, Discover home, label browsing

**Date:** 2026-08-25

## Goal

Turn the home page from a search box into a place to find music worth
downloading, using three Qobuz endpoints Navidrums does not currently call:
`album/getFeatured`, `genre/list`, and `label/get`.

Three features, built in this order because each depends on the last:

1. **Genre tree** — a reusable genre picker plus `/genre/{id}` pages
2. **Discover home** — configurable featured rows, genre-filterable
3. **Label browsing** — `/label/{id}`, reached from the album page

## Depends on

`2026-08-25-strip-to-qobuz-direct-design.md` must land first. This spec assumes
a single provider and refers to `ProviderManager.Provider()` throughout.

## Framing

Navidrums is a downloader with no link to a music library — downloaded files are
moved away by an external script. Browse pages are therefore a **funnel into the
queue**, not a view of a collection. Two consequences shape the design:

- Every album rendered must be queueable. `components/album_card.html` already
  carries a download button, so reusing it gives every new page the funnel for
  free.
- Nothing may check the filesystem to decide what is already owned. The script
  moves files, so the filesystem would lie. The `tracks` table is the record.

---

## Task 0: capture real API responses

**Do this first.** `api-examples/qobuz-api/` covers only search, album, track and
artist. The response shapes of `album/getFeatured`, `playlist/getFeatured`,
`genre/list` and `label/get` have not been observed. Writing DTOs against
assumptions would mean rewriting them.

A throwaway script authenticates with the configured credentials and saves:

| File | Request |
|------|---------|
| `featured-new-releases.json` | `album/getFeatured?type=new-releases&limit=20` |
| `featured-genre-filtered.json` | `album/getFeatured?type=new-releases&genre_id={id}&limit=20` |
| `featured-playlists.json` | `playlist/getFeatured?type=editor-picks&limit=20` |
| `genres.json` | `genre/list` |
| `genre-get.json` | `genre/get?genre_id={id}` |
| `label.json` | `label/get?label_id={id}&extra=albums&limit=20` |

Two questions must be answered before the DTOs are written:

1. **Does `genre/list` return the full tree, or only top-level genres?** If only
   top-level, children come from `genre/get?genre_id=&extra=subgenres` and
   `GetGenres` fans out one call per top-level genre — which is exactly why the
   genre tree gets a 24-hour cache.
2. **Which `type` values does `album/getFeatured` actually accept?** The set
   below is from the web player and may be incomplete or partly stale. Types
   that error are dropped from the Settings list rather than shipped broken.

Findings are recorded in `QOBUZ_API.md` alongside the existing endpoint
documentation.

---

## Catalog layer

### Provider interface

With one provider there is no capability to negotiate, so the browse methods go
directly on `Provider` rather than a separate optional interface. `mock.go`
gains the four methods.

```go
GetFeatured(ctx context.Context, kind, genreID string, limit, offset int) ([]domain.Album, error)
GetFeaturedPlaylists(ctx context.Context, genreID string, limit, offset int) ([]domain.Playlist, error)
GetGenres(ctx context.Context) ([]domain.Genre, error)
GetLabel(ctx context.Context, labelID string, limit, offset int) (*domain.Label, error)
```

An empty `genreID` means unfiltered and omits the parameter from the upstream
request entirely, rather than sending an empty value.

### New files

| File | Purpose |
|------|---------|
| `internal/catalog/qobuz_browse.go` | The four methods on `QobuzDirectProvider` |
| `internal/catalog/qobuz_browse_dto.go` | Response structs |
| `internal/catalog/qobuz_browse_convert.go` | `ToDomain()` mappings |

Separate files rather than appending to `qobuz_direct.go`, which is already 514
lines and would pass 800.

Album conversion reuses the existing `QobuzAlbumResponse.ToDomain()`. If
featured responses carry a trimmed album object, the DTO embeds the existing
struct rather than duplicating fields.

### Domain additions

```go
type Genre struct {
    ID       string   `json:"id"`
    Name     string   `json:"name"`
    Slug     string   `json:"slug,omitempty"`
    Children []Genre  `json:"children,omitempty"`
}

type Label struct {
    ID          string  `json:"id"`
    Name        string  `json:"name"`
    Description string  `json:"description,omitempty"`
    AlbumsCount int     `json:"albums_count,omitempty"`
    Albums      []Album `json:"albums,omitempty"`
}
```

`domain.Album` gains `LabelID string` and `GenreID string`. Both are populated
from the Qobuz album payload, which already carries `label.id` and `genre.id`.
Where empty, the album page renders label and genre as plain text as it does
today.

`domain.Album` also gains `Owned bool` with tag `json:"-"`. This is view state
on a domain struct, which is a deliberate compromise: the alternative is a
wrapper type threaded through `album_card.html`, which is shared by search
results, similar albums and recommendations, and would have to change all of
them. The `json:"-"` tag keeps it out of every serialised form.

### Caching

`CachedProvider` gains the four methods with per-method TTLs, since they age
very differently:

| Method | TTL | Reason |
|--------|-----|--------|
| `GetGenres` | 24h | The taxonomy is effectively static |
| `GetFeatured` | 1h | Editorial rows change at most daily |
| `GetFeaturedPlaylists` | 1h | Same |
| `GetLabel` | 1h | A back catalogue changes rarely |

Cache keys include every argument — `kind`, `genreID`, `limit`, `offset` — so a
genre-filtered row does not collide with an unfiltered one.

### Failure behaviour

A failed browse call renders that row or page section empty with a quiet inline
note. Never a 500, never a partial page. One dead row must not take down the
page — the entire reason rows load independently. Missing credentials surface
the existing `ErrQobuzCredentialsMissing` message, the same as search does.

---

## Ownership marking

### The seam

```go
// internal/app/ownership.go
type OwnershipIndex interface {
    OwnedAlbumIDs(ctx context.Context, albumIDs []string) (map[string]bool, error)
}
```

The only implementation for now is backed by the `tracks` table:

```sql
SELECT DISTINCT album_id FROM tracks WHERE album_id IN (?, ?, …)
```

`tracks.album_id` already exists and is indexed (`idx_tracks_album_id`), so this
is one indexed lookup **per row**, not per card.

Sub-project D (read-only Subsonic library index) adds a second implementation
and a composite that ORs the two. Nothing in the templates or handlers changes
when it does — that is the point of the seam. It matters here because the user
has a large pre-existing collection that Navidrums did not download, which the
`tracks` table cannot see.

### Rendering

`album_card.html` gains, guarded on `.Owned`: a check badge in the corner and a
muted cover (a CSS class, not a separate template). The download button stays
live so an album can be deliberately re-queued.

Every browse handler sets `Owned` before rendering. Search results and
recommendations are out of scope for this spec; they can adopt it later with no
further work.

---

## HTTP and UI

### Routes

| Route | Handler | Purpose |
|-------|---------|---------|
| `GET /htmx/discover/row/{kind}` | `DiscoverRowHTMX` | One featured row; `?genre_id=` optional |
| `GET /htmx/discover/playlists` | `DiscoverPlaylistsHTMX` | The curated-playlists row |
| `GET /genre/{id}` | `GenrePage` | Genre page with sub-genre chips |
| `GET /htmx/genre-picker` | `GenrePickerHTMX` | The shared picker fragment |
| `GET /label/{id}` | `LabelPage` | Label albums; `?page=`, `?sort=`, `?genre_id=` |

Settings routes go behind the existing `requireAdmin` group:

| Route | Handler |
|-------|---------|
| `GET /htmx/discover-rows` | `GetDiscoverRowsHTMX` |
| `POST /htmx/discover-rows` | `SetDiscoverRowsHTMX` |
| `POST /htmx/discover-rows/reset` | `ResetDiscoverRowsHTMX` |

### Home page

`index.html` keeps the search box and the existing library-seeded
recommendations panel unchanged. Below them, one section per enabled row, in the
configured order.

Each row is a placeholder that loads itself:

```html
<div hx-get="/htmx/discover/row/new-releases"
     hx-trigger="load"
     hx-include="[name='genre_id']"
     hx-swap="innerHTML">…</div>
```

This mirrors the existing `#results hx-get="/htmx/lucky" hx-trigger="load"`
pattern. The page paints immediately and a slow row cannot block the rest —
which matters, since the README targets low-end hardware and each row is one
upstream call.

The genre picker sits in the page header and carries `name="genre_id"`. Changing
it re-triggers every row. The selection lives in the query string only; it is
not persisted.

### New templates

| File | Purpose |
|------|---------|
| `components/discover_row.html` | Title, horizontally scrolling `album_card` list |
| `components/genre_picker.html` | Genre select built from the cached tree |
| `genre.html` | Genre page: sub-genre chips, then featured rows scoped to it |
| `label.html` | Label header, sort control, paginated album grid |

`genre.html` reuses `discover_row.html`; the genre page is the Discover rows
with `genre_id` pinned, which is why building the picker first makes the page
nearly free.

`label.html` reuses the existing `components/pagination.html`. Sort offers
newest-first (default) and oldest-first, applied to the album list.

### Album page

The Label row in `album.html` becomes a link to `/label/{{.Album.LabelID}}` when
`LabelID` is non-empty, plain text otherwise. Same treatment for Genre with
`/genre/{{.Album.GenreID}}`.

---

## Settings: configurable Discover rows

Stored under a new key `discover_rows` as a JSON array, ordered, following the
pattern of `genre_map` and `mood_list`:

```json
[
  {"kind": "new-releases",      "enabled": true},
  {"kind": "editor-picks",      "enabled": true},
  {"kind": "press-awards",      "enabled": true},
  {"kind": "ideal-discography", "enabled": true},
  {"kind": "most-streamed",     "enabled": false},
  {"kind": "best-sellers",      "enabled": false},
  {"kind": "qobuzissims",       "enabled": false},
  {"kind": "playlists",         "enabled": true}
]
```

`"playlists"` is not an `album/getFeatured` type; it maps to
`playlist/getFeatured` and is handled by its own route. The known-kinds list is
a Go constant, so an unrecognised `kind` in stored settings is ignored rather
than requested — that is what keeps a stale config from producing broken rows
after the Task 0 probe prunes the list.

Settings UI: a panel of checkboxes with drag-to-reorder and a Reset button,
matching the existing mood-list and language-list panels. Missing or unparseable
settings fall back to the defaults above.

---

## Testing

Following the existing `qobuz_direct_test.go` pattern — `httptest` servers
returning the JSON captured in Task 0.

**Catalog:** each of the four methods against canned responses; genre tree
assembly including the fan-out path if `genre/list` proves shallow; cache TTLs
observed per method; a failing upstream returning an error rather than a panic.

**Ownership:** `OwnedAlbumIDs` with a seeded `tracks` table — all owned, none
owned, partial, and an empty input list (which must not emit `IN ()`).

**Handlers:** each route renders with a stubbed provider via the existing mock;
a provider error renders the empty-row note and HTTP 200, not a 500; the genre
filter reaches the provider call; label pagination and sort produce the right
offsets.

**Settings:** round-trip of `discover_rows`, reset to defaults, an unknown
`kind` ignored, malformed JSON falling back to defaults.

---

## Risks and unknowns

**Response shapes are unverified.** Task 0 exists to close this. If
`album/getFeatured` turns out not to accept `genre_id`, the genre filter degrades
to filtering the returned page client-side, and that limitation is documented
rather than worked around.

**Row count versus rate limiting.** Eight enabled rows means eight upstream
calls on first paint. The existing API throttling applies, and the 1-hour cache
means only the first visitor after expiry pays. If the probe shows Qobuz
rate-limiting this pattern, rows switch from `hx-trigger="load"` to
`hx-trigger="revealed"` so below-the-fold rows load on scroll.

**Genre IDs on albums.** The design assumes `genre.id` is present on album
payloads. `api-examples/qobuz-api/album.json` confirms it there; featured-row
album objects may be trimmed. If absent, the genre link is simply not rendered
for those cards.

---

## Out of scope

- Ownership marking on search results and recommendations — the seam supports
  it; not wired here.
- A `/labels` index and label following — considered and rejected. Most arrivals
  come from an album page, and an index of thousands of labels has no useful
  ordering.
- Sub-projects C (artist page grouping), A (download pipeline) and D (Subsonic
  index), each with its own spec.
