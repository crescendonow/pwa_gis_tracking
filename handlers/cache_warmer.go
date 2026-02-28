package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"sync"
	"time"

	"pwa_gis_tracking/services"
)

// ========================================================================
// Background Cache Warmer
//
// แทนที่จะรอให้ user คนแรกมา request แล้วค่อยคำนวณ (Lazy Loading)
// ตัว Warmer จะ "คำนวณเตรียมไว้ล่วงหน้า" (Proactive Cache) ทุก 15 นาที
//
// ข้อดี:
//   - ทุก request ดึงจาก cache ได้ทันที (< 0.1 วินาที)
//   - ไม่มี cold-start penalty สำหรับ user คนแรก
//   - Stale data fallback: ถ้า warm รอบใหม่ fail จะยังใช้ cache เดิมได้
//
// Flow:
//   1. Startup → warm ทันที (ไม่ต้องรอ 15 นาทีแรก)
//   2. ทุก 15 นาที → warm ใหม่ทั้งหมด
//   3. warm = คำนวณ dashboard สำหรับ "ทุกเขต" + "แต่ละเขต" + date combos
// ========================================================================

// WarmInterval is the interval between cache warming cycles.
// Default: 15 minutes. Configurable for testing.
var WarmInterval = 15 * time.Minute

// warmerRunning guards against multiple warmer goroutines
var warmerRunning bool
var warmerMu sync.Mutex

// StartCacheWarmer starts the background cache warming goroutine.
// It warms the cache immediately on startup, then repeats every WarmInterval.
// Safe to call multiple times — only one warmer will run.
//
// Usage in main.go:
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//	handlers.StartCacheWarmer(ctx)
func StartCacheWarmer(ctx context.Context) {
	warmerMu.Lock()
	if warmerRunning {
		warmerMu.Unlock()
		log.Println("[CacheWarmer] Already running, skipping duplicate start")
		return
	}
	warmerRunning = true
	warmerMu.Unlock()

	log.Printf("[CacheWarmer] Starting background cache warmer (interval: %v)", WarmInterval)

	// Warm immediately on startup (don't wait for the first tick)
	go func() {
		log.Println("[CacheWarmer] 🔥 Initial warming on startup...")
		warmAllDashboards()

		ticker := time.NewTicker(WarmInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				log.Println("[CacheWarmer] 🔄 Periodic cache warming cycle...")
				warmAllDashboards()
			case <-ctx.Done():
				log.Println("[CacheWarmer] 🛑 Stopped (context cancelled)")
				warmerMu.Lock()
				warmerRunning = false
				warmerMu.Unlock()
				return
			}
		}
	}()
}

// warmAllDashboards pre-computes dashboard data for all common query patterns:
//   - All zones combined (zone="")
//   - Each individual zone (zone="1", "2", ..., "10")
//
// Each combination is stored in cache with a long TTL.
// Date filters are not pre-warmed (too many combinations) — those still use
// lazy caching from the normal API handler.
func warmAllDashboards() {
	start := time.Now()

	// 1. Fetch list of all zones
	zones, err := services.GetZones()
	if err != nil {
		log.Printf("[CacheWarmer] ✗ Failed to fetch zones: %v", err)
		return
	}

	// Build warm tasks: "" (all) + each zone
	tasks := []struct {
		zone string
		desc string
	}{
		{zone: "", desc: "ทุกเขต (all zones)"},
	}
	for _, z := range zones {
		tasks = append(tasks, struct {
			zone string
			desc string
		}{zone: z.Zone, desc: fmt.Sprintf("เขต %s", z.Zone)})
	}

	// 2. Execute warming concurrently (bounded concurrency)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3) // limit concurrent warm jobs to avoid overloading MongoDB
	successCount := 0
	failCount := 0
	var mu sync.Mutex

	for _, task := range tasks {
		wg.Add(1)
		sem <- struct{}{}
		go func(zone, desc string) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := warmDashboard(zone, "", ""); err != nil {
				log.Printf("[CacheWarmer]   ✗ FAILED: %s — %v", desc, err)
				mu.Lock()
				failCount++
				mu.Unlock()
			} else {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}(task.zone, task.desc)
	}

	wg.Wait()
	elapsed := time.Since(start)
	log.Printf("[CacheWarmer] ✓ Warm cycle complete: %d success, %d failed, took %v",
		successCount, failCount, elapsed)
}

