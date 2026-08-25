package catalog

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/cesargomez89/navidrums/internal/constants"
	"github.com/cesargomez89/navidrums/internal/store"
)

type Logger interface {
	With(keyValues ...any) *slog.Logger
	Info(msg string, keyValues ...any)
	Error(msg string, keyValues ...any)
}

// ProviderManager owns the single Qobuz provider and its response cache.
// Credentials can change at runtime through Settings, so the built provider is
// discarded whenever they do and rebuilt on next use.
type ProviderManager struct {
	logger     Logger
	settings   *store.SettingsRepo
	cacheTTL   time.Duration
	db         *store.DB
	qobuzCreds QobuzCredentials

	provider *CachedProvider
	mu       sync.RWMutex
}

func NewProviderManager(db *store.DB, settings *store.SettingsRepo, cacheTTL time.Duration, logger Logger) *ProviderManager {
	return &ProviderManager{
		logger:   logger,
		settings: settings,
		cacheTTL: cacheTTL,
		db:       db,
	}
}

// SetQobuzCredentials supplies the credentials configured through the
// environment. They act as defaults: anything saved in Settings wins, so the
// UI can update credentials without a restart.
func (m *ProviderManager) SetQobuzCredentials(creds QobuzCredentials) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.qobuzCreds = creds
	m.provider = nil
}

// QobuzCredentials returns the effective credentials: values stored in
// Settings, each falling back to the environment when unset.
func (m *ProviderManager) QobuzCredentials() QobuzCredentials {
	m.mu.RLock()
	creds := m.qobuzCreds
	m.mu.RUnlock()

	if m.settings == nil {
		return creds
	}

	stored := func(key, fallback string) string {
		if val, err := m.settings.Get(key); err == nil && val != "" {
			return val
		}
		return fallback
	}

	creds.AppID = stored(store.SettingQobuzAppID, creds.AppID)
	creds.AppSecret = stored(store.SettingQobuzAppSecret, creds.AppSecret)
	creds.AuthToken = stored(store.SettingQobuzAuthToken, creds.AuthToken)
	creds.Email = stored(store.SettingQobuzEmail, creds.Email)
	creds.PasswordMD5 = stored(store.SettingQobuzPasswordMD5, creds.PasswordMD5)

	return creds
}

// CheckQobuzCredentials probes Qobuz with the effective credentials.
func (m *ProviderManager) CheckQobuzCredentials(ctx context.Context) *QobuzStatus {
	provider := NewQobuzDirectProvider(m.qobuzBaseURL(), m.QobuzCredentials())
	return provider.CheckCredentials(ctx)
}

// qobuzBaseURL returns the configured endpoint, or the official API when the
// setting is unset. Only an install that migrated from a custom qobuz-direct
// instance, or one that set it deliberately, has anything stored here.
func (m *ProviderManager) qobuzBaseURL() string {
	if m.settings == nil {
		return constants.QobuzDirectDefaultURL
	}
	if val, err := m.settings.Get(store.SettingQobuzBaseURL); err == nil && val != "" {
		return val
	}
	return constants.QobuzDirectDefaultURL
}

// Provider returns the cached Qobuz provider, building it on first use.
//
// The provider is built *before* the write lock is taken, not inside it:
// build reads the effective credentials, which takes the read lock, and
// sync.RWMutex is not reentrant — doing that while holding the write lock
// deadlocks the first catalog call the process makes. Two goroutines racing
// here may each build one; that is cheap, and only one is kept.
func (m *ProviderManager) Provider() Provider {
	m.mu.RLock()
	p := m.provider
	m.mu.RUnlock()
	if p != nil {
		return p
	}

	built := m.build()

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.provider == nil {
		m.provider = built
	}
	return m.provider
}

func (m *ProviderManager) build() *CachedProvider {
	qobuz := NewQobuzDirectProvider(m.qobuzBaseURL(), m.QobuzCredentials())
	var cacheStore *storeCache
	if m.db != nil {
		cacheStore = &storeCache{store: m.db}
	}
	return NewCachedProvider(qobuz, cacheStore, m.cacheTTL)
}

// InvalidateAllCaches drops the built provider so the next call picks up new
// credentials or a new base URL.
func (m *ProviderManager) InvalidateAllCaches() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provider = nil
}

// NewProviderManagerWithProvider wraps an already-built provider. It exists so
// handlers can be tested against a stub without a database or credentials.
func NewProviderManagerWithProvider(p Provider) *ProviderManager {
	return &ProviderManager{provider: NewCachedProvider(p, noopCache{}, 0)}
}

// noopCache satisfies Cache without storing anything, so an injected provider
// is called on every request and tests see exactly what they stubbed.
type noopCache struct{}

func (noopCache) GetCache(string) ([]byte, error)              { return nil, nil }
func (noopCache) SetCache(string, []byte, time.Duration) error { return nil }
func (noopCache) ClearCache() error                            { return nil }
