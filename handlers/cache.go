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
// Reduces repeated MongoDB aggregation queries (10+ seconds → instant).
// Cache key is derived from query parameters (zone, startDate, endDate).
// Default TTL: 5 minutes. Configurable via CacheTTL.
// ========================================================================

// CacheTTL is the time-to-live for cached dashboard responses.
var CacheTTL = 5 * time.Minute

type cacheEntry struct {
	Data      json.RawMessage
	ExpiresAt time.Time
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
	log.Printf("Dashboard cache HIT: key=%s", key)
	return entry.Data
}

// SetCachedDashboard stores dashboard data in cache with TTL.
func SetCachedDashboard(key string, data interface{}) {
	raw, err := json.Marshal(data)
	if err != nil {
		log.Printf("Cache marshal error: %v", err)
		return
	}

	dashCacheMu.Lock()
	defer dashCacheMu.Unlock()

	dashCache[key] = cacheEntry{
		Data:      raw,
		ExpiresAt: time.Now().Add(CacheTTL),
	}
	log.Printf("Dashboard cache SET: key=%s, ttl=%v", key, CacheTTL)
}

// InvalidateDashboardCache clears all cached dashboard data.
// Call this when data is known to have changed.
func InvalidateDashboardCache() {
	dashCacheMu.Lock()
	defer dashCacheMu.Unlock()
	dashCache = make(map[string]cacheEntry)
	log.Println("Dashboard cache INVALIDATED")
}

// CleanExpiredCache removes expired entries. Run periodically.
func CleanExpiredCache() {
	dashCacheMu.Lock()
	defer dashCacheMu.Unlock()
	now := time.Now()
	for k, v := range dashCache {
		if now.After(v.ExpiresAt) {
			delete(dashCache, k)
		}
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