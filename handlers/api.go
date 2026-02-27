package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"pwa_gis_tracking/config"
	"pwa_gis_tracking/services"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

// GetZones returns all zones with branch counts.
// GET /api/zones
func GetZones(c *gin.Context) {
	zones, err := services.GetZones()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": zones})
}

// GetOffices returns branch offices, optionally filtered by zone.
// GET /api/offices?zone=xxx
func GetOffices(c *gin.Context) {
	zone := c.Query("zone")

	var offices interface{}
	var err error

	if zone != "" {
		offices, err = services.GetOfficesByZone(zone)
	} else {
		offices, err = services.GetAllOffices()
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": offices})
}

// GetOfficesWithGeom returns all offices with lat/lng from wkb_geometry.
// GET /api/offices/geom
func GetOfficesWithGeom(c *gin.Context) {
	offices, err := services.GetAllOfficesWithGeom()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": offices, "count": len(offices)})
}

// GetYears returns available years for date filtering.
// GET /api/years
func GetYears(c *gin.Context) {
	years, err := services.GetYearsFromRecordDate()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": years})
}

// GetLayers returns all supported layer names with Thai display names.
// GET /api/layers
func GetLayers(c *gin.Context) {
	layers := services.GetAllLayerNames()
	result := []map[string]string{}
	for _, l := range layers {
		result = append(result, map[string]string{
			"name":         l,
			"display_name": services.GetLayerDisplayName(l),
		})
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": result})
}

// GetBranchCounts returns feature counts per layer for a single branch.
// GET /api/counts?pwaCode=xxx&startDate=xxx&endDate=xxx
func GetBranchCounts(c *gin.Context) {
	pwaCode := c.Query("pwaCode")
	startDate := c.Query("startDate")
	endDate := c.Query("endDate")

	if pwaCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pwaCode is required"})
		return
	}

	layers, err := services.CountAllLayersForBranch(pwaCode, startDate, endDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var total int64
	for _, cnt := range layers {
		total += cnt
	}

	c.JSON(http.StatusOK, gin.H{
		"status":   "success",
		"pwa_code": pwaCode,
		"layers":   layers,
		"total":    total,
	})
}

// GetDashboardSummary returns the full dashboard summary with all branches and zone totals.
// GET /api/dashboard?zone=xxx&startDate=xxx&endDate=xxx
func GetDashboardSummary(c *gin.Context) {
	zone := c.Query("zone")
	startDate := c.Query("startDate")
	endDate := c.Query("endDate")

	// Check cache first — avoids 10+ second MongoDB aggregation
	cacheKey := CacheKey("dashboard", zone, startDate, endDate)
	if cached := GetCachedDashboard(cacheKey); cached != nil {
		c.Header("X-Cache", "HIT")
		c.Data(http.StatusOK, "application/json; charset=utf-8", cached)
		return
	}
	c.Header("X-Cache", "MISS")

	var officeList []struct {
		PwaCode string
		Name    string
		Zone    string
	}

	if zone != "" {
		rawOffices, e := services.GetOfficesByZone(zone)
		if e != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": e.Error()})
			return
		}
		for _, o := range rawOffices {
			officeList = append(officeList, struct {
				PwaCode string
				Name    string
				Zone    string
			}{o.PwaCode, o.Name, o.Zone})
		}
	} else {
		rawOffices, e := services.GetAllOffices()
		if e != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": e.Error()})
			return
		}
		for _, o := range rawOffices {
			officeList = append(officeList, struct {
				PwaCode string
				Name    string
				Zone    string
			}{o.PwaCode, o.Name, o.Zone})
		}
	}

	// Count features for all branches concurrently
	type branchResult struct {
		PwaCode    string           `json:"pwa_code"`
		BranchName string           `json:"branch_name"`
		Zone       string           `json:"zone"`
		Layers     map[string]int64 `json:"layers"`
		Total      int64            `json:"total"`
		PipeLong   float64          `json:"pipe_long"`
	}

	results := make(chan branchResult, len(officeList))
	sem := make(chan struct{}, 15) // concurrency limiter

	for _, o := range officeList {
		sem <- struct{}{}
		go func(pwa, name, z string) {
			defer func() { <-sem }()

			layers, err := services.CountAllLayersForBranch(pwa, startDate, endDate)
			if err != nil {
				results <- branchResult{PwaCode: pwa, BranchName: name, Zone: z, Layers: map[string]int64{}, Total: 0}
				return
			}
			var total int64
			for _, cnt := range layers {
				total += cnt
			}
			pipeLong, _ := services.SumPipeLength(pwa, startDate, endDate)
			results <- branchResult{PwaCode: pwa, BranchName: name, Zone: z, Layers: layers, Total: total, PipeLong: pipeLong}
		}(o.PwaCode, o.Name, o.Zone)
	}

	// Collect results
	var allResults []branchResult
	for i := 0; i < len(officeList); i++ {
		r := <-results
		allResults = append(allResults, r)
	}

	// Sort by zone (numeric) then pwaCode
	sort.Slice(allResults, func(i, j int) bool {
		zi, _ := strconv.Atoi(allResults[i].Zone)
		zj, _ := strconv.Atoi(allResults[j].Zone)
		if zi != zj {
			return zi < zj
		}
		return allResults[i].PwaCode < allResults[j].PwaCode
	})

	// Aggregate totals per zone
	zoneTotals := make(map[string]map[string]int64)
	zoneNames := []string{}
	seen := map[string]bool{}
	for _, r := range allResults {
		if !seen[r.Zone] {
			seen[r.Zone] = true
			zoneNames = append(zoneNames, r.Zone)
		}
		if _, ok := zoneTotals[r.Zone]; !ok {
			zoneTotals[r.Zone] = make(map[string]int64)
		}
		for layer, cnt := range r.Layers {
			zoneTotals[r.Zone][layer] += cnt
		}
		zoneTotals[r.Zone]["_total"] += r.Total
		zoneTotals[r.Zone]["_branches"]++
	}

	// Sort zone names numerically
	sort.Slice(zoneNames, func(i, j int) bool {
		a, _ := strconv.Atoi(zoneNames[i])
		b, _ := strconv.Atoi(zoneNames[j])
		return a < b
	})

	// Compute grand totals across all zones
	grandTotal := make(map[string]int64)
	for _, zt := range zoneTotals {
		for k, v := range zt {
			grandTotal[k] += v
		}
	}

	response := gin.H{
		"status":         "success",
		"branches":       allResults,
		"zone_totals":    zoneTotals,
		"grand_total":    grandTotal,
		"zone_names":     zoneNames,
		"total_branches": len(allResults),
	}

	// Store in cache for subsequent requests
	SetCachedDashboard(cacheKey, response)

	c.JSON(http.StatusOK, response)
}

