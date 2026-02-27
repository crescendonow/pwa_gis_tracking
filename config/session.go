package config // ← must match the folder name: config/

import (
	"net/http"
	"os"

	"github.com/gorilla/sessions"
)

// Store is the global gorilla/sessions cookie store.
// The authentication key is loaded from SESSION_SECRET env var.
// An optional encryption key (SESSION_ENC_KEY, must be 16, 24, or 32 bytes)
// enables AES encryption of cookie contents for extra security.
var Store *sessions.CookieStore

const SessionName = "pwa_gis_session"

// InitSessionStore initialises the gorilla/sessions cookie store.
// Call once from main.go before starting the HTTP server.
func InitSessionStore() {
	authKey := []byte(getEnvOrDefault("SESSION_SECRET", "change-me-in-production-32chars!!"))

	encKeyStr := os.Getenv("SESSION_ENC_KEY")
	if encKeyStr != "" {
		Store = sessions.NewCookieStore(authKey, []byte(encKeyStr))
	} else {
		Store = sessions.NewCookieStore(authKey)
	}

	Store.Options = &sessions.Options{
		Path:     "/pwa_gis_tracking/",
		MaxAge:   7200,                // 2 hours
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode, // correct typed constant (= 2), not a bare int
	}
}