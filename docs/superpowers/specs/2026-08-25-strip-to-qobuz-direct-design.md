# Strip to qobuz-direct

**Date:** 2026-08-25

## Goal

Remove every catalog provider except `qobuz-direct`. Navidrums becomes a
single-provider downloader: the Monochrome, HiFi (Tidal proxy) and Qobuz-proxy
implementations, the multi-provider selection machinery, and the fallback
chains all go away.

This lands before any new feature work. Building the browse features
(see `2026-08-25-qobuz-browse-design.md`) on top of the multi-provider
abstraction and then removing that abstraction would mean wiring them twice.

## Non-goals

- No behaviour change to downloading, tagging, queueing or the downloads
  browser beyond what removal forces.
- No change to the Qobuz credential model, the status probe, or request signing.
- No rewrite of the dated design documents under `.opencode/plans/` and
  `docs/superpowers/`. They record decisions that were true when made;
  rewriting them turns a history into a lie.

---

## Deleted outright

### Go files (13)

| File | Why |
|------|-----|
| `internal/catalog/hifi.go` | HiFi provider |
| `internal/catalog/monochrome.go` | Monochrome provider |
| `internal/catalog/monochrome_dto.go` | Monochrome DTOs |
| `internal/catalog/monochrome_test.go` | Monochrome tests |
| `internal/catalog/dto.go` | HiFi/Tidal DTOs — generic name, but bound to `*HifiProvider` |
| `internal/catalog/convert.go` | Every method is `func (r API…) ToDomain(p *HifiProvider)` |
| `internal/catalog/convert_test.go` | Tests for the above |
| `internal/catalog/search.go` | `func (p *HifiProvider) Search(...)` |
| `internal/catalog/search_test.go` | Tests for the above |
| `internal/catalog/cross_provider.go` | Cross-provider-type fallback |
| `internal/catalog/fallback.go` | Multi-URL fallback within a provider type |
| `internal/catalog/qobuz.go` | Qobuz **proxy** provider (not qobuz-direct) |
| `internal/catalog/qobuz_test.go` | Proxy tests |

`internal/catalog/resolve_test.go` covers cross-provider ISRC resolution and is
expected to go with `cross_provider.go`. Confirm at implementation time rather
than assuming; if any case exercises qobuz-direct's own `resolveTrackID`, keep
that case and move it to `qobuz_direct_test.go`.

### Documentation (2)

- `HIFI_API.md`
- `MONOCHROME_API.md`

---

## Pruned surgically

These files look deletable but are shared. Removing them breaks qobuz-direct.

### `internal/catalog/qobuz_dto.go` and `qobuz_convert.go`

The proxy and the official API return **the same payload objects**; the proxy
merely wrapped them in `{success, data}`. `qobuz_direct.go` depends on
`QobuzSearchData`, `QobuzAlbumResponse`, `QobuzTrackResponse`,
`QobuzArtistPageResponse` and their converters.

Remove only the six envelope structs — those declaring `Success bool` with a
`Data` field. Every payload DTO and every `ToDomain()` stays.

### `internal/catalog/types.go`

- Remove `multiSegmentReader` (DASH segment fetching, used only by `hifi.go`).
- Keep `apiStatusError`, `readErrorBody`, `statusCodeOf`, `multiError`,
  `joinErrors`, `formatID`, `FlexCover` unless the compiler proves an orphan.

### `internal/constants/constants.go`

Remove `MonochromeDefaultURL`, `MonochromeDefaultName`, `QobuzDirectDefaultName`,
`TidalImageExt`, `MimeTypeBTS`, `MimeTypeDashXML`. Keep `QobuzDirectDefaultURL` —
`manager.go` and migration 17 both need it live. Check
`ImageSizeSmall/Medium/Large` for surviving callers; remove if orphaned.

**This requires freezing migration 16 first — see below.** Migration 16 is the
only remaining reference to the three constants being deleted, so the deletion
does not compile until that dependency is broken.

### `internal/http/imageproxy.go`

`allowedImageHosts` drops `resources.tidal.com` and `static.tidal.com`.
`static.qobuz.com` stays. Update `imageproxy_test.go` accordingly.

---

## Structural collapse

### `internal/catalog/provider.go`

Keep the `Provider` interface. It has one implementation now, but it is the
seam `mock.go` uses and every handler test depends on it.

Remove `ProviderType`, `ProviderTypes`, `DefaultProviderType`,
`IsValidProviderType`, and `fallbackProviderTypes`.

### `internal/catalog/manager.go`

`ProviderManager` loses `chains map[ProviderType]*CachedProvider`, the
per-operation accessors, and the cross-provider machinery.

Before:

