package mapsync

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// HTTPWarmer requests internal Martin tiles with bounded concurrency. Martin is
// loopback-only; this component never exposes its URL to browsers.
type HTTPWarmer struct {
	BaseURL       string
	Source        string
	Client        *http.Client
	MaxConcurrent int
}

func (warmer HTTPWarmer) Warm(ctx context.Context, targets []WarmTarget) error {
	base, err := url.Parse(strings.TrimRight(warmer.BaseURL, "/"))
	if err != nil {
		return err
	}
	client := warmer.Client
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	maxConcurrent := warmer.MaxConcurrent
	if maxConcurrent < 1 {
		maxConcurrent = 4
	}
	sem := make(chan struct{}, maxConcurrent)
	var group sync.WaitGroup
	var once sync.Once
	var firstErr error
	for _, target := range targets {
		for _, layer := range warmLayers(target.Z) {
			target, layer := target, layer
			group.Add(1)
			go func() {
				defer group.Done()
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-sem }()
				endpoint := *base
				endpoint.Path = fmt.Sprintf("%s/%s/%d/%d/%d", strings.TrimRight(base.Path, "/"), warmer.Source, target.Z, target.X, target.Y)
				query := endpoint.Query()
				query.Set("layer_name", layer)
				query.Set("pwa_codes", "*")
				query.Set("start_date", "")
				query.Set("end_date", "")
				endpoint.RawQuery = query.Encode()
				request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
				if err != nil {
					once.Do(func() { firstErr = err })
					return
				}
				response, err := client.Do(request)
				if err == nil {
					_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
					response.Body.Close()
					if response.StatusCode < 200 || response.StatusCode >= 300 {
						err = fmt.Errorf("martin warm %s: %s", endpoint.Path, response.Status)
					}
				}
				if err != nil {
					once.Do(func() { firstErr = err })
				}
			}()
		}
	}
	group.Wait()
	return firstErr
}

func warmLayers(zoom int) []string {
	layers := []string{"pwa_waterworks", "dma_boundary"}
	if zoom >= 10 {
		layers = append(layers, "pipe", "pipe_serv")
	}
	return layers
}
