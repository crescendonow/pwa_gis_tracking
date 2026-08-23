package mapsync

import (
	"testing"
	"time"
)

func TestNeedsReconciliation(t *testing.T) {
	now := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	if !NeedsReconciliation(now, nil) {
		t.Fatal("first run should reconcile")
	}
	recent := now.Add(-23 * time.Hour)
	if NeedsReconciliation(now, &recent) {
		t.Fatal("recent full sync should not reconcile")
	}
	due := now.Add(-24 * time.Hour)
	if !NeedsReconciliation(now, &due) {
		t.Fatal("24 hour sync should reconcile")
	}
}

func TestWarmProfileCoversRequiredZoomsWithoutDuplicates(t *testing.T) {
	targets := WarmProfile([]Bounds{{West: 99.5, South: 13.0, East: 101.0, North: 14.5}})
	seen := map[WarmTarget]bool{}
	zooms := map[int]bool{}
	for _, target := range targets {
		if seen[target] {
			t.Fatalf("duplicate target: %#v", target)
		}
		seen[target] = true
		zooms[target.Z] = true
	}
	for zoom := 5; zoom <= 10; zoom++ {
		if !zooms[zoom] {
			t.Fatalf("missing zoom %d", zoom)
		}
	}
}
