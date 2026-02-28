package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"pwa_gis_tracking/services"

	"github.com/gin-gonic/gin"
)

// GetFeaturesList returns paginated features with field-mapped properties.
// Dynamic columns are derived from FieldMapping in field_mapping.go.
//
// GET /api/features/list?pwaCode=xxx&collection=pipe&page=1&pageSize=50&search=xxx&startDate=xxx&endDate=xxx
func GetFeaturesList(c *gin.Context) {
	pwaCode := c.Query("pwaCode")
	collection := c.Query("collection")
	startDate := c.Query("startDate")
	endDate := c.Query("endDate")
	search := c.Query("search")

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "50"))

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

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	result, err := services.ListFeaturesPaginated(pwaCode, collection, startDate, endDate, search, page, pageSize)
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

// GetFieldMapping returns the field mapping definition for a given collection.
// GET /api/field-mapping?collection=pipe
func GetFieldMapping(c *gin.Context) {
	collection := c.Query("collection")
	if collection == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "collection is required"})
		return
	}

	mapping, exists := services.FieldMapping[collection]
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{
			"error":      "No field mapping found for collection: " + collection,
			"collection": collection,
		})
		return
	}

	columns := []map[string]string{}
	for mongoKey, pgKey := range mapping {
		if pgKey == "password" {
			continue
		}
		columns = append(columns, map[string]string{
			"key":       pgKey,
			"mongo_key": mongoKey,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "success",
		"collection": collection,
		"mapping":    mapping,
		"columns":    columns,
	})
}