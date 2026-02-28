package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"sync"
	"time"
)

// ========================================================================
// In-Memory TTL Cache for Dashboard API
//
// Two modes of operation:
//   1. Lazy Cache (original)   — CacheTTL = 5 min, set on first user request
//   2. Warm Cache (background) — WarmCacheTTL = 20 min, set by cache_warmer.go
//
// The warmer uses a longer TTL (20 min) than the warm interval (15 min)
// to ensure overlap: old cache stays valid while the next warm cycle runs.
// If a warm cycle fails, stale data remains available until the next success.
//
// Cache key = sha256(zone|startDate|endDate)[:16]
// ========================================================================

// CacheTTL is the TTL for lazy-loaded cache entries (user-triggered).
var CacheTTL = 5 * time.Minute

// WarmCacheTTL is the TTL for warmer-populated cache entries.
// Set longer than WarmInterval to provide overlap/fallback.
var WarmCacheTTL = 20 * time.Minute

type cacheEntry struct {
	Data      json.RawMessage
	ExpiresAt time.Time
	Source    string // "lazy" or "warm" — for logging/monitoring
}

var (
	dashCache   = make(map[string]cacheEntry)
	dashCacheMu sync.RWMutex
)

// CacheKey generates a deterministic cache key from query parameters.
func CacheKey(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte("|"))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// GetCachedDashboard returns cached dashboard data if available and not expired.
// Returns nil if cache miss or expired.
func GetCachedDashboard(key string) json.RawMessage {
	dashCacheMu.RLock()
	defer dashCacheMu.RUnlock()

	entry, ok := dashCache[key]
	if !ok {
		return nil
	}
	if time.Now().After(entry.ExpiresAt) {
		return nil
	}
	log.Printf("[Cache] HIT key=%s source=%s expires_in=%v", key, entry.Source, time.Until(entry.ExpiresAt).Round(time.Second))
	return entry.Data
}

// SetCachedDashboard stores dashboard data from a lazy (user-triggered) request.
// Uses the default CacheTTL.
func SetCachedDashboard(key string, data interface{}) {
	raw, err := json.Marshal(data)
	if err != nil {
		log.Printf("[Cache] marshal error: %v", err)
		return
	}
	SetCachedDashboardRaw(key, raw, CacheTTL)
}

// SetCachedDashboardRaw stores pre-serialised JSON with a custom TTL.
// Used by both lazy cache and the background warmer.
func SetCachedDashboardRaw(key string, raw json.RawMessage, ttl time.Duration) {
	dashCacheMu.Lock()
	defer dashCacheMu.Unlock()

	source := "lazy"
	if ttl == WarmCacheTTL {
		source = "warm"
	}

	dashCache[key] = cacheEntry{
		Data:      raw,
		ExpiresAt: time.Now().Add(ttl),
		Source:    source,
	}
	log.Printf("[Cache] SET key=%s source=%s ttl=%v", key, source, ttl)
}

// InvalidateDashboardCache clears all cached dashboard data.
// Call this when underlying data changes (e.g. after data import).
func InvalidateDashboardCache() {
	dashCacheMu.Lock()
	defer dashCacheMu.Unlock()
	count := len(dashCache)
	dashCache = make(map[string]cacheEntry)
	log.Printf("[Cache] INVALIDATED (%d entries removed)", count)
}

// GetCacheStats returns monitoring info about current cache state.
func GetCacheStats() map[string]interface{} {
	dashCacheMu.RLock()
	defer dashCacheMu.RUnlock()

	now := time.Now()
	warmCount := 0
	lazyCount := 0
	expiredCount := 0
	entries := []map[string]interface{}{}

	for k, v := range dashCache {
		if now.After(v.ExpiresAt) {
			expiredCount++
			continue
		}
		if v.Source == "warm" {
			warmCount++
		} else {
			lazyCount++
		}
		entries = append(entries, map[string]interface{}{
			"key":        k,
			"source":     v.Source,
			"expires_in": time.Until(v.ExpiresAt).Round(time.Second).String(),
			"size_bytes": len(v.Data),
		})
	}

	return map[string]interface{}{
		"total_entries":   len(dashCache),
		"warm_entries":    warmCount,
		"lazy_entries":    lazyCount,
		"expired_entries": expiredCount,
		"entries":         entries,
	}
}

// CleanExpiredCache removes expired entries. Run periodically.
func CleanExpiredCache() {
	dashCacheMu.Lock()
	defer dashCacheMu.Unlock()
	now := time.Now()
	removed := 0
	for k, v := range dashCache {
		if now.After(v.ExpiresAt) {
			delete(dashCache, k)
			removed++
		}
	}
	if removed > 0 {
		log.Printf("[Cache] Cleaned %d expired entries", removed)
	}
}

// StartCacheCleaner runs a background goroutine to clean expired entries every minute.
func StartCacheCleaner() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			CleanExpiredCache()
		}
	}()
}