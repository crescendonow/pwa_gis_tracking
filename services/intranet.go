// Package services/intranet.go
// Wraps the HTTP call to the PWA intranet authentication endpoint,
// replicating the PHP curl + JSON-cleanup pattern in check_user.php.
//
// NOTE: We shell out to curl.exe because the PWA intranet (IIS/8.5) forcibly
// rejects Go's native TLS Client Hello. curl.exe uses Windows' schannel which
// the intranet server accepts without issues.
package services

import (
	"context"
	"crypto/md5" //nolint:gosec // Legacy API requires MD5 hashing
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// intranetBaseURL is the upstream authentication endpoint.
const intranetBaseURL = "https://intranet.pwa.co.th/login/app_gis.php"

// IntranetUser holds the fields returned by the upstream API that we care about.
// Field names match the JSON keys observed in the PHP code.
type IntranetUser struct {
	Check     string `json:"check"`    // "P" = pass, other = fail
	User      string `json:"user"`     // employee number
	Myname    string `json:"Myname"`
	MySurname string `json:"MySurname"`
	DivName   string `json:"div_name"`
	DepName   string `json:"dep_name"`
	JobName   string `json:"job_name"`
	Position  string `json:"position"`
	Level     string `json:"level"`
	Area      string `json:"area"`
	BA        string `json:"ba"`
	PwaCode   string `json:"pwacode"`
}

// hashPassword returns the MD5 hex digest of the plain-text password,
// matching PHP's md5($password).
func hashPassword(plain string) string {
	//nolint:gosec // Required for backward-compatibility with the upstream API
	return fmt.Sprintf("%x", md5.Sum([]byte(plain)))
}

// AuthenticateIntranet calls the PWA intranet API via curl.exe and returns
// the decoded user on success.
//
// We use curl.exe because Go's crypto/tls is incompatible with the legacy
// IIS/8.5 TLS stack (connection forcibly closed during handshake).
// curl.exe uses Windows schannel which handles the renegotiation correctly.
func AuthenticateIntranet(username, password string) (*IntranetUser, error) {
	// Build target URL
	params := url.Values{}
	params.Set("u", username)
	params.Set("p", hashPassword(password))
	target := intranetBaseURL + "?" + params.Encode()

	// 15-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Shell out to curl.exe (-k = skip TLS verify, -s = silent, -S = show errors)
	cmd := exec.CommandContext(ctx, `C:\Windows\System32\curl.exe`, "-k", "-s", "-S", target)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("intranet request failed (curl): %w", err)
	}

	// Replicate PHP's character-stripping:
	//   $json = str_replace("(", "", $json);
	//   $json = str_replace(")", "", $json);
	//   $json = str_replace(";", "", $json);
	cleaned := strings.NewReplacer("(", "", ")", "", ";", "").Replace(string(output))

	var user IntranetUser
	if err := json.Unmarshal([]byte(cleaned), &user); err != nil {
		return nil, fmt.Errorf("intranet JSON decode failed: %w (body: %.200s)", err, cleaned)
	}

	return &user, nil
}