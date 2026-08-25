package services

import (
	"errors"
	"testing"

	"pwa_gis_tracking/config"
)

func TestMapPwaCodesDerivesServerScope(t *testing.T) {
	offices := func(zone string) ([]string, error) {
		if zone != "5" {
			return nil, errors.New("unexpected zone")
		}
		return []string{"5511011", "5511001", "5511011"}, nil
	}
	cases := []struct {
		name, scope, branch, zone, want string
	}{
		{name: "all", scope: ScopeAll, want: "*"},
		{name: "region", scope: ScopeRegion, zone: "5", want: "5511001,5511011"},
		{name: "branch", scope: ScopeBranch, branch: "5511011", want: "5511011"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MapPwaCodes(tc.scope, tc.branch, tc.zone, offices)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("codes = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMapPwaCodesRejectsMissingBranch(t *testing.T) {
	if _, err := MapPwaCodes(ScopeBranch, "", "", nil); err == nil {
		t.Fatal("expected missing branch to be rejected")
	}
}

func TestIsMapLayerUsesExplicitAllowList(t *testing.T) {
	if !IsMapLayer("meter") {
		t.Fatal("meter should be allowed")
	}
	if IsMapLayer("../../catalog") {
		t.Fatal("unknown layer should be rejected")
	}
	if IsMapLayerAtZoom("meter", 13) || !IsMapLayerAtZoom("meter", 14) {
		t.Fatal("meter must be enforced from zoom 14")
	}
}

func TestNarrowMapPwaCodesCannotWidenScope(t *testing.T) {
	if got, err := NarrowMapPwaCodes("5511001,5511011", "5511011"); err != nil || got != "5511011" {
		t.Fatalf("narrow result = %q, %v", got, err)
	}
	if _, err := NarrowMapPwaCodes("5511001,5511011", "9999999"); err == nil {
		t.Fatal("out-of-scope branch should be rejected")
	}
	if _, err := NarrowMapPwaCodes("*", "*"); err == nil {
		t.Fatal("browser wildcard should be rejected")
	}
}

func TestLoadMapSummaryUsesMapDatabaseAndFallsBackWhenUnavailable(t *testing.T) {
	previousPg, previousMap := config.PgDB, config.MapDB
	config.PgDB, config.MapDB = nil, nil
	t.Cleanup(func() { config.PgDB, config.MapDB = previousPg, previousMap })

	if _, _, err := LoadMapSummary(ScopeAll, "", "", nil); err == nil {
		t.Fatal("expected an error when neither MapDB nor PgDB is configured")
	}
}

func TestNarrowMapPwaCodesByZoneIntersectsAuthenticatedScope(t *testing.T) {
	offices := func(zone string) ([]string, error) {
		if zone != "5" {
			return nil, errors.New("unexpected zone")
		}
		return []string{"5511001", "5511011"}, nil
	}
	if got, err := NarrowMapPwaCodesByZone("*", "5", offices); err != nil || got != "5511001,5511011" {
		t.Fatalf("all-scope zone = %q, %v", got, err)
	}
	if got, err := NarrowMapPwaCodesByZone("5511011,6611001", "5", offices); err != nil || got != "5511011" {
		t.Fatalf("intersection = %q, %v", got, err)
	}
	if _, err := NarrowMapPwaCodesByZone("6611001", "5", offices); err == nil {
		t.Fatal("empty zone intersection must be rejected")
	}
}
