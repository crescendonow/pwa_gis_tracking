package services

import "testing"

// TestResolvePermission covers the scope (permission_leak) and download_tier
// decision table from note/14_plan_for_requirements_login_authen.md §4.12.
func TestResolvePermission(t *testing.T) {
	cases := []struct {
		name      string
		input     PermissionInput
		wantScope string
		wantTier  string
	}{
		{
			name:      "HQ department -> all/full",
			input:     PermissionInput{DepName: "สำนักควบคุมน้ำสูญเสีย"},
			wantScope: ScopeAll,
			wantTier:  TierFull,
		},
		{
			name:      "นักวิชาการภูมิสารสนเทศ -> branch/full",
			input:     PermissionInput{Position: "นักวิชาการภูมิสารสนเทศ 6"},
			wantScope: ScopeBranch,
			wantTier:  TierFull,
		},
		{
			name: "หัวหน้างาน + งานแผนที่แนวท่อ -> reg/full",
			input: PermissionInput{
				Position: "หัวหน้างาน 7",
				JobName:  "งานแผนที่แนวท่อ",
			},
			wantScope: ScopeRegion,
			wantTier:  TierFull,
		},
		{
			name:      "ผอ.กอง -> branch/full",
			input:     PermissionInput{Position: "ผู้อำนวยการกองระบบจำหน่าย"},
			wantScope: ScopeBranch,
			wantTier:  TierFull,
		},
		{
			name:      "ผจก. -> branch/full",
			input:     PermissionInput{Position: "ผจก.กปภ.สาขาบางบัวทอง"},
			wantScope: ScopeBranch,
			wantTier:  TierFull,
		},
		{
			name:      "พนักงานพัสดุ -> branch/basic",
			input:     PermissionInput{Position: "พนักงานพัสดุ 4"},
			wantScope: ScopeBranch,
			wantTier:  TierBasic,
		},
		{
			name: "ช่างโยธา + งานบริการและควบคุมน้ำสูญเสีย -> branch/basic",
			input: PermissionInput{
				Position: "ช่างโยธา 5",
				JobName:  "งานบริการและควบคุมน้ำสูญเสีย",
			},
			wantScope: ScopeBranch,
			wantTier:  TierBasic,
		},
		{
			name:      "explicit user ID 14180 -> all/full",
			input:     PermissionInput{UserID: "14180"},
			wantScope: ScopeAll,
			wantTier:  TierFull,
		},
		{
			name: "water-loss job overrides broader department -> reg/full",
			input: PermissionInput{
				DepName: "สำนักควบคุมน้ำสูญเสีย",
				JobName: "งานน้ำสูญเสีย",
			},
			wantScope: ScopeRegion,
			wantTier:  TierFull,
		},
		{
			name:      "all fields empty -> branch/basic",
			input:     PermissionInput{},
			wantScope: ScopeBranch,
			wantTier:  TierBasic,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolvePermission(tc.input)
			if got.Permission != "leak" {
				t.Errorf("Permission = %q, want %q", got.Permission, "leak")
			}
			if got.PermissionLeak != tc.wantScope {
				t.Errorf("PermissionLeak = %q, want %q", got.PermissionLeak, tc.wantScope)
			}
			if got.DownloadTier != tc.wantTier {
				t.Errorf("DownloadTier = %q, want %q", got.DownloadTier, tc.wantTier)
			}
		})
	}
}

// TestIsFormatAllowed covers the format/tier gating matrix.
func TestIsFormatAllowed(t *testing.T) {
	basicAllowed := []string{"csv", "xlsx"}
	for _, f := range basicAllowed {
		if !IsFormatAllowed(TierBasic, f) {
			t.Errorf("IsFormatAllowed(basic, %q) = false, want true", f)
		}
	}

	basicDenied := []string{"shp", "geojson", "gpkg", "fgb", "tab", "pmtiles", "mbtiles"}
	for _, f := range basicDenied {
		if IsFormatAllowed(TierBasic, f) {
			t.Errorf("IsFormatAllowed(basic, %q) = true, want false", f)
		}
	}

	allFormats := append(append([]string{}, basicAllowed...), basicDenied...)
	for _, f := range allFormats {
		if !IsFormatAllowed(TierFull, f) {
			t.Errorf("IsFormatAllowed(full, %q) = false, want true", f)
		}
	}
}

func TestEmployee10432AlwaysHasFullDownload(t *testing.T) {
	got := ResolvePermission(PermissionInput{
		DepName: "สำนักควบคุมน้ำสูญเสีย",
		JobName: "งานบริการและควบคุมน้ำสูญเสีย",
		UserID:  "10432",
	})

	if got.DownloadTier != TierFull {
		t.Fatalf("DownloadTier = %q, want %q", got.DownloadTier, TierFull)
	}
}

func TestMapScopeOverrideDoesNotWidenOtherPermissions(t *testing.T) {
	const userID = "map-test-user"
	mapScopeOverrides[userID] = ScopeAll
	t.Cleanup(func() { delete(mapScopeOverrides, userID) })

	input := PermissionInput{UserID: userID}
	permission := ResolvePermission(input)
	if permission.PermissionLeak != ScopeBranch || permission.DownloadTier != TierBasic {
		t.Fatalf("ordinary permission changed: %#v", permission)
	}
	if got := ResolveMapScope(input); got != ScopeAll {
		t.Fatalf("map scope = %q, want %q", got, ScopeAll)
	}
}
