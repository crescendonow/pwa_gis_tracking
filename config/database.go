package config

import (
	"context"
	"database/sql"
	"log"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver registered as "pgx"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// PgDB is the shared PostgreSQL connection pool.
var PgDB *sql.DB

// MongoDB is the shared MongoDB client.
var MongoDB *mongo.Client

// LoginLogFile is the shared file handle for writing login audit entries.
// Set by main.go before any handlers are called.
var LoginLogFile *os.File

// ConnectPostgres opens the PostgreSQL connection pool and verifies it.
func ConnectPostgres() {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		// Build DSN from individual env vars so operators have both options.
		dsn = buildPgDSN()
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("postgres open error: %v", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatalf("postgres ping failed: %v", err)
	}

	// Force UTF-8 encoding for Thai text support
	if _, err := db.Exec("SET client_encoding TO 'UTF8'"); err != nil {
		log.Printf("warning: could not set client_encoding: %v", err)
	}

	PgDB = db
	log.Println("PostgreSQL connected")
}

func buildPgDSN() string {
	host := getEnvOrDefault("PG_HOST", "localhost")
	port := getEnvOrDefault("PG_PORT", "5432")
	user := getEnvOrDefault("PG_USER", "postgres")
	pass := getEnvOrDefault("PG_PASS", "")
	dbname := getEnvOrDefault("PG_DB", "postgres")
	sslmode := getEnvOrDefault("PG_SSLMODE", "disable")
	return "host=" + host + " port=" + port +
		" user=" + user + " password=" + pass +
		" dbname=" + dbname + " sslmode=" + sslmode +
		" client_encoding=UTF8"
}

// ConnectMongoDB opens the MongoDB client and verifies the connection.
func ConnectMongoDB() {
	MongoDBName = getEnvOrDefault("MONGO_DB_NAME", "pwa_gis_tracking")
	uri := getEnvOrDefault("MONGO_URI", "mongodb://localhost:27017")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(context.TODO(), options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatalf("mongo connect error: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("mongo ping failed: %v", err)
	}

	MongoDB = client
	log.Println("MongoDB connected")
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}