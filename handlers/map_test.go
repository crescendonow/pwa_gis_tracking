package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetMapTileForwardsOnlyServerDerivedBranch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamQuery url.Values
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
		_, _ = w.Write([]byte("mvt"))
	}))
	defer upstream.Close()
	t.Setenv("MARTIN_URL", upstream.URL)
	previousClient := mapHTTPClient
	mapHTTPClient = upstream.Client()
	t.Cleanup(func() { mapHTTPClient = previousClient })

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/?pwa_codes=*&start_date=2026-08-01", nil)
	context.Params = gin.Params{{Key: "layer", Value: "meter"}, {Key: "z", Value: "14"}, {Key: "x", Value: "13250"}, {Key: "y", Value: "7560"}}
	context.Set("uid", "ordinary-user")
	context.Set("permission_leak", "branch")
	context.Set("pwacode", "5511011")
	context.Set("area", "5")

	GetMapTile(context)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%q", recorder.Code, recorder.Body.String())
	}
	if got := upstreamQuery.Get("pwa_codes"); got != "5511011" {
		t.Fatalf("upstream pwa_codes = %q, want server branch", got)
	}
	if upstreamQuery.Get("layer_name") != "meter" || upstreamQuery.Get("start_date") != "2026-08-01" {
		t.Fatalf("unexpected upstream query: %v", upstreamQuery)
	}
}

func TestGetMapTileRejectsUnknownLayerBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	context.Params = gin.Params{{Key: "layer", Value: "catalog"}, {Key: "z", Value: "5"}, {Key: "x", Value: "25"}, {Key: "y", Value: "14"}}
	GetMapTile(context)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestGetMapTileRejectsMeterBelowProgressiveZoomBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	t.Setenv("MARTIN_URL", upstream.URL)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	context.Params = gin.Params{{Key: "layer", Value: "meter"}, {Key: "z", Value: "13"}, {Key: "x", Value: "6600"}, {Key: "y", Value: "3700"}}
	GetMapTile(context)
	if recorder.Code != http.StatusBadRequest || requests != 0 {
		t.Fatalf("status = %d, upstream requests = %d", recorder.Code, requests)
	}
}

func TestGetMapTileReturnsNoContentWhenMartinHasEmptyTile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	t.Setenv("MARTIN_URL", upstream.URL)
	previousClient := mapHTTPClient
	mapHTTPClient = upstream.Client()
	t.Cleanup(func() { mapHTTPClient = previousClient })

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	context.Params = gin.Params{{Key: "layer", Value: "meter"}, {Key: "z", Value: "14"}, {Key: "x", Value: "13250"}, {Key: "y", Value: "7560"}}
	context.Set("permission_leak", "all")

	GetMapTile(context)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty on 204", recorder.Body.String())
	}
}

func TestGetMapTileReturnsNoContentWhenMartinHasNoSourceForTheTile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer upstream.Close()
	t.Setenv("MARTIN_URL", upstream.URL)
	previousClient := mapHTTPClient
	mapHTTPClient = upstream.Client()
	t.Cleanup(func() { mapHTTPClient = previousClient })

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	context.Params = gin.Params{{Key: "layer", Value: "meter"}, {Key: "z", Value: "14"}, {Key: "x", Value: "13250"}, {Key: "y", Value: "7560"}}
	context.Set("permission_leak", "all")

	GetMapTile(context)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestGetMapTileReturnsBadGatewayWhenMartinFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()
	t.Setenv("MARTIN_URL", upstream.URL)
	previousClient := mapHTTPClient
	mapHTTPClient = upstream.Client()
	t.Cleanup(func() { mapHTTPClient = previousClient })

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	context.Params = gin.Params{{Key: "layer", Value: "meter"}, {Key: "z", Value: "14"}, {Key: "x", Value: "13250"}, {Key: "y", Value: "7560"}}
	context.Set("permission_leak", "all")

	GetMapTile(context)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestGetMapTileAppliesAllScopeZoneFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamQuery url.Values
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamQuery = r.URL.Query()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	t.Setenv("MARTIN_URL", upstream.URL)
	previousClient, previousOffices := mapHTTPClient, mapOfficesByZone
	mapHTTPClient = upstream.Client()
	mapOfficesByZone = func(zone string) ([]string, error) { return []string{"5511011", "5511001"}, nil }
	t.Cleanup(func() { mapHTTPClient, mapOfficesByZone = previousClient, previousOffices })

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/?zone=5", nil)
	context.Params = gin.Params{{Key: "layer", Value: "dma_boundary"}, {Key: "z", Value: "5"}, {Key: "x", Value: "25"}, {Key: "y", Value: "14"}}
	context.Set("permission_leak", "all")
	GetMapTile(context)
	if recorder.Code != http.StatusOK || upstreamQuery.Get("pwa_codes") != "5511001,5511011" {
		t.Fatalf("status = %d, query = %v", recorder.Code, upstreamQuery)
	}
}
