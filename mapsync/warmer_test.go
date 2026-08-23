package mapsync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestHTTPWarmerMatchesDefaultBrowserTileQueries(t *testing.T) {
	var mutex sync.Mutex
	requests := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		requests = append(requests, request.URL.String())
		mutex.Unlock()
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	warmer := HTTPWarmer{BaseURL: server.URL, Source: "pwa_gis_map_tile", Client: server.Client(), MaxConcurrent: 2}
	if err := warmer.Warm(context.Background(), []WarmTarget{{Z: 5, X: 25, Y: 14}, {Z: 10, X: 800, Y: 470}}); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 6 {
		t.Fatalf("requests = %d, want 2 overview + 4 detailed", len(requests))
	}
	for _, value := range requests {
		request, err := http.NewRequest(http.MethodGet, server.URL+value, nil)
		if err != nil {
			t.Fatal(err)
		}
		query := request.URL.Query()
		if query.Get("layer_name") == "" || query.Get("pwa_codes") != "*" {
			t.Fatalf("warm query does not match browser contract: %s", value)
		}
	}
}

func TestHTTPWarmerRejectsNonSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusNotFound)
		_, _ = response.Write([]byte("missing source"))
	}))
	defer server.Close()
	warmer := HTTPWarmer{BaseURL: server.URL, Source: "wrong-source", Client: server.Client(), MaxConcurrent: 1}
	if err := warmer.Warm(context.Background(), []WarmTarget{{Z: 5, X: 25, Y: 14}}); err == nil {
		t.Fatal("404 response must fail cache warming")
	}
}
