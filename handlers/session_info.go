package handlers

import (
	"net/http"

	"pwa_gis_tracking/config"
	"pwa_gis_tracking/services"

	"github.com/gin-gonic/gin"
)

// GetSessionInfo returns the current user's session data for permission-aware UI.
// The frontend uses this to determine which zones/branches to show.
//
// GET /api/session/info
//
// Response:
//
//	{
//	  "uid":              "14180",
//	  "uname":            "สมชาย ใจดี",
//	  "pwa_code":         "1020",
//	  "permission":       "leak",
//	  "permission_leak":  "all",    // "all"|"reg"|"branch"
//	  "download_tier":    "full",   // "full"|"basic"
//	  "allowed_formats":  ["xlsx","csv","geojson", ...],
//	  "area":             "3",      // zone number
//	  "job_name":         "งานแผนที่แนวท่อ",
//	  "division":         "กองเทคโนโลยี...",
//	  "institution":      "สำนักควบคุม..."
//	}
func GetSessionInfo(c *gin.Context) {
	session, err := config.Store.Get(c.Request, config.SessionName)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	// Sessions created before this deploy won't have download_tier set —
	// fall back to the most restrictive tier, "basic".
	tier, _ := session.Values[sessDownloadTier].(string)
	if tier == "" {
		tier = services.TierBasic
	}

	c.JSON(http.StatusOK, gin.H{
		"status":          "success",
		"uid":             session.Values[sessUID],
		"uname":           session.Values[sessUname],
		"pwa_code":        session.Values[sessPwaCode],
		"permission":      session.Values[sessPermission],
		"permission_leak": session.Values[sessPermLeak],
		"download_tier":   tier,
		"allowed_formats": services.AllowedFormats(tier),
		"area":            session.Values[sessArea],
		"job_name":        session.Values[sessJobName],
		"division":        session.Values[sessDivision],
		"institution":     session.Values[sessInsitution],
		"position":        session.Values[sessPosition],
	})
}