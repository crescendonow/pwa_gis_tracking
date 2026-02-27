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

		// REST API endpoints
		api := base.Group("/api")
		{
			api.GET("/zones", handlers.GetZones)
			api.GET("/offices", handlers.GetOffices)
			api.GET("/offices/geom", handlers.GetOfficesWithGeom)
			api.GET("/years", handlers.GetYears)
			api.GET("/layers", handlers.GetLayers)
			api.GET("/counts", handlers.GetBranchCounts)
			api.GET("/dashboard", handlers.GetDashboardSummary)
			api.GET("/export/excel", handlers.ExportExcel)
			api.GET("/export/geodata", handlers.ExportGeoData)
			api.GET("/features/map", handlers.GetFeaturesForMap)
			api.GET("/features/properties", handlers.GetFeatureProps)
			api.GET("/cache/invalidate", handlers.InvalidateCache)
			api.GET("/debug/collection", handlers.DebugCollection)
		}

		// Health check
		base.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"status":  "OK",
				"service": "PWA GIS Online Tracking",
			})
		})
	}
}
