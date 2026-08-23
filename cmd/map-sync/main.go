// pwa-gis-map-sync is the standalone, NSSM-ready MongoDB-to-PostGIS map mirror.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pwa_gis_tracking/mapsync"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
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
		Source: mapsync.MongoSource{Client: mongoClient, Database: firstEnvDefault("MONGO_DB_NAME", "pwa_gis_tracking")},
		Store:  mapsync.PostgresStore{DB: database}, Warmer: warmer,
		BatchSize: readInt("MAP_SYNC_BATCH_SIZE", 500), MaxConcurrent: readInt("MAP_SYNC_CONCURRENCY", 4),
		CycleTimeout: readDuration("MAP_SYNC_CYCLE_TIMEOUT", 14*time.Minute), CollectionTimeout: readDuration("MAP_SYNC_COLLECTION_TIMEOUT", 10*time.Minute),
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
	log.Printf("pwa-gis-map-sync started: every %s, reconciliation every %s", mapsync.IncrementalInterval, mapsync.ReconcileInterval)
	if err := worker.Run(serviceCtx); err != nil && err != context.Canceled {
		log.Printf("pwa-gis-map-sync stopped: %v", err)
	}
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
