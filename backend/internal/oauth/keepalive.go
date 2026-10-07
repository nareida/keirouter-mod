package oauth

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/mydisha/keirouter/backend/internal/store"
)

const (
	// DefaultKeepAliveInterval is how often the background refresher checks
	// for near-expiry OAuth tokens.
	DefaultKeepAliveInterval = 5 * time.Minute
	// BackgroundRefreshLead moves refresh work out of the request path. It is
	// deliberately wider than TokenManager's request-time refreshSkew.
	BackgroundRefreshLead = 30 * time.Minute
	// DefaultKeepAliveMaxParallel bounds simultaneous token endpoint calls.
	DefaultKeepAliveMaxParallel = 4
)

// KeepAlive runs a background loop that proactively refreshes near-expiry
// OAuth access tokens. It prevents request-time latency from just-in-time
// refresh and detects expired refresh tokens early so the dashboard can show
// a "Reconnect" prompt.
type KeepAlive struct {
	interval    time.Duration
	tokenMgr    *TokenManager
	accounts    *store.AccountRepo
	tenantID    string
	log         *slog.Logger
	maxParallel int
}

// NewKeepAlive builds a KeepAlive.
func NewKeepAlive(tm *TokenManager, accounts *store.AccountRepo, tenantID string, log *slog.Logger) *KeepAlive {
	return &KeepAlive{
		interval:    DefaultKeepAliveInterval,
		tokenMgr:    tm,
		accounts:    accounts,
		tenantID:    tenantID,
		log:         log,
		maxParallel: DefaultKeepAliveMaxParallel,
	}
}

// SetMaxParallel overrides the background refresh concurrency bound.
func (k *KeepAlive) SetMaxParallel(n int) {
	if n > 0 {
		k.maxParallel = n
	}
}

// SetInterval overrides the default check interval.
func (k *KeepAlive) SetInterval(d time.Duration) {
	if d > 0 {
		k.interval = d
	}
}

// Run starts the keepalive loop. It blocks until ctx is cancelled. Callers
// should launch it as a goroutine tied to the application context.
func (k *KeepAlive) Run(ctx context.Context) {
	k.log.Info("oauth keepalive started", "interval", k.interval)

	// Run once immediately on startup to catch stale tokens early.
	k.refreshAll(ctx)

	ticker := time.NewTicker(k.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			k.log.Info("oauth keepalive stopped")
			return
		case <-ticker.C:
			k.refreshAll(ctx)
		}
	}
}

// refreshAll lists all OAuth accounts for the tenant and refreshes those that
// are near expiry. Failures are logged but do not stop the loop.
func (k *KeepAlive) refreshAll(ctx context.Context) {
	accs, err := k.accounts.ListByTenant(ctx, k.tenantID)
	if err != nil {
		k.log.Error("oauth keepalive: list accounts", "err", err)
		return
	}

	refreshed, skipped, failed, reconnect := k.refreshAccounts(ctx, accs)

	if refreshed > 0 || failed > 0 || reconnect > 0 {
		k.log.Info("oauth keepalive pass complete",
			"refreshed", refreshed,
			"skipped", skipped,
			"failed", failed,
			"needs_reconnect", reconnect,
		)
	}
}

func (k *KeepAlive) refreshAccounts(ctx context.Context, accs []store.Account) (refreshed, skipped, failed, reconnect int) {
	maxParallel := k.maxParallel
	if maxParallel <= 0 {
		maxParallel = DefaultKeepAliveMaxParallel
	}
	sem := make(chan struct{}, maxParallel)
	results := make(chan bool, len(accs))
	var wg sync.WaitGroup

	for _, acc := range accs {
		if acc.AuthKind != store.AuthOAuth {
			skipped++
			continue
		}
		if acc.Disabled {
			skipped++
			continue
		}
		// Already flagged for reconnection; skip until the user re-authenticates.
		if acc.NeedsReconnect {
			reconnect++
			continue
		}
		// Expiry-less imports cannot be proactively scheduled. Their request path
		// can still force a refresh after an upstream auth rejection.
		if acc.TokenExpiresAt == nil || time.Until(*acc.TokenExpiresAt) > BackgroundRefreshLead {
			skipped++
			continue
		}

		acc := acc
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results <- false
				return
			}

			_, err := k.tokenMgr.ForceRefresh(ctx, acc)
			if err != nil {
				k.log.Warn("oauth keepalive: refresh failed",
					"account", acc.ID,
					"provider", acc.Provider,
					"err", err,
				)
				results <- false
				return
			}
			k.log.Debug("oauth keepalive: refreshed", "account", acc.ID, "provider", acc.Provider)
			results <- true
		}()
	}
	wg.Wait()
	close(results)
	for ok := range results {
		if ok {
			refreshed++
		} else {
			failed++
		}
	}
	return refreshed, skipped, failed, reconnect
}