// warmDashboard computes the dashboard data for a given zone/date combination
// and stores it in cache. This is the same logic as GetDashboardSummary handler
// but decoupled from HTTP context.
func warmDashboard(zone, startDate, endDate string) error {
	// Build office list
	var officeList []struct {
		PwaCode string
		Name    string
		Zone    string
	}

	if zone != "" {
		rawOffices, err := services.GetOfficesByZone(zone)
		if err != nil {
			return fmt.Errorf("GetOfficesByZone(%s): %w", zone, err)
		}
		for _, o := range rawOffices {
			officeList = append(officeList, struct {
				PwaCode string
				Name    string
				Zone    string
			}{o.PwaCode, o.Name, o.Zone})
		}
	} else {
		rawOffices, err := services.GetAllOffices()
		if err != nil {
			return fmt.Errorf("GetAllOffices: %w", err)
		}
		for _, o := range rawOffices {
			officeList = append(officeList, struct {
				PwaCode string
				Name    string
				Zone    string
			}{o.PwaCode, o.Name, o.Zone})
		}
	}

	if len(officeList) == 0 {
		return nil // no branches — nothing to warm
	}

	// Count features for all branches concurrently
	type branchResult struct {
		PwaCode    string           `json:"pwa_code"`
		BranchName string           `json:"branch_name"`
		Zone       string           `json:"zone"`
		Layers     map[string]int64 `json:"layers"`
		Total      int64            `json:"total"`
		PipeLong   float64          `json:"pipe_long"`
	}

	results := make(chan branchResult, len(officeList))
	sem := make(chan struct{}, 15)

	for _, o := range officeList {
		sem <- struct{}{}
		go func(pwa, name, z string) {
			defer func() { <-sem }()

			layers, err := services.CountAllLayersForBranch(pwa, startDate, endDate)
			if err != nil {
				results <- branchResult{PwaCode: pwa, BranchName: name, Zone: z, Layers: map[string]int64{}, Total: 0}
				return
			}
			var total int64
			for _, cnt := range layers {
				total += cnt
			}
			pipeLong, _ := services.SumPipeLength(pwa, startDate, endDate)
			results <- branchResult{PwaCode: pwa, BranchName: name, Zone: z, Layers: layers, Total: total, PipeLong: pipeLong}
		}(o.PwaCode, o.Name, o.Zone)
	}

	// Collect results
	allResults := make([]branchResult, 0, len(officeList))
	for i := 0; i < len(officeList); i++ {
		allResults = append(allResults, <-results)
	}

	// Sort by zone (numeric) then pwaCode
	sort.Slice(allResults, func(i, j int) bool {
		zi, _ := strconv.Atoi(allResults[i].Zone)
		zj, _ := strconv.Atoi(allResults[j].Zone)
		if zi != zj {
			return zi < zj
		}
		return allResults[i].PwaCode < allResults[j].PwaCode
	})

	// Aggregate totals per zone
	zoneTotals := make(map[string]map[string]int64)
	zoneNames := []string{}
	seen := map[string]bool{}
	for _, r := range allResults {
		if !seen[r.Zone] {
			seen[r.Zone] = true
			zoneNames = append(zoneNames, r.Zone)
		}
		if _, ok := zoneTotals[r.Zone]; !ok {
			zoneTotals[r.Zone] = make(map[string]int64)
		}
		for layer, cnt := range r.Layers {
			zoneTotals[r.Zone][layer] += cnt
		}
		zoneTotals[r.Zone]["_total"] += r.Total
		zoneTotals[r.Zone]["_branches"]++
	}

	// Sort zone names numerically
	sort.Slice(zoneNames, func(i, j int) bool {
		a, _ := strconv.Atoi(zoneNames[i])
		b, _ := strconv.Atoi(zoneNames[j])
		return a < b
	})

	// Compute grand totals
	grandTotal := make(map[string]int64)
	for _, zt := range zoneTotals {
		for k, v := range zt {
			grandTotal[k] += v
		}
	}

	// Build the response (same structure as GetDashboardSummary)
	response := map[string]interface{}{
		"status":         "success",
		"branches":       allResults,
		"zone_totals":    zoneTotals,
		"grand_total":    grandTotal,
		"zone_names":     zoneNames,
		"total_branches": len(allResults),
	}

	// Store in cache with extended TTL for warmed data
	cacheKey := CacheKey("dashboard", zone, startDate, endDate)
	raw, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}

	SetCachedDashboardRaw(cacheKey, raw, WarmCacheTTL)
	return nil
}