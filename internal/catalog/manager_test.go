package catalog

import (
	"testing"
	"time"
)

// TestProviderDoesNotDeadlock guards a real regression: Provider() used to
// build the provider while holding the write lock, and building reads the
// effective credentials, which takes the read lock. sync.RWMutex is not
// reentrant, so the first catalog call of the process hung forever and every
// page that touched the catalog stopped responding.
func TestProviderDoesNotDeadlock(t *testing.T) {
	m := NewProviderManager(nil, nil, time.Minute, nil)

	done := make(chan Provider, 1)
	go func() { done <- m.Provider() }()

	select {
	case p := <-done:
		if p == nil {
			t.Fatal("Provider() returned nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Provider() deadlocked")
	}
}

// TestProviderIsCached proves the second call reuses the first build, which is
// what makes the lock dance worth having at all.
func TestProviderIsCached(t *testing.T) {
	m := NewProviderManager(nil, nil, time.Minute, nil)

	first := m.Provider()
	second := m.Provider()

	if first != second {
		t.Error("Provider() rebuilt instead of reusing the cached provider")
	}
}

// TestInvalidateAllCachesRebuilds covers credentials changing in Settings:
// the next call must pick them up without a restart.
func TestInvalidateAllCachesRebuilds(t *testing.T) {
	m := NewProviderManager(nil, nil, time.Minute, nil)

	first := m.Provider()
	m.InvalidateAllCaches()
	second := m.Provider()

	if first == second {
		t.Error("InvalidateAllCaches did not force a rebuild")
	}
}

// TestSetQobuzCredentialsDoesNotDeadlock covers the same reentrancy trap from
// the other direction: setting credentials takes the write lock too.
func TestSetQobuzCredentialsDoesNotDeadlock(t *testing.T) {
	m := NewProviderManager(nil, nil, time.Minute, nil)
	m.Provider()

	done := make(chan struct{})
	go func() {
		m.SetQobuzCredentials(QobuzCredentials{AppID: "123456789"})
		m.Provider()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SetQobuzCredentials followed by Provider() deadlocked")
	}
}
