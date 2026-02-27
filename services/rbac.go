// Package services/rbac.go
// Replaces the sprawling if-else permission logic in check_user.php.
//
// Permission levels (permission_leak):
//   "all"    – full access (HQ departments + explicit HR user IDs)
//   "reg"    – regional access (งานแผนที่แนวท่อ + zone 10 user IDs)
//   "branch" – branch access  (งานบริการและควบคุมน้ำสูญเสีย + manager IDs)
//   ""       – no access (matched user but wrong role — should be rejected upstream)
package services

import (
	"strings"
)

// Permission bundles the two permission strings the PHP session stored.
type Permission struct {
	Permission      string // "leak" or ""
	PermissionLeak  string // "all" | "reg" | "branch" | ""
}

// ─── Allow-lists ─────────────────────────────────────────────────────────────

// allowedDepartments grants full ("all") leak permission to entire departments.
var allowedDepartments = map[string]string{
	"สำนักควบคุมน้ำสูญเสีย":      "all",
	"สำนักตรวจสอบกระบวนการหลัก": "all",
}

// allowedDivisions grants full ("all") leak permission to specific divisions.
var allowedDivisions = map[string]string{
	"กองเทคโนโลยีสารสนเทศระบบประปา": "all",
	"กองบริหารความเสี่ยง":            "all",
}

// allowedJobNames grants permission based on the sanitised job_name.
// Keys are the Thai-only lowercase strings after stripping non-Thai characters.
var allowedJobNames = map[string]string{
	"งานแผนที่แนวท่อ":               "reg",
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

// ─── Public API ──────────────────────────────────────────────────────────────

// ResolvePermission calculates the permission level for an authenticated user
// based on their department, division, job name and employee ID.
// Returns a zero-value Permission (both fields "") if the user has no access.
func ResolvePermission(depName, divName, jobName, userID string) Permission {
	// 1. Department-level rules
	if level, ok := allowedDepartments[depName]; ok {
		return Permission{Permission: "leak", PermissionLeak: level}
	}

	// 2. Division-level rules
	if level, ok := allowedDivisions[divName]; ok {
		return Permission{Permission: "leak", PermissionLeak: level}
	}

	// 3. Job-name rules (strip all non-Thai characters then lowercase)
	sanitised := sanitiseJobName(jobName)
	if level, ok := allowedJobNames[sanitised]; ok {
		return Permission{Permission: "leak", PermissionLeak: level}
	}

	// 4. Explicit user-ID overrides
	if level, ok := explicitUserIDs[userID]; ok {
		return Permission{Permission: "leak", PermissionLeak: level}
	}

	// No matching rule — user authenticated but not authorised.
	return Permission{}
}

// IsAuthorised returns true only when the user has a non-empty permission level.
// Mirrors the outer if-else that decides "Found" vs "N_Rights" in check_user.php.
func IsAuthorised(p Permission) bool {
	return p.Permission != "" && p.PermissionLeak != ""
}

// ─── Helper ──────────────────────────────────────────────────────────────────

// sanitiseJobName keeps only Thai Unicode characters (U+0E00–U+0E7F) and
// lowercases the result, replicating PHP's preg_replace('~[^ก-๛]~iu','', ...).
func sanitiseJobName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x0E00 && r <= 0x0E7F {
			b.WriteRune(r)
		}
	}
	return strings.ToLower(b.String())
}
