// Package services/rbac.go
// Replaces the sprawling if-else permission logic in check_user.php.
//
// Two independent axes (Requirements 13, 2026-08-17):
//
//	permission_leak (scope)  – "all" | "reg" | "branch"   → which branches' data is visible
//	download_tier            – "full" | "basic"           → which export formats are allowed
//
// Every employee authenticated by the intranet API is now allowed to log in
// (Requirement 1). Users who don't match any allow-list below default to the
// narrowest, safest scope ("branch") instead of being rejected outright.
package services

import (
	"os"
	"strings"
	"sync"
)

// Permission bundles the permission strings stored in the session.
type Permission struct {
	Permission     string // "leak" — set for every authenticated user
	PermissionLeak string // "all" | "reg" | "branch" — data scope
	DownloadTier   string // "full" | "basic" — download rights
}

// PermissionInput carries the fields from the intranet API needed to
// resolve both the data scope and the download tier.
type PermissionInput struct {
	DepName  string
	DivName  string
	JobName  string
	Position string
	UserID   string
}

// ─── Constants ───────────────────────────────────────────────────────────────

const (
	// TierFull grants download rights for every export format.
	TierFull = "full"
	// TierBasic restricts download rights to xlsx / csv only.
	TierBasic = "basic"

	// ScopeAll grants visibility of every branch's data (HQ-level).
	ScopeAll = "all"
	// ScopeRegion grants visibility of a single zone/region's branches.
	ScopeRegion = "reg"
	// ScopeBranch grants visibility of the user's own branch only.
	ScopeBranch = "branch"

	// regionalFullDownloadJobName is already in sanitised form.
	regionalFullDownloadJobName = "งานน้ำสูญเสีย"
)

// ─── Allow-lists ─────────────────────────────────────────────────────────────

// allowedDepartments grants full ("all") leak permission to entire departments.
var allowedDepartments = map[string]string{
	"สำนักควบคุมน้ำสูญเสีย":     "all",
	"สำนักตรวจสอบกระบวนการหลัก": "all",
}

// allowedDivisions grants full ("all") leak permission to specific divisions.
var allowedDivisions = map[string]string{
	"กองเทคโนโลยีสารสนเทศระบบประปา": "all",
	"กองบริหารความเสี่ยง":           "all",
}

// allowedJobNames grants permission based on the sanitised job_name.
// Keys are the Thai-only lowercase strings after stripping non-Thai characters.
var allowedJobNames = map[string]string{
	"งานแผนที่แนวท่อ":              "reg",
	regionalFullDownloadJobName:    "reg",
	"งานบริการและควบคุมน้ำสูญเสีย": "branch",
}

// explicitUserIDs grants permission to specific employee numbers.
// These mirror the hard-coded lists in check_user.php.
var explicitUserIDs = map[string]string{
	// HR — full access
	"14180": "all",
	"16361": "all",
	"15632": "all",
	// Zone 10
	"10928": "reg",
	"15011": "reg",
	"16212": "reg",
	// Managers (ผช. ผจก.)
	"11424": "branch",
	"9489":  "branch",
	"6026":  "branch",
}

// fullDownloadUserIDs are employee-specific exceptions that may download
// every supported format, independent of their data-visibility scope.
var fullDownloadUserIDs = map[string]bool{
	"10432": true,
}

// ─── Download-tier keywords (Requirement 1.1) ─────────────────────────────────

// fullTierPositionKeywords are substring-matched against the normalised
// `position` field to grant download_tier = "full".
//
//	นักวิชาการภูมิสารสนเทศ / หัวหน้างาน / ผอ.กอง / ผู้บริหาร
var fullTierPositionKeywords = []string{
	// นักวิชาการภูมิสารสนเทศ
	"ภูมิสารสนเทศ",
	// หัวหน้างาน
	"หัวหน้างาน",
	"หน.งาน",
	"หัวหน้ากลุ่ม",
	// ผอ.กอง
	"ผู้อำนวยการกอง",
	"ผอ.กอง",
	"ผอ.ก.",
	// ผู้บริหาร
	"ผู้ว่าการ",
	"รองผู้ว่าการ",
	"ผู้ช่วยผู้ว่าการ",
	"ผู้อำนวยการฝ่าย",
	"ผู้อำนวยการสำนัก",
	"ผู้จัดการ",
	"ผจก.",
	"ผู้ตรวจ",
}

