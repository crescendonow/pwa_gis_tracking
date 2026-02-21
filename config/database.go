package config

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	PgDB    *sql.DB
	MongoDB *mongo.Database
)

// ConnectPostgres connects to PostgreSQL
func ConnectPostgres() {
	host := os.Getenv("PG_HOST")
	port := os.Getenv("PG_PORT")
	dbname := os.Getenv("PG_DATABASE")
	user := os.Getenv("PG_USER")
	password := os.Getenv("PG_PASSWORD")

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	var err error
	PgDB, err = sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal("Failed to connect to PostgreSQL:", err)
	}

	PgDB.SetMaxOpenConns(10)
	PgDB.SetMaxIdleConns(5)
	PgDB.SetConnMaxLifetime(5 * time.Minute)

	err = PgDB.Ping()
	if err != nil {
		log.Fatal("Failed to ping PostgreSQL:", err)
	}

	fmt.Println("✅ Connected to PostgreSQL successfully!")
}

// ConnectMongoDB connects to MongoDB
func ConnectMongoDB() {
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		// Fallback: ลองใช้ MONGO_URI (ตรงกับ Python config)
		uri = os.Getenv("MONGO_URI")
	}
	if uri == "" {
		log.Fatal("❌ MONGODB_URI or MONGO_URI is not set")
	}

	dbName := os.Getenv("MONGO_DATABASE")
	if dbName == "" {
		dbName = "vallaris_feature" // default ตรงกับ Python
		log.Printf("⚠️ MONGO_DATABASE not set, using default: %s", dbName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	clientOpts := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		log.Fatal("Failed to connect to MongoDB:", err)
	}

	err = client.Ping(ctx, nil)
	if err != nil {
		log.Fatal("Failed to ping MongoDB:", err)
	}

	MongoDB = client.Database(dbName)
	fmt.Printf("✅ Connected to MongoDB successfully! (database: %s)\n", dbName)
}

// GetMongoCollection returns a MongoDB collection
func GetMongoCollection(name string) *mongo.Collection {
	return MongoDB.Collection(name)
}