package handlers

// ========================================================================
// Export Handler — Merge Mode Support (ตัวอย่างโค้ดสำหรับเพิ่มใน handler)
//
// เพิ่ม logic นี้ใน handler ที่จัดการ /api/export/geodata
// รองรับ query params:
//   - pwaCode:    comma-separated (e.g. "001,002,003")
//   - collection: comma-separated (e.g. "pipe,valve,meter")
//   - merge:      "all" | "branch" | "layer" | "" (default=split)
//   - format:     "geojson" | "gpkg" | "shp" | "fgb" | "tab" | "pmtiles"
// ========================================================================

import (
	"net/http"
	"strings"

	"pwa_gis_tracking/services"

	"github.com/gin-gonic/gin"
)

// ExportGeoDataHandler handles GET /api/export/geodata
// Supports both single and merged multi-branch/multi-layer exports.
func ExportGeoDataHandler(c *gin.Context) {
	pwaCodeParam := c.Query("pwaCode")     // comma-separated
	collectionParam := c.Query("collection") // comma-separated
	format := c.DefaultQuery("format", "geojson")
	mergeMode := c.Query("merge") // "all", "branch", "layer", ""
	startDate := c.Query("startDate")
	endDate := c.Query("endDate")

	pwaCodes := splitAndTrim(pwaCodeParam)
	collections := splitAndTrim(collectionParam)

	if len(pwaCodes) == 0 || len(collections) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pwaCode and collection required"})
		return
	}

	// ─── Merged Export Mode ───────────────────────────────
	if mergeMode != "" && (len(pwaCodes) > 1 || len(collections) > 1) {
		// Get merged GeoJSON
		geojsonData, err := services.ExportMergedFeaturesAsGeoJSON(pwaCodes, collections, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Build output filename
		outputName := "merged"
		if len(pwaCodes) == 1 {
			outputName = pwaCodes[0]
		}
		if len(collections) == 1 {
			outputName += "_" + collections[0]
		} else {
			outputName += "_multi"
		}

		switch format {
		case "geojson":
			c.Header("Content-Disposition", "attachment; filename="+outputName+".geojson")
			c.Data(http.StatusOK, "application/geo+json", geojsonData)

		case "gpkg":
			data, err := services.ExportMergedAsGeoPackage(geojsonData, outputName)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.Header("Content-Disposition", "attachment; filename="+outputName+".gpkg")
			c.Data(http.StatusOK, "application/geopackage+sqlite3", data)

		case "shp":
			data, err := services.ExportMergedAsShapefile(geojsonData, outputName)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.Header("Content-Disposition", "attachment; filename="+outputName+"_shp.zip")
			c.Data(http.StatusOK, "application/zip", data)

		// TODO: Add merge support for fgb, tab, pmtiles using same pattern:
		// case "fgb": data, err := services.ExportMergedAsFlatGeobuf(geojsonData, outputName)
		// case "tab": data, err := services.ExportMergedAsMapInfoTAB(geojsonData, outputName)

		default:
			// Fallback: return merged GeoJSON
			c.Header("Content-Disposition", "attachment; filename="+outputName+".geojson")
			c.Data(http.StatusOK, "application/geo+json", geojsonData)
		}
		return
	}

	// ─── Single Export Mode (original logic) ─────────────
	pwaCode := pwaCodes[0]
	collection := collections[0]

	switch format {
	case "geojson":
		data, err := services.ExportFeaturesAsGeoJSON(pwaCode, collection, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Header("Content-Disposition", "attachment; filename="+pwaCode+"_"+collection+".geojson")
		c.Data(http.StatusOK, "application/geo+json", data)

	case "gpkg":
		data, err := services.ExportAsGeoPackage(pwaCode, collection, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Header("Content-Disposition", "attachment; filename="+pwaCode+"_"+collection+".gpkg")
		c.Data(http.StatusOK, "application/geopackage+sqlite3", data)

	case "shp":
		data, err := services.ExportAsShapefile(pwaCode, collection, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Header("Content-Disposition", "attachment; filename="+pwaCode+"_"+collection+"_shp.zip")
		c.Data(http.StatusOK, "application/zip", data)

	case "fgb":
		data, err := services.ExportAsFlatGeobuf(pwaCode, collection, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Header("Content-Disposition", "attachment; filename="+pwaCode+"_"+collection+".fgb")
		c.Data(http.StatusOK, "application/flatgeobuf", data)

	case "tab":
		data, err := services.ExportAsMapInfoTAB(pwaCode, collection, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Header("Content-Disposition", "attachment; filename="+pwaCode+"_"+collection+"_tab.zip")
		c.Data(http.StatusOK, "application/zip", data)

	case "pmtiles":
		data, err := services.ExportAsPMTiles(pwaCode, collection, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Header("Content-Disposition", "attachment; filename="+pwaCode+"_"+collection+".pmtiles")
		c.Data(http.StatusOK, "application/x-pmtiles", data)

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported format: " + format})
	}
}

func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}