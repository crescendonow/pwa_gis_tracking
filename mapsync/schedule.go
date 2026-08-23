package mapsync

import (
	"math"
	"time"
)

const (
	IncrementalInterval      = 15 * time.Minute
	ReconcileInterval        = 24 * time.Hour
	IncrementalReplayOverlap = time.Minute
)

// NeedsReconciliation is kept deterministic for callers and tests. A missing
// successful full sync means the first cycle must reconcile.
func NeedsReconciliation(now time.Time, lastFullSync *time.Time) bool {
	return lastFullSync == nil || !now.Before(lastFullSync.Add(ReconcileInterval))
}

// Bounds is longitude/latitude bounds used by the warming profile.
type Bounds struct{ West, South, East, North float64 }

// WarmTarget is one vector-tile coordinate to request from loopback Martin.
type WarmTarget struct{ Z, X, Y int }

var thailandBounds = Bounds{West: 97.0, South: 5.0, East: 106.0, North: 21.5}

// WarmProfile covers nationwide tiles at z5-z7 and the authoritative PWA-zone
// bounds supplied from PostGIS at z8-z10. Duplicate edge tiles are removed.
func WarmProfile(zoneBounds []Bounds) []WarmTarget {
	seen := make(map[WarmTarget]struct{})
	targets := make([]WarmTarget, 0)
	add := func(bounds Bounds, minimumZoom, maximumZoom int) {
		for zoom := minimumZoom; zoom <= maximumZoom; zoom++ {
			minX, maxY := lonLatTile(bounds.West, bounds.South, zoom)
			maxX, minY := lonLatTile(bounds.East, bounds.North, zoom)
			for x := minX; x <= maxX; x++ {
				for y := minY; y <= maxY; y++ {
					target := WarmTarget{Z: zoom, X: x, Y: y}
					if _, exists := seen[target]; !exists {
						seen[target] = struct{}{}
						targets = append(targets, target)
					}
				}
			}
		}
	}
	add(thailandBounds, 5, 7)
	for _, bounds := range zoneBounds {
		add(bounds, 8, 10)
	}
	return targets
}

func lonLatTile(longitude, latitude float64, zoom int) (int, int) {
	n := math.Exp2(float64(zoom))
	x := int(math.Floor((longitude + 180) / 360 * n))
	latRadians := latitude * math.Pi / 180
	y := int(math.Floor((1 - math.Asinh(math.Tan(latRadians))/math.Pi) / 2 * n))
	max := int(n) - 1
	if x < 0 {
		x = 0
	}
	if x > max {
		x = max
	}
	if y < 0 {
		y = 0
	}
	if y > max {
		y = max
	}
	return x, y
}
