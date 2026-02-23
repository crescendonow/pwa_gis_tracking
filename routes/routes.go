package routes

import (
	"pwa_gis_tracking/handlers"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes sets up all HTTP routes for the application.
func RegisterRoutes(router *gin.Engine) {
	// 1. Load HTML templates (คำสั่งนี้ต้องอยู่กับ router ตัวหลัก)
	router.LoadHTMLGlob("templates/*")

	// 2. สร้าง Group ครอบ Path ที่ต้องการ
	base := router.Group("/pwa_gis_tracking")
	{
		// Serve static files (CSS, JS) ภายใต้ group
		base.Static("/static", "./static")

		// HTML pages
		base.GET("/", func(c *gin.Context) {
			c.HTML(200, "dashboard.html", nil)
		})
		base.GET("/detail", func(c *gin.Context) {
			c.HTML(200, "detail.html", nil)
		})

		// REST API endpoints ภายใต้ group
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
			api.GET("/debug/collection", handlers.DebugCollection)
		}

		// Health check endpoint
		base.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"status":  "OK",
				"service": "PWA GIS Online Tracking",
				"port":    5011,
			})
		})
	}
}