// normalisedFullTierKeywordsOnce lazily normalises fullTierPositionKeywords
// (they're written with dots/spaces for readability, e.g. "ผอ.กอง") the same
// way incoming position strings are normalised, so substring comparison works.
var (
	normalisedFullTierKeywordsOnce sync.Once
	normalisedFullTierKeywords     []string
)

func getNormalisedFullTierKeywords() []string {
	normalisedFullTierKeywordsOnce.Do(func() {
		out := make([]string, 0, len(fullTierPositionKeywords))
		for _, kw := range fullTierPositionKeywords {
			out = append(out, normaliseThai(kw))
		}
		normalisedFullTierKeywords = out
	})
	return normalisedFullTierKeywords
}

// ─── Lazy env-var loading ──────────────────────────────────────────────────────
//
// godotenv.Load() runs inside main(), which executes AFTER every package's
// init(). Reading os.Getenv() in init() would therefore always see an empty
// value. We read the env lazily instead, on first use, guarded by sync.Once.

var (
	extraFullKeywordsOnce sync.Once
	extraFullKeywords     []string

	extraFullUserIDsOnce sync.Once
	extraFullUserIDs     map[string]bool
)

// loadExtraFullKeywords reads DOWNLOAD_FULL_POSITION_KEYWORDS (comma
// separated) once, normalising each keyword the same way positions are
// normalised before comparison.
func loadExtraFullKeywords() []string {
	extraFullKeywordsOnce.Do(func() {
		raw := os.Getenv("DOWNLOAD_FULL_POSITION_KEYWORDS")
		if raw == "" {
			extraFullKeywords = nil
			return
		}
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			out = append(out, normaliseThai(p))
		}
		extraFullKeywords = out
	})
	return extraFullKeywords
}

// loadExtraFullUserIDs reads DOWNLOAD_FULL_USER_IDS (comma separated) once.
func loadExtraFullUserIDs() map[string]bool {
	extraFullUserIDsOnce.Do(func() {
		extraFullUserIDs = make(map[string]bool)
		raw := os.Getenv("DOWNLOAD_FULL_USER_IDS")
		if raw == "" {
			return
		}
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			extraFullUserIDs[p] = true
		}
	})
	return extraFullUserIDs
}

// ─── Public API ──────────────────────────────────────────────────────────────

// ResolvePermission calculates the data scope (permission_leak) and the
// download tier for an authenticated user. Every user who reaches this
// function has already passed intranet authentication (Requirement 1), so
// Permission is always "leak"; users who don't match any allow-list default
// to the narrowest scope, ScopeBranch, rather than being rejected.
func ResolvePermission(u PermissionInput) Permission {
	scope := resolveScope(u)
	tier := resolveDownloadTier(u, scope)

	return Permission{
		Permission:     "leak",
		PermissionLeak: scope,
		DownloadTier:   tier,
	}
}

// resolveScope determines the data-visibility scope from the existing
// allow-lists. Unmatched users get ScopeBranch (the narrowest, safest scope)
// instead of an empty string.
func resolveScope(u PermissionInput) string {
	sanitised := sanitiseJobName(u.JobName)
	if sanitised == regionalFullDownloadJobName {
		return ScopeRegion
	}

	// 1. Department-level rules
	if level, ok := allowedDepartments[u.DepName]; ok {
		return level
	}

	// 2. Division-level rules
	if level, ok := allowedDivisions[u.DivName]; ok {
		return level
	}

	// 3. Job-name rules (strip all non-Thai characters then lowercase)
	if level, ok := allowedJobNames[sanitised]; ok {
		return level
	}

	// 4. Explicit user-ID overrides
	if level, ok := explicitUserIDs[u.UserID]; ok {
		return level
	}

	// No matching rule — default to the narrowest scope (Requirement 1).
	return ScopeBranch
}

