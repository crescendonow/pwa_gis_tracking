package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"pwa_gis_tracking/services"

	"github.com/gin-gonic/gin"
)

// AdvancedQuery handles POST /api/features/advanced-query.
// Accepts a structured condition tree and returns paginated results.
func AdvancedQuery(c *gin.Context) {
	var req services.AdvancedQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	// Require at least one pwa code
	if req.PwaCode == "" && len(req.PwaCodes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pwaCode or pwaCodes is required"})
		return
	}
	if req.Collection == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "collection is required"})
		return
	}

	// Validate collection name
	validLayers := services.GetAllLayerNames()
	valid := false
	for _, l := range validLayers {
		if l == req.Collection {
			valid = true
			break
		}
	}
	if !valid {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid collection. Valid: " + strings.Join(validLayers, ", "),
		})
		return
	}

	// Enforce defaults
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 200 {
		req.PageSize = 50
	}
	if req.Limit <= 0 || req.Limit > 10000 {
		req.Limit = 5000
	}

	result, err := services.ExecuteAdvancedQuery(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "success",
		"data":        result.Data,
		"columns":     result.Columns,
		"page":        result.Page,
		"page_size":   result.PageSize,
		"total":       result.Total,
		"total_pages": result.TotalPages,
	})
}

// AdvancedQueryExport handles POST /api/features/advanced-query/export.
// Executes the query and returns a downloadable file in the requested format.
func AdvancedQueryExport(c *gin.Context) {
	var req struct {
		services.AdvancedQueryRequest
		Format string `json:"format"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	if req.PwaCode == "" && len(req.PwaCodes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pwaCode or pwaCodes is required"})
		return
	}
	if req.Collection == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "collection is required"})
		return
	}

	if req.Format == "" {
		req.Format = "csv"
	}
	if req.Limit <= 0 || req.Limit > 10000 {
		req.Limit = 10000
	}

	filename := fmt.Sprintf("%s_%s_query", req.PwaCode, req.Collection)
	auditDetail := fmt.Sprintf("%s:%s", req.PwaCode, req.Collection)

	// Get GeoJSON with geometry
	geojsonData, err := services.ExportAdvancedQueryAsGeoJSON(&req.AdvancedQueryRequest)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	switch req.Format {
	case "csv":
		csvData, csvErr := services.ConvertGeoJSONToCSV(geojsonData)
		if csvErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "CSV export failed: " + csvErr.Error()})
			return
		}
		// Add BOM for Excel compatibility
		bom := []byte{0xEF, 0xBB, 0xBF}
		csvData = append(bom, csvData...)
		LogAuditEvent(c, "export_csv_query", "export", auditDetail)
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
		c.Data(http.StatusOK, "text/csv; charset=utf-8", csvData)

	case "geojson":
		LogAuditEvent(c, "export_geojson_query", "export", auditDetail)
		c.Header("Content-Type", "application/geo+json")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.geojson", filename))
		c.Data(http.StatusOK, "application/geo+json", geojsonData)

	case "gpkg":
		data, convErr := services.ExportMergedAsGeoPackage(geojsonData, filename)
		if convErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "GPKG export failed: " + convErr.Error()})
			return
		}
		LogAuditEvent(c, "export_gpkg_query", "export", auditDetail)
		c.Header("Content-Type", "application/geopackage+sqlite3")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.gpkg", filename))
		c.Data(http.StatusOK, "application/geopackage+sqlite3", data)

	case "shp":
		data, convErr := services.ExportMergedAsShapefile(geojsonData, filename)
		if convErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Shapefile export failed: " + convErr.Error()})
			return
		}
		LogAuditEvent(c, "export_shp_query", "export", auditDetail)
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s_shp.zip", filename))
		c.Data(http.StatusOK, "application/zip", data)

	case "fgb":
		data, convErr := services.ExportMergedAsFlatGeobuf(geojsonData, filename)
		if convErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "FlatGeobuf export failed: " + convErr.Error()})
			return
		}
		LogAuditEvent(c, "export_fgb_query", "export", auditDetail)
		c.Header("Content-Type", "application/octet-stream")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.fgb", filename))
		c.Data(http.StatusOK, "application/octet-stream", data)

	case "pmtiles":
		// PMTiles via ogr2ogr from GeoJSON
		data, convErr := services.ExportMergedAsPMTiles(geojsonData, filename)
		if convErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "PMTiles export failed: " + convErr.Error()})
			return
		}
		LogAuditEvent(c, "export_pmtiles_query", "export", auditDetail)
		c.Header("Content-Type", "application/octet-stream")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.pmtiles", filename))
		c.Data(http.StatusOK, "application/octet-stream", data)

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported format: " + req.Format})
	}
}
