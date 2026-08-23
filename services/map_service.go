package services

import (
	"errors"
	"sort"
	"strings"
	"time"

	"pwa_gis_tracking/config"
)

var mapLayerMinimumZoom = map[string]int{
	"pwa_waterworks": 5,
	"dma_boundary":   5,
	"pipe":           10,
	"pipe_serv":      10,
	"bldg":           14,
	"struct":         14,
	"valve":          14,
	"firehydrant":    14,
	"leakpoint":      14,
	"step_test":      14,
	"meter":          14,
	"flow_meter":     14,
}

// IsMapLayer is the server-side allow-list shared by the map proxy.
func IsMapLayer(layer string) bool {
	_, ok := mapLayerMinimumZoom[layer]
	return ok
}

// IsMapLayerAtZoom applies the progressive-loading contract at the trusted
// boundary, so a crafted browser request cannot force deep data at low zoom.
func IsMapLayerAtZoom(layer string, zoom int) bool {
	minimum, ok := mapLayerMinimumZoom[layer]
	return ok && zoom >= minimum && zoom <= 22
}

// MapPwaCodes converts an authenticated scope into Martin's pwa_codes value.
// Browser-supplied branch lists are deliberately not part of this interface.
func MapPwaCodes(scope, branch, zone string, officesByZone func(string) ([]string, error)) (string, error) {
	switch scope {
	case ScopeAll:
		return "*", nil
	case ScopeRegion:
		if strings.TrimSpace(zone) == "" || officesByZone == nil {
			return "", errors.New("region is unavailable")
		}
		codes, err := officesByZone(zone)
		if err != nil {
			return "", err
		}
		return joinMapCodes(codes)
	case ScopeBranch:
		return joinMapCodes([]string{branch})
	default:
		return "", errors.New("invalid map scope")
	}
}

// NarrowMapPwaCodes applies an optional UI branch filter without ever widening
// the authenticated code set. ScopeAll may select one syntactically safe code.
func NarrowMapPwaCodes(allowed, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return allowed, nil
	}
	if requested == "*" || len(requested) > 20 {
		return "", errors.New("invalid requested PWA code")
	}
	for _, char := range requested {
		if (char < '0' || char > '9') && (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && char != '-' {
			return "", errors.New("invalid requested PWA code")
		}
	}
	if allowed == "*" {
		return requested, nil
	}
	for _, code := range strings.Split(allowed, ",") {
		if code == requested {
			return requested, nil
		}
	}
	return "", errors.New("requested PWA code is outside map scope")
}

// NarrowMapPwaCodesByZone resolves a UI zone through the authoritative office
// directory and intersects it with the authenticated code set.
func NarrowMapPwaCodesByZone(allowed, requestedZone string, officesByZone func(string) ([]string, error)) (string, error) {
	requestedZone = strings.TrimSpace(requestedZone)
	if requestedZone == "" {
		return allowed, nil
	}
	if len(requestedZone) > 4 || officesByZone == nil {
		return "", errors.New("invalid requested zone")
	}
	for _, char := range requestedZone {
		if char < '0' || char > '9' {
			return "", errors.New("invalid requested zone")
		}
	}
	zoneCodes, err := officesByZone(requestedZone)
	if err != nil {
		return "", err
	}
	zoneAllowed, err := joinMapCodes(zoneCodes)
	if err != nil {
		return "", err
	}
	if allowed == "*" {
		return zoneAllowed, nil
	}
	allowedSet := make(map[string]struct{})
	for _, code := range strings.Split(allowed, ",") {
		allowedSet[code] = struct{}{}
	}
	intersection := make([]string, 0)
	for _, code := range strings.Split(zoneAllowed, ",") {
		if _, ok := allowedSet[code]; ok {
			intersection = append(intersection, code)
		}
	}
	return joinMapCodes(intersection)
}

func joinMapCodes(codes []string) (string, error) {
	unique := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code != "" && code != "*" {
			unique[code] = struct{}{}
		}
	}
	if len(unique) == 0 {
		return "", errors.New("no PWA code is available")
	}
	ordered := make([]string, 0, len(unique))
	for code := range unique {
		ordered = append(ordered, code)
	}
	sort.Strings(ordered)
	return strings.Join(ordered, ","), nil
}

// MapLayerSummary is the small cached metadata contract used by the sidebar.
type MapLayerSummary struct {
	Layer        string `json:"layer"`
	FeatureCount int64  `json:"feature_count"`
}

// LoadMapSummary reads the pre-aggregated mirror table within server-derived RBAC.
func LoadMapSummary(scope, branch, zone string, officesByZone func(string) ([]string, error)) ([]MapLayerSummary, *time.Time, error) {
	codes, err := MapPwaCodes(scope, branch, zone, officesByZone)
	if err != nil {
		return nil, nil, err
	}
	if config.PgDB == nil {
		return nil, nil, errors.New("PostGIS is unavailable")
	}
	rows, err := config.PgDB.Query(`
		SELECT layer, SUM(feature_count), MAX(updated_at)
		FROM pwa_tracking_map.map_summary
		WHERE $1 = '*' OR pwa_code = ANY(string_to_array($1, ','))
		GROUP BY layer
		ORDER BY layer
	`, codes)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	summaries := make([]MapLayerSummary, 0)
	var latest *time.Time
	for rows.Next() {
		var summary MapLayerSummary
		var updated time.Time
		if err := rows.Scan(&summary.Layer, &summary.FeatureCount, &updated); err != nil {
			return nil, nil, err
		}
		updated = updated.UTC()
		if latest == nil || updated.After(*latest) {
			value := updated
			latest = &value
		}
		summaries = append(summaries, summary)
	}
	return summaries, latest, rows.Err()
}