// resolveDownloadTier decides download_tier from position / job_name /
// div_name / employee ID, per Requirement 1.1.
func resolveDownloadTier(in PermissionInput, scope string) string {
	// Users who already have HQ-level ("all") scope keep full download
	// rights, to avoid regressing existing users of the old allow-lists.
	if scope == ScopeAll {
		return TierFull
	}

	// Explicit per-employee override.
	if fullDownloadUserIDs[in.UserID] || loadExtraFullUserIDs()[in.UserID] {
		return TierFull
	}

	// Exact matching preserves the basic tier of the longer existing job name
	// "งานบริการและควบคุมน้ำสูญเสีย".
	if sanitiseJobName(in.JobName) == regionalFullDownloadJobName {
		return TierFull
	}

	position := normaliseThai(in.Position)
	jobName := normaliseThai(in.JobName)
	divName := normaliseThai(in.DivName)

	for _, kw := range getNormalisedFullTierKeywords() {
		if strings.Contains(position, kw) {
			return TierFull
		}
	}

	// Extra keywords configured via DOWNLOAD_FULL_POSITION_KEYWORDS.
	for _, kw := range loadExtraFullKeywords() {
		if strings.Contains(position, kw) {
			return TierFull
		}
	}

	// Supports HR data where the ภูมิสารสนเทศ job line is split out into
	// job_name/div_name while position is generic, e.g. "นักวิชาการ 6".
	geoKeyword := normaliseThai("ภูมิสารสนเทศ")
	academicKeyword := normaliseThai("นักวิชาการ")
	if (strings.Contains(jobName, geoKeyword) || strings.Contains(divName, geoKeyword)) &&
		strings.Contains(position, academicKeyword) {
		return TierFull
	}

	return TierBasic
}

// IsAuthorised returns true when the user has a non-empty permission level.
// Kept for backward compatibility with any other callers; under the new
// rules ResolvePermission always sets Permission = "leak" for authenticated
// users (Requirement 1), so this effectively always returns true.
func IsAuthorised(p Permission) bool {
	return p.Permission != ""
}

// IsFullDownload reports whether tier grants unrestricted download rights.
func IsFullDownload(tier string) bool {
	return tier == TierFull
}

// alwaysAllowedFormats lists formats every authenticated user can download
// regardless of download_tier.
var alwaysAllowedFormats = map[string]bool{
	"xlsx": true,
	"csv":  true,
}

// fullTierFormats lists formats that additionally require TierFull.
var fullTierFormats = map[string]bool{
	"geojson": true,
	"gpkg":    true,
	"shp":     true,
	"fgb":     true,
	"tab":     true,
	"pmtiles": true,
	"mbtiles": true,
}

// IsFormatAllowed reports whether a given export format may be downloaded
// by a user with the given download_tier.
func IsFormatAllowed(tier, format string) bool {
	format = strings.ToLower(strings.TrimSpace(format))
	if alwaysAllowedFormats[format] {
		return true
	}
	if tier == TierFull {
		return true
	}
	return false
}

// AllowedFormats returns the list of export formats available to a given
// download_tier, for the frontend to render/hide UI controls.
func AllowedFormats(tier string) []string {
	out := make([]string, 0, len(alwaysAllowedFormats)+len(fullTierFormats))
	for f := range alwaysAllowedFormats {
		out = append(out, f)
	}
	if tier == TierFull {
		for f := range fullTierFormats {
			out = append(out, f)
		}
	}
	return out
}

// ─── Helper ──────────────────────────────────────────────────────────────────

// sanitiseJobName keeps only Thai Unicode characters (U+0E00–U+0E7F) and
// lowercases the result, replicating PHP's preg_replace('~[^ก-๛]~iu', ”, ...).
func sanitiseJobName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x0E00 && r <= 0x0E7F {
			b.WriteRune(r)
		}
	}
	return strings.ToLower(b.String())
}

// normaliseThai strips spaces, dots, parentheses and digits, then
// lowercases the result, so that variants such as "ผอ.กอง", "ผอ กอง" and
// "ผู้อำนวยการกอง 8" all compare equal for substring matching.
func normaliseThai(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			continue
		case r == '.' || r == '(' || r == ')':
			continue
		case r >= '0' && r <= '9':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return strings.ToLower(b.String())
}