```go
func (m *ProviderManager) GetProvider(pt ProviderType) Provider
func (m *ProviderManager) GetMetadataProvider() Provider
func (m *ProviderManager) GetDownloadProvider() Provider
func (m *ProviderManager) GetStreamingProvider() Provider
func (m *ProviderManager) GetProvidersByType(t string) []CustomProvider
func (m *ProviderManager) getCrossProvider(primary ProviderType) Provider
```

After:

```go
// Provider returns the cached qobuz-direct provider, building it on first use.
func (m *ProviderManager) Provider() Provider
func (m *ProviderManager) InvalidateAllCaches()
func (m *ProviderManager) SetQobuzCredentials(creds QobuzCredentials)
func (m *ProviderManager) QobuzCredentials() QobuzCredentials
func (m *ProviderManager) CheckQobuzCredentials(ctx context.Context) *QobuzStatus
```

`CustomProvider` and the `store.ProvidersRepo` dependency are removed.
`CachedProvider` stays and now wraps the single provider directly.

`qobuzBaseURL()` no longer reads the `providers` table. It reads the new
`qobuz_base_url` setting, falling back to `constants.QobuzDirectDefaultURL`.

### Call sites (20, all non-test)

`GetMetadataProvider()`, `GetDownloadProvider()` and `GetStreamingProvider()`
all become `Provider()`:

- `internal/app/downloader.go` — 1
- `internal/app/enricher.go` — 4
- `internal/downloader/handlers.go` — 4
- `internal/http/routes.go` — 10
- `internal/http/stream.go` — 1

### `internal/store/`

Delete `providers.go` (`ProvidersRepo`) and `providers_test.go`. Remove the
`providers` table and its index from `schema.go`. Remove the settings-key
constants `SettingActiveProvider`, `SettingActiveMetadataProvider`,
`SettingActiveDownloadProvider`, `SettingActiveStreamingProvider`,
`SettingCustomProviders`. Add `SettingQobuzBaseURL = "qobuz_base_url"`.

---

## Database migration (version 17)

Appended to `migrations` in `internal/store/db.go`, following the existing
struct form.

### First: freeze migration 16

Migration 16 references **six** symbols this change deletes, and is the only
remaining code that references any of them:

| Line | Symbol | Frozen literal |
|------|--------|----------------|
| 461 | `constants.MonochromeDefaultURL` | `https://lol.samidy.workers.dev` |
| 461 | `constants.MonochromeDefaultName` | `Monochrome (default)` |
| 473 | `constants.QobuzDirectDefaultName` | `Qobuz (my subscription)` |
| 486 | `SettingActiveMetadataProvider` | `active_metadata_provider` |
| 487 | `SettingActiveDownloadProvider` | `active_download_provider` |
| 488 | `SettingActiveStreamingProvider` | `active_streaming_provider` |

Replace each with the literal string it holds today:

```go
`INSERT OR IGNORE INTO providers (type, url, name, position)
 VALUES ('monochrome', 'https://lol.samidy.workers.dev', 'Monochrome (default)', 0)`
```

...and likewise `'Qobuz (my subscription)'` for the qobuz-direct row, and a
plain `[]string{"active_metadata_provider", "active_download_provider",
"active_streaming_provider"}` for the settings loop.

`constants.QobuzDirectDefaultURL` (line 473) survives as a live constant, but
freeze it here too for consistency — a migration should not track a value the
application is free to change. After this, `db.go` may no longer import
`constants`; let the compiler decide.

This changes the migration's **code** but not its **effect** — the SQL it
produces is byte-identical, so installs mid-upgrade behave exactly as before.
That distinction is what makes it safe: an applied migration's *result* is
history and must not change, but a frozen literal is a more honest expression
of that history than a reference to a constant the application keeps evolving.
Migration 15 needs no change.

Migration 16 also references the `providers` table, which migration 17 drops.
That is fine — 16 runs before 17 on any install that needs it, and neither runs
again afterwards.

```go
{
    version:     17,
    description: "Remove multi-provider support; qobuz-direct only",
    up: func(tx *sqlx.Tx) error {
        // Carry a configured qobuz-direct URL over to the new setting
        // before the table goes, so a custom endpoint is not silently lost.
        var url string
        err := tx.QueryRow(
            `SELECT url FROM providers WHERE type = 'qobuz-direct'
             ORDER BY position LIMIT 1`).Scan(&url)
        // Literal, not constants.QobuzDirectDefaultURL: a migration must not
        // track a value the application is free to change later.
        if err == nil && url != "" && url != "https://www.qobuz.com/api.json/0.2" {
            if _, err := tx.Exec(
                `INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)`,
                SettingQobuzBaseURL, url); err != nil {
                return err
            }
        }

        if _, err := tx.Exec(`DROP TABLE IF EXISTS providers`); err != nil {
            return err
        }

        if _, err := tx.Exec(
            `DELETE FROM settings WHERE key IN (?, ?, ?, ?, ?)`,
            "active_provider", "active_metadata_provider",
            "active_download_provider", "active_streaming_provider",
            "custom_providers"); err != nil {
            return err
        }

        _, err = tx.Exec(
            `UPDATE jobs SET type = 'sync_provider' WHERE type = 'sync_hifi'`)
        return err
    },
},
```

