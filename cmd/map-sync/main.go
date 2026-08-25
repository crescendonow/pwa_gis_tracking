// pwa-gis-map-sync is the standalone, NSSM-ready MongoDB-to-PostGIS map mirror.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"pwa_gis_tracking/mapsync"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	modeFlag := flag.String("mode", "", `sync mode: "service" (default, long-running) or "backfill" (one unlimited cycle then exit)`)
	flag.Parse()
	mode := strings.ToLower(strings.TrimSpace(*modeFlag))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(os.Getenv("MAP_SYNC_MODE")))
	}
	if mode == "" {
		mode = "service"
	}
	if envFile := os.Getenv("MAP_SYNC_ENV_FILE"); envFile != "" {
		_ = godotenv.Overload(envFile)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	databaseURL := firstEnv("DATABASE_URL", "POSTGRES_DSN")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL or POSTGRES_DSN is required")
	}
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		log.Fatalf("postgres open: %v", err)
	}
	defer database.Close()
	database.SetMaxOpenConns(readInt("MAP_SYNC_PG_MAX_CONNS", 8))
	if err := database.PingContext(ctx); err != nil {
		log.Fatalf("postgres ping: %v", err)
	}
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		log.Fatal("MONGO_URI is required")
	}
	mongoClient, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI).SetServerSelectionTimeout(15*time.Second))
	if err != nil {
		log.Fatalf("mongo connect: %v", err)
	}
	defer mongoClient.Disconnect(context.Background())
	if err := mongoClient.Ping(ctx, nil); err != nil {
		log.Fatalf("mongo ping: %v", err)
	}
	warmer := mapsync.HTTPWarmer{BaseURL: firstEnvDefault("MAP_MARTIN_URL", "http://127.0.0.1:5031"), Source: firstEnvDefault("MAP_MARTIN_SOURCE", "pwa_gis_map_tile"), MaxConcurrent: readInt("MAP_WARM_CONCURRENCY", 4)}
	worker := &mapsync.Worker{
		Source: mapsync.MongoSource{Client: mongoClient, Database: firstEnvDefault("MONGO_DB_NAME", "pwa_gis_tracking"), IncludeRollups: readBool("MAP_SYNC_INCLUDE_ROLLUPS", false)},
		Store:  mapsync.PostgresStore{DB: database}, Warmer: warmer,
		BatchSize: readInt("MAP_SYNC_BATCH_SIZE", 500), MaxConcurrent: readInt("MAP_SYNC_CONCURRENCY", 4),
		CycleTimeout: readDuration("MAP_SYNC_CYCLE_TIMEOUT", 14*time.Minute), CollectionTimeout: readDuration("MAP_SYNC_COLLECTION_TIMEOUT", 10*time.Minute),
		FinalizeTimeout: readDuration("MAP_SYNC_FINALIZE_TIMEOUT", 10*time.Minute),
		LoadDMAColors:   openDMAColorLoader(ctx),
		WarmTargets: func(ctx context.Context) ([]mapsync.WarmTarget, error) {
			bounds, err := mapsync.LoadZoneBounds(ctx, database)
			if err != nil {
				return nil, err
			}
			return mapsync.WarmProfile(bounds), nil
		},
		Report: func(result mapsync.CycleResult, cycleErr error) {
			payload, _ := json.Marshal(result)
			log.Printf("map_sync_cycle result=%s error=%q", payload, errorText(cycleErr))
		},
	}
	serviceCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if mode == "backfill" {
		// A first-time backfill of tens of millions of records must not be cut
		// short by the service's cycle/collection timeouts.
		worker.CycleTimeout = -1
		worker.CollectionTimeout = -1
		runBackfill(serviceCtx, worker)
		return
	}
	log.Printf("pwa-gis-map-sync started: every %s, reconciliation every %s", mapsync.IncrementalInterval, mapsync.ReconcileInterval)
	if err := worker.Run(serviceCtx); err != nil && err != context.Canceled {
		log.Printf("pwa-gis-map-sync stopped: %v", err)
	}
}

// runBackfill drives a single unlimited RunCycle to completion, logging
// progress periodically since the cycle itself may run for hours and does
// not return until it is entirely done. It exits the process directly so the
// operator's script can branch on the exit code.
func runBackfill(ctx context.Context, worker *mapsync.Worker) {
	var mu sync.Mutex
	collectionsDone, featuresUpserted := 0, 0
	current := ""
	worker.Progress = func(result mapsync.CollectionResult) {
		mu.Lock()
		collectionsDone++
		featuresUpserted += result.Upserted
		current = result.Alias
		mu.Unlock()
	}
	stopProgress := make(chan struct{})
	var progress sync.WaitGroup
	progress.Add(1)
	go func() {
		defer progress.Done()
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopProgress:
				return
			case <-ticker.C:
				mu.Lock()
				log.Printf("map_sync_progress collections_done=%d features_upserted=%d current=%s", collectionsDone, featuresUpserted, current)
				mu.Unlock()
			}
		}
	}()
	log.Printf("pwa-gis-map-sync backfill starting: unlimited cycle and collection timeouts")
	result, err := worker.RunCycle(ctx, true)
	close(stopProgress)
	progress.Wait()
	payload, _ := json.Marshal(result)
	log.Printf("map_sync_backfill result=%s error=%q", payload, errorText(err))
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// openDMAColorLoader opens the optional second connection to the DMA colour
// source database (a different server than the mirror). It never fails
// startup: a missing or unreachable MAP_DMA_SOURCE_DATABASE_URL just logs a
// warning and leaves DMA colours unsynced until it is configured.
func openDMAColorLoader(ctx context.Context) func(context.Context) ([]mapsync.DMAColor, error) {
	dsn := strings.TrimSpace(os.Getenv("MAP_DMA_SOURCE_DATABASE_URL"))
	if dsn == "" {
		log.Printf("map_sync_dma_source_disabled reason=%q", "MAP_DMA_SOURCE_DATABASE_URL is not set")
		return nil
	}
	source, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Printf("map_sync_dma_source_disabled reason=\"open: %v\"", err)
		return nil
	}
	if err := source.PingContext(ctx); err != nil {
		log.Printf("map_sync_dma_source_disabled reason=\"ping: %v\"", err)
		_ = source.Close()
		return nil
	}
	source.SetMaxOpenConns(2)
	return mapsync.DMAColorSource{DB: source}.Load
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
func firstEnvDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
func readInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}
func readBool(key string, fallback bool) bool {
	value, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(key)))
	if err != nil {
		return fallback
	}
	return value
}
func readDuration(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
