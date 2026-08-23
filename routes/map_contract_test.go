package routes_test

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestMapPageRendersRequiredSidebarControls(t *testing.T) {
	router := setupTestRouter(t)
	recorder := performRequest(router, http.MethodGet, basePath+"/map", authenticatedCookies(t)...)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%q", recorder.Code, recorder.Body.String())
	}
	for _, required := range []string{"id=\"overviewMap\"", "id=\"quickSearch\"", "id=\"projectLayers\"", "id=\"terrainToggle\"", "id=\"aiForm\"", "/static/js/map.js"} {
		if !strings.Contains(recorder.Body.String(), required) {
			t.Errorf("map page is missing %q", required)
		}
	}
}

func TestMapTileRouteStillRequiresAuthentication(t *testing.T) {
	router := setupTestRouter(t)
	recorder := performRequest(router, http.MethodGet, basePath+"/api/map/tiles/meter/14/13250/7560")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestMapFrontendKeepsProgressiveAndDMAContracts(t *testing.T) {
	ensureRepoRoot(t)
	contents, err := os.ReadFile("static/js/map.js")
	if err != nil {
		t.Fatal(err)
	}
	javascript := string(contents)
	for _, required := range []string{
		`{ id: "meter", label: "มาตรวัดน้ำ", minzoom: 14`,
		`["get", "sld_color_fill"]`,
		`["get", "sld_color_stroke"]`,
		`server.arcgisonline.com/ArcGIS/rest/services/World_Imagery`,
		`tiles.openfreemap.org/planet`,
		`type: "symbol", minzoom: 17`,
		`params.set("zone", zone)`,
		`payload.response_type === "geojson"`,
		`payload.text_response`,
	} {
		if !strings.Contains(javascript, required) {
			t.Errorf("map.js is missing contract %q", required)
		}
	}
}

func TestEveryPageUsesPWAFavicon(t *testing.T) {
	ensureRepoRoot(t)
	for _, name := range []string{"login.html", "dashboard.html", "detail.html", "map.html"} {
		contents, err := os.ReadFile("templates/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(contents), `/pwa_gis_tracking/static/images/pwa-logo.jpg`) {
			t.Errorf("%s does not use the PWA favicon", name)
		}
	}
}
