package routes

import (
	"pwa_gis_tracking/handlers"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes sets up all HTTP routes for the application.
func RegisterRoutes(router *gin.Engine) {
	// Serve static files (CSS, JS)
	router.Static("/static", "./static")

	// Load HTML templates
	router.LoadHTMLGlob("templates/*")

	// HTML pages
	router.GET("/", func(c *gin.Context) {
		c.HTML(200, "dashboard.html", nil)
	})
	router.GET("/detail", func(c *gin.Context) {
		c.HTML(200, "detail.html", nil)
	})

	// REST API endpoints
	api := router.Group("/api")
	{
		api.GET("/zones", handlers.GetZones)                // List zones with branch counts
		api.GET("/offices", handlers.GetOffices)            // List offices (optional zone filter)
		api.GET("/offices/geom", handlers.GetOfficesWithGeom) // Offices with lat/lng from WKB geometry
		api.GET("/years", handlers.GetYears)                // Available years for date filter
		api.GET("/layers", handlers.GetLayers)              // Supported GIS layers
		api.GET("/counts", handlers.GetBranchCounts)        // Feature counts for a single branch
		api.GET("/dashboard", handlers.GetDashboardSummary) // Full dashboard summary
		api.GET("/export/excel", handlers.ExportExcel)      // Download Excel summary
		api.GET("/export/geodata", handlers.ExportGeoData)  // Download GeoJSON/GPKG
		api.GET("/debug/collection", handlers.DebugCollection) // Debug MongoDB collection lookup
	}

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "OK",
			"service": "PWA GIS Online Tracking",
			"port":    5011,
		})
	})
}
