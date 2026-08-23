package mapsync

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var collectionAlias = regexp.MustCompile(`^b?([^_]+)_(.+)$`)

const (
	DefaultDMAFill   = "#2f80ed"
	DefaultDMAStroke = "#1f4f8a"
)

// ParseCollectionAlias converts Mongo metadata aliases (b{pwaCode}_{layer})
// into the key used by the mirror. It rejects ambiguous aliases early.
func ParseCollectionAlias(alias, id string) (Collection, error) {
	matches := collectionAlias.FindStringSubmatch(strings.TrimSpace(alias))
	if len(matches) != 3 || matches[1] == "" || matches[2] == "" {
		return Collection{}, fmt.Errorf("invalid collection alias %q", alias)
	}
	return Collection{Alias: alias, ID: id, PwaCode: matches[1], Layer: matches[2]}, nil
}

// TransformFeature validates and normalises the Mongo document without losing
// its stable identity or GeoJSON geometry.
func TransformFeature(collection Collection, source SourceFeature, syncedAt time.Time) (MirrorFeature, error) {
	if collection.Alias == "" || collection.PwaCode == "" || collection.Layer == "" {
		return MirrorFeature{}, fmt.Errorf("collection is incomplete")
	}
	if strings.TrimSpace(source.ID) == "" {
		return MirrorFeature{}, fmt.Errorf("feature in %s has no source id", collection.Alias)
	}
	geometry, err := json.Marshal(source.Geometry)
	if err != nil || !json.Valid(geometry) {
		return MirrorFeature{}, fmt.Errorf("feature %s has invalid geometry", source.ID)
	}
	var geoJSON struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
		Geometries  json.RawMessage `json:"geometries"`
	}
	if err := json.Unmarshal(geometry, &geoJSON); err != nil || geoJSON.Type == "" || (len(geoJSON.Coordinates) == 0 && len(geoJSON.Geometries) == 0) {
		return MirrorFeature{}, fmt.Errorf("feature %s geometry is not GeoJSON", source.ID)
	}

	properties := cloneJSONValues(source.Properties)
	return MirrorFeature{
		SourceCollection: collection.Alias,
		Layer:            collection.Layer,
		PwaCode:          featurePwaCode(properties, collection.PwaCode),
		SourceID:         source.ID,
		GeometryJSON:     geometry,
		Properties:       properties,
		SourceCreatedAt:  source.CreatedAt,
		SourceUpdatedAt:  source.UpdatedAt,
		SyncedAt:         syncedAt.UTC(),
	}, nil
}

func featurePwaCode(properties map[string]any, fallback string) string {
	for _, key := range []string{"pwaCode", "pwa_code", "PWA_CODE"} {
		if value, exists := properties[key]; exists {
			code := strings.TrimSpace(fmt.Sprint(value))
			if code != "" && code != "<nil>" {
				return code
			}
		}
	}
	return fallback
}

func cloneJSONValues(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	copy := make(map[string]any, len(values))
	for key, value := range values {
		copy[key] = normaliseJSONValue(value)
	}
	return copy
}

func normaliseJSONValue(value any) any {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	case map[string]any:
		return cloneJSONValues(typed)
	case []any:
		result := make([]any, len(typed))
		for i, entry := range typed {
			result[i] = normaliseJSONValue(entry)
		}
		return result
	default:
		return value
	}
}

var cssColor = regexp.MustCompile(`(?i)^(#[0-9a-f]{3}([0-9a-f]{3})?([0-9a-f]{2})?|rgba?\([0-9.,% ]+\)|hsla?\([0-9.,% ]+\)|(black|white|red|green|blue|yellow|orange|purple|gray|grey|cyan|magenta|transparent))$`)

// NormalizeDMAColors enforces the same browser-safe fallback policy as the SQL
// enrichment function. It is public so adapters can validate values before logs
// or previews expose them.
func NormalizeDMAColors(fill, stroke string) (string, string) {
	if !cssColor.MatchString(strings.TrimSpace(fill)) {
		fill = DefaultDMAFill
	}
	if !cssColor.MatchString(strings.TrimSpace(stroke)) {
		stroke = DefaultDMAStroke
	}
	return fill, stroke
}
