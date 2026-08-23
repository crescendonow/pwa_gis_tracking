package routes_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"pwa_gis_tracking/config"
	"pwa_gis_tracking/routes"

	"github.com/gin-gonic/gin"
)

const basePath = "/pwa_gis_tracking"

func setupTestRouter(t *testing.T) *gin.Engine {
	t.Helper()

	ensureRepoRoot(t)

	gin.SetMode(gin.TestMode)
	config.InitSessionStore()
	config.PgDB = nil
	config.MongoDB = nil

	router := gin.New()
	routes.RegisterRoutes(router)
	return router
}

func ensureRepoRoot(t *testing.T) {
	t.Helper()

	if _, err := os.Stat(filepath.Join("templates", "dashboard.html")); err == nil {
		return
	}
	if _, err := os.Stat(filepath.Join("..", "templates", "dashboard.html")); err == nil {
		if err := os.Chdir(".."); err != nil {
			t.Fatalf("chdir repo root: %v", err)
		}
		return
	}

	t.Fatal("could not locate templates/dashboard.html from test working directory")
}

func addAuthenticatedSession(t *testing.T, req *http.Request, rr *httptest.ResponseRecorder) {
	t.Helper()

	session, err := config.Store.Get(req, config.SessionName)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}

	session.Values["ses_userid"] = "123456"
	session.Values["permission"] = "admin"
	session.Values["loginstatus"] = 1
	session.Values["uid"] = "123456"
	session.Values["uname"] = "Route Test User"
	session.Values["pwacode"] = "1001"
	session.Values["permission_leak"] = "admin"

	if err := session.Save(req, rr); err != nil {
		t.Fatalf("save session: %v", err)
	}
}

func authenticatedCookies(t *testing.T) []*http.Cookie {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, basePath+"/", nil)
	rr := httptest.NewRecorder()
	addAuthenticatedSession(t, req, rr)

	cookies := rr.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected authenticated session cookie")
	}
	return cookies
}

func performRequest(router http.Handler, method, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func decodeJSONBody(t *testing.T, rr *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()

	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode json body: %v; body=%q", err, rr.Body.String())
	}
	return body
}

func TestProtectedAPIRouteRequiresAuth(t *testing.T) {
	router := setupTestRouter(t)

	rr := performRequest(router, http.MethodGet, basePath+"/api/cache/status")

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	body := decodeJSONBody(t, rr)
	if body["status"] != "error" {
		t.Fatalf("status = %v, want error", body["status"])
	}
}

func TestProtectedPageRedirectsWithoutAuth(t *testing.T) {
	router := setupTestRouter(t)

	rr := performRequest(router, http.MethodGet, basePath+"/")

	if rr.Code != http.StatusFound {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusFound)
	}
	if location := rr.Header().Get("Location"); location != basePath+"/login" {
		t.Fatalf("Location = %q, want %q", location, basePath+"/login")
	}
}

func TestHealthRouteRequiresAuthAndReturnsOK(t *testing.T) {
	router := setupTestRouter(t)

	rr := performRequest(router, http.MethodGet, basePath+"/health", authenticatedCookies(t)...)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d; body=%q", rr.Code, http.StatusOK, rr.Body.String())
	}

	body := decodeJSONBody(t, rr)
	if body["status"] != "OK" {
		t.Fatalf("status = %v, want OK", body["status"])
	}
	if body["service"] != "PWA GIS Online Tracking" {
		t.Fatalf("service = %v, want PWA GIS Online Tracking", body["service"])
	}
}

func TestCacheStatusRouteWithAuth(t *testing.T) {
	router := setupTestRouter(t)

	rr := performRequest(router, http.MethodGet, basePath+"/api/cache/status", authenticatedCookies(t)...)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d; body=%q", rr.Code, http.StatusOK, rr.Body.String())
	}

	body := decodeJSONBody(t, rr)
	if body["status"] != "success" {
		t.Fatalf("status = %v, want success", body["status"])
	}
	if _, ok := body["cache"].(map[string]interface{}); !ok {
		t.Fatalf("cache = %T, want object", body["cache"])
	}
}

func TestStaticRouteRegistered(t *testing.T) {
	router := setupTestRouter(t)

	rr := performRequest(router, http.MethodGet, basePath+"/static/css/style.css")

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d; body=%q", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestDashboardHtmlRedirectRequiresAuth(t *testing.T) {
	router := setupTestRouter(t)

	rr := performRequest(router, http.MethodGet, basePath+"/dashboard.html", authenticatedCookies(t)...)

	if rr.Code != http.StatusMovedPermanently {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusMovedPermanently)
	}
	if location := rr.Header().Get("Location"); location != basePath+"/" {
		t.Fatalf("Location = %q, want %q", location, basePath+"/")
	}
}

func TestMapPageRequiresAuthentication(t *testing.T) {
	router := setupTestRouter(t)

	rr := performRequest(router, http.MethodGet, basePath+"/map")

	if rr.Code != http.StatusFound {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusFound)
	}
	if location := rr.Header().Get("Location"); location != basePath+"/login" {
		t.Fatalf("Location = %q, want %q", location, basePath+"/login")
	}
}
