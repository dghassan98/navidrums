package catalog

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/cesargomez89/navidrums/internal/store"
)

type Logger interface {
	With(keyValues ...any) *slog.Logger
	Info(msg string, keyValues ...any)
	Error(msg string, keyValues ...any)
}

type ProviderManager struct {
	logger     Logger
	providers  *store.ProvidersRepo
	settings   *store.SettingsRepo
	cacheTTL   time.Duration
	db         *store.DB
	qobuzCreds QobuzCredentials

	chains map[ProviderType]*CachedProvider
	mu     sync.RWMutex
}

func NewProviderManager(db *store.DB, settings *store.SettingsRepo, cacheTTL time.Duration, logger Logger) *ProviderManager {
	var providersRepo *store.ProvidersRepo
	if db != nil {
		providersRepo = store.NewProvidersRepo(db)
	}

	return &ProviderManager{
		logger:    logger,
		providers: providersRepo,
		settings:  settings,
		cacheTTL:  cacheTTL,
		db:        db,
	}
}

// SetQobuzCredentials supplies the credentials configured through the
// environment. They act as defaults: anything saved in Settings wins, so the
// UI can update credentials without a restart.
func (m *ProviderManager) SetQobuzCredentials(creds QobuzCredentials) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.qobuzCreds = creds
	m.chains = nil
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

// qobuzBaseURL returns the first configured qobuz-direct endpoint, or "" to let
// the provider fall back to the official API.
func (m *ProviderManager) qobuzBaseURL() string {
	if m.providers == nil {
		return ""
	}
	records, err := m.providers.ListByType(string(ProviderTypeQobuzDirect))
	if err != nil || len(records) == 0 {
		return ""
	}
	return records[0].URL
}

func (m *ProviderManager) readSetting(key string) ProviderType {
	if m.settings == nil {
		return DefaultProviderType
	}
	val, err := m.settings.Get(key)
	if err != nil || val == "" {
		return DefaultProviderType
	}
	if !IsValidProviderType(val) {
		return DefaultProviderType
	}
	return ProviderType(val)
}

func (m *ProviderManager) buildChain(pt ProviderType) *CachedProvider {
	fb := &FallbackProvider{manager: m, providerType: pt}
	var cacheStore *storeCache
	if m.db != nil {
		cacheStore = &storeCache{store: m.db}
	}
	return NewCachedProvider(fb, cacheStore, m.cacheTTL, pt)
}

func (m *ProviderManager) GetProvider(pt ProviderType) Provider {
	m.mu.RLock()
	chain := m.chains[pt]
	m.mu.RUnlock()

	if chain != nil {
		return chain
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.chains == nil {
		m.chains = make(map[ProviderType]*CachedProvider)
	}
	if m.chains[pt] == nil {
		m.chains[pt] = m.buildChain(pt)
	}
	return m.chains[pt]
}

func (m *ProviderManager) GetMetadataProvider() Provider {
	return m.GetProvider(m.readSetting(store.SettingActiveMetadataProvider))
}

func (m *ProviderManager) GetDownloadProvider() Provider {
	primary := m.readSetting(store.SettingActiveDownloadProvider)
	return m.getCrossProvider(primary)
}

func (m *ProviderManager) getCrossProvider(primary ProviderType) Provider {
	chain := m.GetProvider(primary)

	var fallbacks []Provider
	for _, pt := range fallbackProviderTypes(primary) {
		if m.hasProvidersOfType(pt) {
			fallbacks = append(fallbacks, m.GetProvider(pt))
		}
	}

	if len(fallbacks) == 0 {
		return chain
	}

	return &crossProviderFallback{primary: chain, fallbacks: fallbacks}
}

func (m *ProviderManager) GetStreamingProvider() Provider {
	primary := m.readSetting(store.SettingActiveStreamingProvider)
	return m.getCrossProvider(primary)
}

func (m *ProviderManager) InvalidateAllCaches() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chains = nil
}

type CustomProvider struct {
	ID   int64  `json:"id,omitempty"`
	Name string `json:"name"`
	URL  string `json:"url"`
	Type string `json:"type"`
}

func (m *ProviderManager) GetProvidersByType(providerType string) []CustomProvider {
	if m.providers == nil {
		return nil
	}
	providers, err := m.providers.ListByType(providerType)
	if err != nil {
		return nil
	}
	result := make([]CustomProvider, len(providers))
	for i, p := range providers {
		result[i] = CustomProvider{
			ID:   p.ID,
			Name: p.Name,
			URL:  p.URL,
			Type: p.Type,
		}
	}
	return result
}
