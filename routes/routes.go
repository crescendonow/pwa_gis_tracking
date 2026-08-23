package routes

import (
	"pwa_gis_tracking/handlers"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes sets up all HTTP routes for the application.
func RegisterRoutes(router *gin.Engine) {

	// 1. Load HTML templates
	router.LoadHTMLGlob("templates/*")

	basePath := "/pwa_gis_tracking"

	// ─── Public routes (no auth required) ─────────────────
	pub := router.Group(basePath)
	{
		// Login page (GET) and login action (POST)
		pub.GET("/login", handlers.ShowLoginPage)
		pub.POST("/login", handlers.HandleLogin)

		// Logout
		pub.GET("/logout", handlers.HandleLogout)
	}

	// ─── Static files (served WITHOUT auth so login page can load CSS/images) ──
	// Single Static() call to avoid Gin wildcard conflict.
	// Images, icons, CSS, JS are all under ./static/
	router.Group(basePath).Static("/static", "./static")

	// ─── Protected routes (session auth required) ─────────
	base := router.Group(basePath, handlers.AuthRequired(basePath))
	{
		// HTML pages
		base.GET("/", func(c *gin.Context) {
			c.HTML(200, "dashboard.html", nil)
		})
		base.GET("/dashboard.html", func(c *gin.Context) {
			c.Redirect(301, basePath+"/")
		})
		base.GET("/detail", func(c *gin.Context) {
			c.HTML(200, "detail.html", nil)
		})
		base.GET("/map", func(c *gin.Context) {
			c.HTML(200, "map.html", nil)
		})

		// REST API endpoints
		api := base.Group("/api", handlers.AuditLogMiddleware())
		{
			api.GET("/zones", handlers.GetZones)
			api.GET("/zones/centers", handlers.GetZoneCenters)
			api.GET("/offices", handlers.GetOffices)
			api.GET("/offices/geom", handlers.GetOfficesWithGeom)
			api.GET("/years", handlers.GetYears)
			api.GET("/layers", handlers.GetLayers)
			api.GET("/counts", handlers.GetBranchCounts)
			api.GET("/dashboard", handlers.GetDashboardSummary)
			api.GET("/export/excel", handlers.ExportExcel)
			// Requirement 1.2: geodata formats (geojson/gpkg/shp/fgb/tab/pmtiles/
			// mbtiles) require download_tier == "full"; /export/excel stays
			// ungated since xlsx is allowed for everyone.
			api.GET("/export/geodata",
				handlers.RequireFullDownload("format", "geojson"),
				handlers.ExportGeoData)
			api.GET("/features/map", handlers.GetFeaturesForMap)
			api.GET("/features/properties", handlers.GetFeatureProps)
			api.GET("/cache/invalidate", handlers.InvalidateCache)
			api.GET("/debug/collection", handlers.DebugCollection)
			// Add API route for monitoring cache status
			api.GET("/cache/status", handlers.GetCacheStatus)
			api.GET("/features/list", handlers.GetFeaturesList)
			api.GET("/features/suggest", handlers.GetFeatureSuggestions)
			api.GET("/features/facets", handlers.GetFeatureFacets)
			api.GET("/field-mapping", handlers.GetFieldMapping)
			// Session info (for permission-aware UI)
			api.GET("/session/info", handlers.GetSessionInfo)

			// Advanced Query Builder
			api.POST("/features/advanced-query", handlers.AdvancedQuery)
			api.POST("/features/advanced-query/export", handlers.AdvancedQueryExport)

			// Chatbot — text-to-query (proxy to Python service)
			api.POST("/chatbot/query", handlers.ChatbotQuery)

			api.GET("/map/config", handlers.GetMapConfig)
			api.GET("/map/summary", handlers.GetMapSummary)
		}

		// Tile requests are high-frequency and remain authenticated, but bypass
		// per-request audit inserts to avoid one database write for every map tile.
		base.GET("/api/map/tiles/:layer/:z/:x/:y", handlers.GetMapTile)

		// Health check
		base.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"status":  "OK",
				"service": "PWA GIS Online Tracking",
			})
		})
	}
}
