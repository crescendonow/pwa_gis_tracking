// Package services/office_auth.go
// Provides the pwa_code lookup that check_user.php performs after
// a successful intranet login, using a safe parameterized query.
package services

import (
	"database/sql"
	"fmt"

	"pwa_gis_tracking/config"
)

// LookupPwaCode retrieves the pwa_code for the given BA (branch area) code
// from the pwa_office.pwa_office_ba table.
//
// Uses a parameterized query — unlike the PHP code which directly interpolated
// $obj->ba into the SQL string, creating a SQL injection risk.
//
// Returns an empty string (not an error) when no row is found, so callers can
// proceed even when the BA is unknown.
func LookupPwaCode(ba string) (string, error) {
	const query = `
		SELECT pwa_code
		FROM pwa_office.pwa_office_ba
		WHERE ba = $1
		LIMIT 1
	`

	var pwaCode string
	err := config.PgDB.QueryRow(query, ba).Scan(&pwaCode)
	if err == sql.ErrNoRows {
		return "", nil // BA not in table — continue gracefully
	}
	if err != nil {
		return "", fmt.Errorf("pwa_code lookup failed: %w", err)
	}
	return pwaCode, nil
}
