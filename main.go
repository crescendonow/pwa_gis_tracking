package main

import (
	"fmt"
	"log"
	"os"

	"pwa_gis_tracking/config"
	"pwa_gis_tracking/routes"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system env")
	}

	// Establish database connections
	config.ConnectPostgres()
	config.ConnectMongoDB()

	// Initialize Gin router in release mode
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	// CORS middleware for cross-origin requests
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// Register all routes
	routes.RegisterRoutes(router)

	// Determine port (default: 5011)
	port := os.Getenv("PORT")
	if port == "" {
		port = "5011"
	}

	fmt.Printf("\n")
	fmt.Printf("============================================\n")
	fmt.Printf("  PWA GIS Online Tracking Dashboard\n")
	fmt.Printf("  Running on http://localhost:%s\n", port)
	fmt.Printf("============================================\n")
	fmt.Printf("\n")

	router.Run(":" + port)
}