// InvalidateCache clears the dashboard cache (force refresh on next load).
// GET /api/cache/invalidate
func InvalidateCache(c *gin.Context) {
	InvalidateDashboardCache()
	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Cache invalidated"})
}

// ExportExcel generates and downloads an Excel summary report.
// GET /api/export/excel?zone=xxx&startDate=xxx&endDate=xxx
func ExportExcel(c *gin.Context) {
	zone := c.Query("zone")
	startDate := c.Query("startDate")
	endDate := c.Query("endDate")

	var officeList []struct {
		PwaCode string
		Name    string
		Zone    string
	}

	if zone != "" {
		rawOffices, err := services.GetOfficesByZone(zone)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, o := range rawOffices {
			officeList = append(officeList, struct {
				PwaCode string
				Name    string
				Zone    string
			}{o.PwaCode, o.Name, o.Zone})
		}
	} else {
		rawOffices, err := services.GetAllOffices()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, o := range rawOffices {
			officeList = append(officeList, struct {
				PwaCode string
				Name    string
				Zone    string
			}{o.PwaCode, o.Name, o.Zone})
		}
	}

	layers := services.GetAllLayerNames()

	f := excelize.NewFile()
	sheet := "GIS Summary"
	f.SetSheetName("Sheet1", sheet)

	// Build header row: insert "Pipe Length(m)" right after "pipe" layer
	headers := []string{"No.", "Branch Code", "Branch Name", "Zone"}
	pipeColOffset := -1
	for i, l := range layers {
		headers = append(headers, services.GetLayerDisplayName(l))
		if l == "pipe" {
			headers = append(headers, "ความยาวท่อ(ม.)")
			pipeColOffset = i
		}
	}
	headers = append(headers, "Total")

	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
	}

	// Style header row
	style, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 12, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"1B4F72"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "000000", Style: 1},
			{Type: "top", Color: "000000", Style: 1},
			{Type: "bottom", Color: "000000", Style: 1},
			{Type: "right", Color: "000000", Style: 1},
		},
	})
	lastCol, _ := excelize.CoordinatesToCellName(len(headers), 1)
	f.SetCellStyle(sheet, "A1", lastCol, style)

	// Fetch data concurrently
	sem := make(chan struct{}, 10)
	type rowData struct {
		Index    int
		PwaCode  string
		Name     string
		Zone     string
		Layers   map[string]int64
		Total    int64
		PipeLong float64
	}

	rowChan := make(chan rowData, len(officeList))
	for i, o := range officeList {
		sem <- struct{}{}
		go func(idx int, pwa, name, z string) {
			defer func() { <-sem }()
			layerCounts, _ := services.CountAllLayersForBranch(pwa, startDate, endDate)
			var total int64
			for _, cnt := range layerCounts {
				total += cnt
			}
			pipeLong, _ := services.SumPipeLength(pwa, startDate, endDate)
			rowChan <- rowData{Index: idx, PwaCode: pwa, Name: name, Zone: z, Layers: layerCounts, Total: total, PipeLong: pipeLong}
		}(i, o.PwaCode, o.Name, o.Zone)
	}

	rows := make([]rowData, len(officeList))
	for i := 0; i < len(officeList); i++ {
		r := <-rowChan
		rows[r.Index] = r
	}

	// Write data rows (pipe_long inserted after pipe layer column)
	for i, r := range rows {
		row := i + 2
		f.SetCellValue(sheet, fmt.Sprintf("A%d", row), i+1)
		f.SetCellValue(sheet, fmt.Sprintf("B%d", row), r.PwaCode)
		f.SetCellValue(sheet, fmt.Sprintf("C%d", row), r.Name)
		f.SetCellValue(sheet, fmt.Sprintf("D%d", row), r.Zone)

		col := 5 // Start at column E
		for j, l := range layers {
			cell, _ := excelize.CoordinatesToCellName(col, row)
			f.SetCellValue(sheet, cell, r.Layers[l])
			col++
			if j == pipeColOffset {
				plCell, _ := excelize.CoordinatesToCellName(col, row)
				f.SetCellValue(sheet, plCell, r.PipeLong)
				col++
			}
		}
		totalCell, _ := excelize.CoordinatesToCellName(col, row)
		f.SetCellValue(sheet, totalCell, r.Total)
	}

	// Auto-fit column widths
	for i := range headers {
		col, _ := excelize.ColumnNumberToName(i + 1)
		f.SetColWidth(sheet, col, col, 15)
	}
	f.SetColWidth(sheet, "C", "C", 30)

	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", "attachment; filename=pwa_gis_summary.xlsx")

	if err := f.Write(c.Writer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

// DebugCollection returns debug info about a MongoDB collection for a branch.
// GET /api/debug/collection?pwaCode=xxx&layer=xxx
func DebugCollection(c *gin.Context) {
	pwaCode := c.Query("pwaCode")
	layer := c.DefaultQuery("layer", "pipe")
	if pwaCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pwaCode is required"})
		return
	}
	dbName := config.MongoDBName
	alias := fmt.Sprintf("b%s_%s", pwaCode, layer)
	collectionID, err := services.FindCollectionID(pwaCode, layer)
	result := gin.H{"database": dbName, "alias_query": alias, "collection_id": collectionID, "error": ""}
	if err != nil {
		result["error"] = err.Error()
	} else {
		count, _ := services.CountFeatures(pwaCode, layer, "", "")
		result["feature_count"] = count
	}
	collections, _ := services.GetCollectionsForPwaCode(pwaCode)
	result["available_collections"] = collections
	c.JSON(http.StatusOK, result)
}

