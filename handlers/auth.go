// Package handlers/auth.go
//
// Replaces two PHP files:
//   • check_user.php  → HandleLogin  (POST /login)
//   • check_session.php → AuthRequired (middleware)
//
// Design decisions vs. the PHP originals:
//   - Passwords are MD5-hashed only to satisfy the upstream intranet API;
//     the hash is NEVER stored locally.
//   - Sessions are managed with gorilla/sessions backed by a server-side HMAC
//     signed cookie, so clients cannot tamper with session values.
//   - SQL uses parameterised queries throughout (no string interpolation).
//   - Permission logic is delegated to services.ResolvePermission, which
//     replaces the deeply nested if-else tree in check_user.php.
package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"pwa_gis_tracking/config"
	"pwa_gis_tracking/services"

	"github.com/gin-gonic/gin"
)

// ─── Session key constants ────────────────────────────────────────────────────

// Using typed constants avoids typo-prone bare string keys scattered around.
const (
	sessUserID       = "ses_userid"   // gorilla session ID token
	sessUname        = "uname"
	sessPwaCode      = "pwacode"
	sessPermission   = "permission"
	sessPermLeak     = "permission_leak"
	sessArea         = "area"
	sessJobName      = "job_name"
	sessDivision     = "division"
	sessInsitution   = "insitution"
	sessPosition     = "position"
	sessLvl          = "lvl"
	sessUID          = "uid"
	sessLoginStatus  = "loginstatus"
)

// ─── Login page ───────────────────────────────────────────────────────────────

// ShowLoginPage renders the login HTML template.
// GET /pwa_gis_tracking/login
func ShowLoginPage(c *gin.Context) {
	basePath := resolveBasePath(c.Request.URL.Path, "/login")
	c.HTML(http.StatusOK, "login.html", gin.H{"BasePath": basePath})
}

// ─── Login action ─────────────────────────────────────────────────────────────

// loginRequest is the expected JSON body for POST /login.
type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Info     string `json:"info"` // device/browser info for logging (optional)
}

// HandleLogin authenticates the user against the PWA intranet API,
// resolves their RBAC permission, queries pwa_code from PostgreSQL,
// persists the session, and writes an audit log entry.
//
// POST /pwa_gis_tracking/login
func HandleLogin(c *gin.Context) {
	basePath := resolveBasePath(c.Request.URL.Path, "/login")

	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"result":  "error",
			"message": "กรุณากรอกชื่อผู้ใช้และรหัสผ่าน",
			"link":    "./",
		})
		return
	}

	// 1. Sanitise username: keep digits only (replicates PHP's preg_replace('~[^0-9]~iu','',…))
	username := keepDigitsOnly(req.Username)

	// 2. Call intranet authentication endpoint
	user, err := services.AuthenticateIntranet(username, req.Password)
	if err != nil {
		log.Printf("intranet auth error for user %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"result":  "N_Found",
			"message": "ไม่สามารถเชื่อมต่อระบบยืนยันตัวตนได้",
			"link":    "./",
		})
		return
	}

	// 3. Reject non-passing responses from the intranet
	if user.Check != "P" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"result":  "N_Found",
			"message": "ชื่อผู้ใช้หรือรหัสผ่านไม่ถูกต้อง",
			"link":    "./",
		})
		return
	}

	// 4. Resolve RBAC permission (replaces the big if-else block)
	perm := services.ResolvePermission(user.DepName, user.DivName, user.JobName, user.User)
	if !services.IsAuthorised(perm) {
		c.JSON(http.StatusForbidden, gin.H{
			"result":  "N_Rights",
			"message": "คุณไม่มีสิทธิ์เข้าใช้งานระบบนี้",
			"link":    "./",
		})
		return
	}

	// 5. Query pwa_code from PostgreSQL (parameterised — no SQL injection)
	pwaCode, err := services.LookupPwaCode(user.BA)
	if err != nil {
		log.Printf("pwa_code lookup error for ba=%s: %v", user.BA, err)
		// Non-fatal: continue without pwa_code rather than blocking the login
	}

	// 6. Establish session
	session, err := config.Store.Get(c.Request, config.SessionName)
	if err != nil {
		// Corrupt/old session — create a fresh one
		session, _ = config.Store.New(c.Request, config.SessionName)
	}
	session.Options.HttpOnly = true
	session.Options.SameSite = http.SameSiteLaxMode

	fullName := user.Myname + " " + user.MySurname

	session.Values[sessUserID]      = user.User // employee ID (session.ID is empty for CookieStore)
	session.Values[sessUname]       = fullName
	session.Values[sessPwaCode]     = pwaCode
	session.Values[sessPermission]  = perm.Permission
	session.Values[sessPermLeak]    = perm.PermissionLeak
	session.Values[sessArea]        = user.Area
	session.Values[sessJobName]     = user.JobName
	session.Values[sessDivision]    = user.DivName
	session.Values[sessInsitution]  = user.DepName
	session.Values[sessPosition]    = user.Position
	session.Values[sessLvl]         = user.Level
	session.Values[sessUID]         = user.User
	session.Values[sessLoginStatus] = 1

	if err := session.Save(c.Request, c.Writer); err != nil {
		log.Printf("session save error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"result": "error", "link": "./"})
		return
	}

	// 7. Write audit log entry (replicates PHP's file_put_contents)
	writeLoginLog(pwaCode, user.PwaCode, user.User)

	// 8. Return success response
	// "status"+"redirect" are used by login.html JS; legacy fields kept for API compat.
	c.JSON(http.StatusOK, gin.H{
		"status":      "success",
		"result":      "Found",
		"name":        fullName,
		"uid":         user.User,
		"position":    user.Position,
		"level":       user.Level,
		"job_name":    user.JobName,
		"division":    user.DivName,
		"insitution":  user.DepName,
		"permission":  perm.Permission,
		"redirect":    basePath + "/",
		"link":        basePath + "/",
	})
}

