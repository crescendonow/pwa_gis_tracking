package config

import (
	"go.mongodb.org/mongo-driver/mongo"
)

// MongoDBName holds the database name used throughout the app.
// Set this in ConnectMongoDB() when you parse the connection string.
var MongoDBName string

// GetMongoCollection returns a *mongo.Collection from the connected database.
// Usage:  col := config.GetMongoCollection("my_collection")
func GetMongoCollection(collectionName string) *mongo.Collection {
	return MongoDB.Database(MongoDBName).Collection(collectionName)
}