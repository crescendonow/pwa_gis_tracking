package mapsync

import (
	"testing"
	"time"
)

func TestTransformFeaturePreservesMapIdentityAndGeometry(t *testing.T) {
	collection, err := ParseCollectionAlias("b2101_dma_boundary", "abc")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	feature, err := TransformFeature(collection, SourceFeature{ID: "mongo-id", Geometry: map[string]any{"type": "Point", "coordinates": []float64{100.5, 13.7}}, Properties: map[string]any{"dma_id": "D1", "recordDate": "2026-08-20"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if feature.PwaCode != "2101" || feature.Layer != "dma_boundary" || feature.SourceID != "mongo-id" {
		t.Fatalf("unexpected identity: %#v", feature)
	}
	if string(feature.GeometryJSON) != `{"coordinates":[100.5,13.7],"type":"Point"}` {
		t.Fatalf("geometry changed: %s", feature.GeometryJSON)
	}
}

func TestNormalizeDMAColorsUsesFallbackForUnsafeValues(t *testing.T) {
	fill, stroke := NormalizeDMAColors("url(javascript:bad)", "#ABCDEF")
	if fill != DefaultDMAFill || stroke != "#ABCDEF" {
		t.Fatalf("got %q, %q", fill, stroke)
	}
	fill, _ = NormalizeDMAColors("notacolor", "red")
	if fill != DefaultDMAFill {
		t.Fatalf("unknown CSS name should use fallback, got %q", fill)
	}
}

func TestTransformFeatureUsesFeaturePwaCodeBeforeCollectionAlias(t *testing.T) {
	collection, err := ParseCollectionAlias("b5511000_dma_boundary", "abc")
	if err != nil {
		t.Fatal(err)
	}
	feature, err := TransformFeature(collection, SourceFeature{
		ID:         "mongo-id",
		Geometry:   map[string]any{"type": "Point", "coordinates": []float64{100.5, 13.7}},
		Properties: map[string]any{"pwaCode": "5511011", "dmaId": 52},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if feature.PwaCode != "5511011" {
		t.Fatalf("PwaCode = %q, want feature pwaCode", feature.PwaCode)
	}
}