`sql.ErrNoRows` from the first query is expected on a fresh install and must
not fail the migration.

---

## Renames

| Before | After | Touches |
|--------|-------|---------|
| `domain.JobTypeSyncHiFi` = `"sync_hifi"` | `domain.JobTypeSyncProvider` = `"sync_provider"` | `models.go`, `handlers.go`, `worker.go`, migration 17 |
| `POST /htmx/track/{id}/enrich-hifi` | `POST /htmx/track/{id}/enrich-provider` | `handler.go`, `routes.go`, `components/track_form.html` |
| `POST /htmx/downloads/enrich-hifi` | `POST /htmx/downloads/enrich-provider` | `handler.go`, `routes.go`, `downloads.html` |
| `Handler.EnrichHiFiHTMX` | `Handler.EnrichProviderHTMX` | `routes.go` |
| `Handler.BulkEnrichHiFiHTMX` | `Handler.BulkEnrichProviderHTMX` | `routes.go` |

These htmx endpoints have no external consumers; `API.md` documents them and
is updated in the same change.

---

## Settings page

`web/templates/settings.html` loses:

- The **Monochrome Providers**, **HIFI Providers** and **QOBUZ Providers**
  panels (add/remove/reorder forms and their lists).
- The **QOBUZ DIRECT Providers** panel, replaced by a single optional
  "API base URL" field in the existing Qobuz credentials block, placeholder
  `https://www.qobuz.com/api.json/0.2`.
- The **Default APIs** section — all three per-operation dropdowns.

It keeps the Qobuz credentials block, the `/htmx/qobuz-status` probe, and
every non-provider setting.

Handlers removed from `internal/http/handler.go` and `routes.go`:
`GetProvidersHTMX`, `ReorderProvidersHTMX`, `AddProviderHTMX`,
`RemoveProviderHTMX`, `GetDefaultAPIsHTMX`, `SetDefaultAPIHTMX`, and their
routes. `internal/http/qobuz_settings.go` gains the base-URL get/set pair.

---

## Documentation

Rewritten, not patched — each describes multi-provider as a headline feature:

| File | Change |
|------|--------|
| `README.md` | Drop the Multi-Provider Architecture sections; describe a Qobuz-only downloader |
| `ARCHITECTURE.md` | Providers layer becomes one adapter; remove chain/fallback description |
| `CONFIGURATION.md` | Remove provider selection and instance config; document `qobuz_base_url` |
| `DOMAIN.md` | Remove provider-type field notes |
| `API.md` | Remove provider routes; rename the two enrich routes |
| `AGENTS.md` | Remove multi-provider rules |
| `.env.sample` | Remove HiFi/Monochrome variables |
| `QOBUZ_API.md` | Delete the "Qobuz Proxy (`qobuz`)" section; keep qobuz-direct |

`api-examples/hifi-api/` and `api-examples/monochrome-api/` are deleted.

---

## Testing and verification

1. `go build ./...` — the compiler finds every reference to a deleted symbol.
2. `golangci-lint run` — the configured unused-code checks find orphans the
   compiler tolerates. This is the step that proves the removal is complete;
   deletion is verifiable in a way most refactors are not.
3. `go test ./...` — green, minus deleted provider tests.
4. Migration test in `internal/store/migrations_test.go`: a DB seeded with a
   `providers` table, a custom qobuz-direct URL, the five settings keys and a
   `sync_hifi` job migrates to a DB with no `providers` table, the URL in
   `qobuz_base_url`, no stale settings, and a `sync_provider` job.
5. Manual smoke: search, album page, artist page, queue a download, settings
   page renders with no provider panels.

Existing tests that construct providers by type, or assert fallback
behaviour, are deleted along with the code they cover — not rewritten to pass.

---

## Risks

**An install running Monochrome loses its provider on upgrade.** Migration 16
defaulted fresh installs to `monochrome`, so an install that never configured
Qobuz will have no working provider until `QOBUZ_*` credentials are set. This
is inherent to the decision, not a defect. The Qobuz status panel already
reports missing credentials clearly, and `ErrQobuzCredentialsMissing` surfaces
on the first search. Call it out in the release notes.

**Scope discipline.** The temptation during a large deletion is to tidy
adjacent code. Stay to the inventory above; unrelated refactoring belongs in
its own change.

---

## Out of scope

Deferred to their own specs, in order:

- **B** — Qobuz browse: genre tree, Discover home, label browsing
- **C** — artist page release-type grouping and sort
- **A** — download pipeline: digital booklets, classical metadata, hi-res floor
- **D** — read-only Subsonic library index