// ─── Logout ───────────────────────────────────────────────────────────────────

// HandleLogout destroys the session and redirects to the login page.
// GET /pwa_gis_tracking/logout
func HandleLogout(c *gin.Context) {
	basePath := resolveBasePath(c.Request.URL.Path, "/logout")

	session, err := config.Store.Get(c.Request, config.SessionName)
	if err == nil {
		// MaxAge = -1 instructs the browser to delete the cookie immediately.
		session.Options.MaxAge = -1
		_ = session.Save(c.Request, c.Writer)
	}

	c.Redirect(http.StatusFound, basePath+"/login")
}

// ─── Auth middleware ──────────────────────────────────────────────────────────

// AuthRequired replaces check_session.php.
//
// It reads the gorilla session and checks that:
//   1. ses_userid is set (non-empty)
//   2. permission is set (non-empty)
//   3. loginstatus == 1
//
// On failure it either returns JSON 401 (for /api/* paths) or redirects to
// the login page (for HTML page requests).
func AuthRequired(basePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		session, err := config.Store.Get(c.Request, config.SessionName)
		if err != nil {
			abortOrRedirect(c, basePath)
			return
		}

		// Mirror PHP: !$_SESSION['ses_userid'] || !$_SESSION['permission'] || !$_SESSION['loginstatus']
		sesUserID, _ := session.Values[sessUserID].(string)
		permission, _ := session.Values[sessPermission].(string)
		loginStatus, _ := session.Values[sessLoginStatus].(int)

		if sesUserID == "" || permission == "" || loginStatus != 1 {
			// Destroy the session (mirrors session_destroy() in PHP)
			session.Options.MaxAge = -1
			_ = session.Save(c.Request, c.Writer)
			abortOrRedirect(c, basePath)
			return
		}

		// Expose session values to downstream handlers if needed
		c.Set("uid", session.Values[sessUID])
		c.Set("uname", session.Values[sessUname])
		c.Set("pwacode", session.Values[sessPwaCode])
		c.Set("permission", permission)
		c.Set("permission_leak", session.Values[sessPermLeak])

		c.Next()
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// abortOrRedirect returns JSON 401 for API calls and a redirect for page requests.
func abortOrRedirect(c *gin.Context, basePath string) {
	if strings.HasPrefix(c.Request.URL.Path, basePath+"/api") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"status":  "error",
			"message": "กรุณาเข้าสู่ระบบ",
		})
		return
	}
	c.Redirect(http.StatusFound, basePath+"/login")
	c.Abort()
}

// resolveBasePath trims the given suffix from the URL path to derive the
// application base path (e.g. "/pwa_gis_tracking").
func resolveBasePath(urlPath, suffix string) string {
	p := "/" + strings.Trim(urlPath, "/")
	return strings.TrimSuffix(p, suffix)
}

// keepDigitsOnly replicates PHP's preg_replace('~[^0-9]~iu', '', $username).
func keepDigitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// writeLoginLog replicates the PHP file_put_contents audit log.
// Format: "YYYY-MM-DD HH:MM:SS  <pwaCodeBA>|<pwaCodeAPI>  <employeeID>"
func writeLoginLog(pwaCodeBA, pwaCodeAPI, employeeID string) {
	if config.LoginLogFile == nil {
		return
	}
	ts := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("%s  %s|%s  %s\n", ts, pwaCodeBA, pwaCodeAPI, employeeID)

	if _, err := fmt.Fprint(config.LoginLogFile, line); err != nil {
		log.Printf("login log write error: %v", err)
	}
}