// ExportGeoData exports features as GeoJSON (or other formats) for download.
// GET /api/export/geodata?pwaCode=xxx&collection=xxx&format=geojson&startDate=xxx&endDate=xxx
// Supported formats: geojson, gpkg, shp, fgb, mbtiles
func ExportGeoData(c *gin.Context) {
	pwaCode := c.Query("pwaCode")
	collection := c.Query("collection")
	startDate := c.Query("startDate")
	endDate := c.Query("endDate")
	format := c.DefaultQuery("format", "geojson")

	if pwaCode == "" || collection == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pwaCode and collection are required"})
		return
	}

	// Validate collection name
	validLayers := services.GetAllLayerNames()
	valid := false
	for _, l := range validLayers {
		if l == collection {
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

	geojsonData, err := services.ExportFeaturesAsGeoJSON(pwaCode, collection, startDate, endDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	filename := fmt.Sprintf("%s_%s", pwaCode, collection)

	switch format {
	case "geojson":
		c.Header("Content-Type", "application/geo+json")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.geojson", filename))
		c.Data(http.StatusOK, "application/geo+json", geojsonData)

	case "gpkg":
		// TODO: Implement with go-sqlite3 + GeoPackage SQL spec
		// For now, export as GeoJSON with note
		c.Header("Content-Type", "application/geo+json")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.geojson", filename))
		c.Data(http.StatusOK, "application/geo+json", geojsonData)

	case "shp":
		// TODO: Implement with jonas-p/go-shp
		c.Header("Content-Type", "application/geo+json")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.geojson", filename))
		c.Data(http.StatusOK, "application/geo+json", geojsonData)

	case "fgb":
		fgbData, fgbErr := services.ExportAsFlatGeobuf(pwaCode, collection, startDate, endDate)
		if fgbErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "FlatGeobuf export failed: " + fgbErr.Error()})
			return
		}
		c.Header("Content-Type", "application/octet-stream")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.fgb", filename))
		c.Data(http.StatusOK, "application/octet-stream", fgbData)

	case "mbtiles":
		// TODO: Implement with go-sqlite3 + MVT tile generation
		c.Header("Content-Type", "application/geo+json")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.geojson", filename))
		c.Data(http.StatusOK, "application/geo+json", geojsonData)

	default:
		c.Header("Content-Type", "application/geo+json")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.geojson", filename))
		c.Data(http.StatusOK, "application/geo+json", geojsonData)
	}
}

