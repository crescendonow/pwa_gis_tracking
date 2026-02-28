package handlers

import (
	"log"
	"strings"
	"time"

	"pwa_gis_tracking/config"

	"github.com/gin-gonic/gin"
)

// ========================================================================
// Audit Log System
//
// 1. AuditLogMiddleware — Gin middleware for automatic request logging
//    Captures: user info, request path, method, response status, duration
//    Applied to /api/* routes to track all API interactions
//
// 2. LogAuditEvent — Explicit logging for specific actions (export, login)
//    Called directly from handlers when more context is needed
//
// Table: audit_logs.pwagis_track_log (see migrations/001_audit_log.sql)
// ========================================================================

// AuditLogMiddleware logs all API requests to the audit table.
// Attach to router groups that need tracking.
//
// Usage in routes.go:
//
//	api := base.Group("/api", handlers.AuditLogMiddleware())
func AuditLogMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// Process request
		c.Next()

		// Skip static files and health checks
		path := c.Request.URL.Path
		if strings.Contains(path, "/static/") || strings.HasSuffix(path, "/health") {
			return
		}

		// Skip noisy endpoints (cache status, etc.)
		if strings.HasSuffix(path, "/cache/status") {
			return
		}

		duration := time.Since(start)

		// Extract user info from gin context (set by AuthRequired middleware)
		uid, _ := c.Get("uid")
		uname, _ := c.Get("uname")
		pwaCode, _ := c.Get("pwacode")
		permLeak, _ := c.Get("permission_leak")

		// Determine action from path
		action := classifyAction(c.Request.Method, path, c.Query("format"))

		// Determine target
		targetType, targetValue := classifyTarget(c)

		// Write to DB asynchronously (don't block response)
		go insertAuditLog(
			strOrEmpty(uid),
			strOrEmpty(uname),
			strOrEmpty(pwaCode),
			strOrEmpty(permLeak),
			action,
			targetType,
			targetValue,
			c.ClientIP(),
			c.Request.UserAgent(),
			path,
			c.Request.Method,
			c.Writer.Status(),
			int(duration.Milliseconds()),
		)
	}
}

// LogAuditEvent writes an explicit audit log entry.
// Use this for actions that need extra context beyond what middleware captures.
//
// Example usage in ExportGeoData handler:
//
//	handlers.LogAuditEvent(c, "export_geojson", "export",
//	    fmt.Sprintf("pwaCode=%s,collection=%s", pwaCode, collection))
func LogAuditEvent(c *gin.Context, action, targetType, targetValue string) {
	uid, _ := c.Get("uid")
	uname, _ := c.Get("uname")
	pwaCode, _ := c.Get("pwacode")
	permLeak, _ := c.Get("permission_leak")

	go insertAuditLog(
		strOrEmpty(uid),
		strOrEmpty(uname),
		strOrEmpty(pwaCode),
		strOrEmpty(permLeak),
		action,
		targetType,
		targetValue,
		c.ClientIP(),
		c.Request.UserAgent(),
		c.Request.URL.Path,
		c.Request.Method,
		0,
		0,
	)
}

// ────────────────────────────────────────────────────────────
// Internal helpers
// ────────────────────────────────────────────────────────────

func insertAuditLog(
	userID, userName, pwaCode, permLevel,
	action, targetType, targetValue,
	ip, userAgent, requestPath, requestMethod string,
	responseStatus, durationMs int,
) {
	if config.PgDB == nil {
		return
	}

	const query = `
		INSERT INTO audit_logs.pwagis_track_log
			(user_id, user_name, pwa_code, permission_level,
			 action, target_type, target_value,
			 ip_address, user_agent, request_path, request_method,
			 response_status, duration_ms)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	`

	_, err := config.PgDB.Exec(query,
		userID, userName, pwaCode, permLevel,
		action, targetType, targetValue,
		ip, userAgent, requestPath, requestMethod,
		responseStatus, durationMs,
	)
	if err != nil {
		// Log error but don't fail — audit is best-effort
		log.Printf("[AuditLog] insert error: %v (action=%s user=%s)", err, action, userID)
	}
}

// classifyAction maps request paths to human-readable action names.
func classifyAction(method, path, format string) string {
	// Export endpoints
	if strings.Contains(path, "/export/geodata") {
		switch format {
		case "geojson":
			return "export_geojson"
		case "gpkg":
			return "export_gpkg"
		case "shp":
			return "export_shp"
		case "fgb":
			return "export_fgb"
		case "tab":
			return "export_tab"
		case "pmtiles":
			return "export_pmtiles"
		default:
			return "export_" + format
		}
	}
	if strings.Contains(path, "/export/excel") {
		return "export_excel"
	}

	// View endpoints
	if strings.Contains(path, "/features/map") {
		return "view_map"
	}
	if strings.Contains(path, "/features/list") {
		return "click_layer_modal"
	}
	if strings.Contains(path, "/features/properties") {
		return "view_feature_props"
	}
	if strings.Contains(path, "/counts") {
		return "view_detail"
	}
	if strings.Contains(path, "/dashboard") {
		return "view_dashboard"
	}

	// Login/logout
	if strings.Contains(path, "/login") && method == "POST" {
		return "login"
	}
	if strings.Contains(path, "/logout") {
		return "logout"
	}

	return "api_call"
}

// classifyTarget extracts the target type and value from query parameters.
func classifyTarget(c *gin.Context) (string, string) {
	pwaCode := c.Query("pwaCode")
	collection := c.Query("collection")
	zone := c.Query("zone")

	if collection != "" && pwaCode != "" {
		return "layer", pwaCode + ":" + collection
	}
	if pwaCode != "" {
		return "branch", pwaCode
	}
	if zone != "" {
		return "zone", zone
	}

	return "", ""
}

func strOrEmpty(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}