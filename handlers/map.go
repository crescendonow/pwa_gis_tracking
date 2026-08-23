package handlers

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"pwa_gis_tracking/services"
)

var mapHTTPClient = &http.Client{Timeout: 20 * time.Second}
var mapSummaryLoader = services.LoadMapSummary
var mapOfficesByZone = func(zone string) ([]string, error) {
	offices, err := services.GetOfficesByZone(zone)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(offices))
	for _, office := range offices {
		out = append(out, office.PwaCode)
	}
	return out, nil
}

// GetMapConfig returns browser-safe configuration only: never internal URLs or keys.
func GetMapConfig(c *gin.Context) {
	dem := os.Getenv("MAP_DEM_TILEJSON_URL")
	if dem == "" {
		dem = "https://demotiles.maplibre.org/terrain-tiles/tiles.json"
	}
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("MAP_SATELLITE_PROVIDER")))
	googleTemplate := strings.TrimSpace(os.Getenv("MAP_GOOGLE_TILE_PROXY_TEMPLATE"))
	googleEnabled := provider == "google" && strings.HasPrefix(googleTemplate, "/")
	if !googleEnabled {
		provider = "esri-osm"
		googleTemplate = ""
	}
	c.JSON(http.StatusOK, gin.H{
		"status":                     "success",
		"dem_tilejson_url":           dem,
		"satellite_provider":         provider,
		"google_enabled":             googleEnabled,
		"google_tile_proxy_template": googleTemplate,
		"map_scope":                  mapScope(c),
	})
}

func GetMapSummary(c *gin.Context) {
	scope := mapScope(c)
	summaries, updatedAt, err := mapSummaryLoader(scope, mapContextString(c, "pwacode"), mapContextString(c, "area"), mapOfficesByZone)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "error": "map summary unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "success", "scope": scope, "layers": summaries, "updated_at": updatedAt})
}

func GetMapTile(c *gin.Context) {
	layer := c.Param("layer")
	z, zErr := strconv.Atoi(c.Param("z"))
	x, xErr := strconv.Atoi(c.Param("x"))
	y, yErr := strconv.Atoi(c.Param("y"))
	if zErr != nil || xErr != nil || yErr != nil || !services.IsMapLayerAtZoom(layer, z) || x < 0 || y < 0 || x >= 1<<z || y >= 1<<z {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tile"})
		return
	}
	start, end := firstMapQuery(c, "start_date", "startDate"), firstMapQuery(c, "end_date", "endDate")
	if !validMapDate(start) || !validMapDate(end) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date"})
		return
	}
	codes, err := services.MapPwaCodes(mapScope(c), mapContextString(c, "pwacode"), mapContextString(c, "area"), mapOfficesByZone)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "map scope unavailable"})
		return
	}
	codes, err = services.NarrowMapPwaCodesByZone(codes, c.Query("zone"), mapOfficesByZone)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "zone is outside map scope"})
		return
	}
	codes, err = services.NarrowMapPwaCodes(codes, c.Query("pwa_code"))
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "branch is outside map scope"})
		return
	}
	base := strings.TrimRight(os.Getenv("MARTIN_URL"), "/")
	if base == "" {
		base = "http://127.0.0.1:5031"
	}
	q := url.Values{"layer_name": {layer}, "pwa_codes": {codes}, "start_date": {start}, "end_date": {end}}
	request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, base+"/pwa_gis_map_tile/"+strconv.Itoa(z)+"/"+strconv.Itoa(x)+"/"+strconv.Itoa(y)+"?"+q.Encode(), nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "tile service unavailable"})
		return
	}
	response, err := mapHTTPClient.Do(request)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "tile service unavailable"})
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": "tile service unavailable"})
		return
	}
	contentType := response.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/vnd.mapbox-vector-tile"
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "private, max-age=60")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, response.Body)
}

func mapScope(c *gin.Context) string {
	return services.MapScopeForUser(mapContextString(c, "uid"), mapContextString(c, "permission_leak"))
}
func mapContextString(c *gin.Context, key string) string {
	value, _ := c.Get(key)
	valueString, _ := value.(string)
	return valueString
}
func validMapDate(value string) bool {
	if value == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}
func firstMapQuery(c *gin.Context, keys ...string) string {
	for _, key := range keys {
		if value := c.Query(key); value != "" {
			return value
		}
	}
	return ""
}