// GetFeaturesForMap returns lightweight GeoJSON for MapLibre map rendering.
// Only geometry + _id; properties are loaded on-demand via GetFeatureProps.
// GET /api/features/map?pwaCode=xxx&collection=xxx&startDate=xxx&endDate=xxx
func GetFeaturesForMap(c *gin.Context) {
	pwaCode := c.Query("pwaCode")
	collection := c.Query("collection")
	startDate := c.Query("startDate")
	endDate := c.Query("endDate")

	if pwaCode == "" || collection == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pwaCode and collection are required"})
		return
	}

	// Validate collection name
	validLayers := services.GetAllLayerNames()
	valid := false
	for _, l := range validLayers {
		if l == collection {
			valid = true
			break
		}
	}
	if !valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid collection"})
		return
	}

	data, err := services.ExportFeaturesForMap(pwaCode, collection, startDate, endDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Type", "application/geo+json")
	c.Data(http.StatusOK, "application/geo+json", data)
}

// GetFeatureProps returns full properties for a single feature (lazy-loaded on map click).
// GET /api/features/properties?pwaCode=xxx&collection=xxx&featureId=xxx
func GetFeatureProps(c *gin.Context) {
	pwaCode := c.Query("pwaCode")
	collection := c.Query("collection")
	featureID := c.Query("featureId")

	if pwaCode == "" || collection == "" || featureID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pwaCode, collection, and featureId are required"})
		return
	}

	props, err := services.GetFeatureProperties(pwaCode, collection, featureID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "success",
		"feature_id": featureID,
		"properties": props,
	})
}