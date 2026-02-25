package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"github.com/gin-gonic/gin"
)

const (
	cookieName    = "pwa_gis_session"
	cookieMaxAge  = 7200 // 2 hours
	defaultSecret = "pwa-gis-tracking-secret-key-2025"
)

// getSessionSecret returns the session secret from env or default.
func getSessionSecret() string {
	s := os.Getenv("SESSION_SECRET")
	if s == "" {
		s = defaultSecret
	}
	return s
}

// getAuthCredentials returns username/password from env or defaults.
func getAuthCredentials() (string, string) {
	user := os.Getenv("AUTH_USER")
	pass := os.Getenv("AUTH_PASS")
	if user == "" {
		user = "admin"
	}
	if pass == "" {
		pass = "password"
	}
	return user, pass
}

// signToken creates an HMAC-SHA256 signed token: "username|timestamp|signature"
func signToken(username string) string {
	ts := fmt.Sprintf("%d", time.Now().Unix())
	payload := username + "|" + ts

	mac := hmac.New(sha256.New, []byte(getSessionSecret()))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	return payload + "|" + sig
}

// verifyToken checks if the token is valid and not expired (2 hours).
func verifyToken(token string) (string, bool) {
	parts := strings.SplitN(token, "|", 3)
	if len(parts) != 3 {
		return "", false
	}

	username := parts[0]
	ts := parts[1]
	sig := parts[2]

	// Verify signature
	payload := username + "|" + ts
	mac := hmac.New(sha256.New, []byte(getSessionSecret()))
	mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return "", false
	}

	// Check expiry (2 hours)
	var tsInt int64
	fmt.Sscanf(ts, "%d", &tsInt)
	if time.Now().Unix()-tsInt > int64(cookieMaxAge) {
		return "", false
	}

	return username, true
}

// ShowLoginPage renders the login page.
// GET /pwa_gis_tracking/login
func ShowLoginPage(c *gin.Context) {
	basePath := "/" + strings.Trim(c.Request.URL.Path, "/")
	basePath = strings.TrimSuffix(basePath, "/login")

	c.HTML(http.StatusOK, "login.html", gin.H{
		"BasePath": basePath,
	})
}

// HandleLogin processes login POST request.
// POST /pwa_gis_tracking/login
func HandleLogin(c *gin.Context) {
	basePath := "/" + strings.Trim(c.Request.URL.Path, "/")
	basePath = strings.TrimSuffix(basePath, "/login")

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "ข้อมูลไม่ถูกต้อง",
		})
		return
	}

	authUser, authPass := getAuthCredentials()

	if req.Username != authUser || req.Password != authPass {
		c.JSON(http.StatusUnauthorized, gin.H{
			"status":  "error",
			"message": "ชื่อผู้ใช้หรือรหัสผ่านไม่ถูกต้อง",
		})
		return
	}

	// Login success — set signed cookie
	token := signToken(req.Username)
	isSecure := c.Request.TLS != nil ||
		c.GetHeader("X-Forwarded-Proto") == "https"

	c.SetCookie(cookieName, token, cookieMaxAge, basePath+"/", "", isSecure, true)

	c.JSON(http.StatusOK, gin.H{
		"status":   "success",
		"message":  "เข้าสู่ระบบสำเร็จ",
		"redirect": basePath + "/",
	})
}

// HandleLogout clears the session cookie and redirects to login.
// GET /pwa_gis_tracking/logout
func HandleLogout(c *gin.Context) {
	basePath := "/" + strings.Trim(c.Request.URL.Path, "/")
	basePath = strings.TrimSuffix(basePath, "/logout")

	isSecure := c.Request.TLS != nil ||
		c.GetHeader("X-Forwarded-Proto") == "https"

	c.SetCookie(cookieName, "", -1, basePath+"/", "", isSecure, true)
	c.Redirect(http.StatusFound, basePath+"/login")
}

// AuthRequired is middleware that checks for a valid session cookie.
// If not authenticated, redirects to login page.
func AuthRequired(basePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(cookieName)
		if err != nil || token == "" {
			redirectToLogin(c, basePath)
			return
		}

		username, valid := verifyToken(token)
		if !valid {
			redirectToLogin(c, basePath)
			return
		}

		// Store username in context for use in handlers
		c.Set("username", username)
		c.Next()
	}
}

func redirectToLogin(c *gin.Context, basePath string) {
	// For API requests, return JSON 401
	if strings.HasPrefix(c.Request.URL.Path, basePath+"/api") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"status":  "error",
			"message": "กรุณาเข้าสู่ระบบ",
		})
		return
	}

	// For page requests, redirect to login
	c.Redirect(http.StatusFound, basePath+"/login")
	c.Abort()
}