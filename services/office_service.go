package services

import (
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strconv"

	"pwa_gis_tracking/config"
	"pwa_gis_tracking/models"
)

// GetAllOffices retrieves all branch offices, sorted by zone (numeric) then pwa_code.
func GetAllOffices() ([]models.PwaOffice, error) {
	rows, err := config.PgDB.Query(`
		SELECT pwa_code, name, zone 
		FROM pwa_office.pwa_office234 
		ORDER BY zone, pwa_code
	`)
	if err != nil {
		return nil, fmt.Errorf("query offices failed: %v", err)
	}
	defer rows.Close()

	var offices []models.PwaOffice
	for rows.Next() {
		var o models.PwaOffice
		if err := rows.Scan(&o.PwaCode, &o.Name, &o.Zone); err != nil {
			log.Printf("scan office row error: %v", err)
			continue
		}
		offices = append(offices, o)
	}

	// Sort zones numerically: 1,2,3,...10 (not alphabetically: 1,10,2,...)
	sortOfficesNumeric(offices)
	return offices, nil
}

// GetOfficesByZone retrieves branch offices filtered by zone code.
func GetOfficesByZone(zone string) ([]models.PwaOffice, error) {
	rows, err := config.PgDB.Query(`
		SELECT pwa_code, name, zone 
		FROM pwa_office.pwa_office234 
		WHERE zone = $1
		ORDER BY pwa_code
	`, zone)
	if err != nil {
		return nil, fmt.Errorf("query offices by zone failed: %v", err)
	}
	defer rows.Close()

	var offices []models.PwaOffice
	for rows.Next() {
		var o models.PwaOffice
		if err := rows.Scan(&o.PwaCode, &o.Name, &o.Zone); err != nil {
			continue
		}
		offices = append(offices, o)
	}
	return offices, nil
}

// GetZones retrieves all zones with branch count, sorted numerically (1-10).
func GetZones() ([]models.ZoneSummary, error) {
	rows, err := config.PgDB.Query(`
		SELECT zone, COUNT(*) as branch_count 
		FROM pwa_office.pwa_office234 
		GROUP BY zone 
		ORDER BY zone
	`)
	if err != nil {
		return nil, fmt.Errorf("query zones failed: %v", err)
	}
	defer rows.Close()

	var zones []models.ZoneSummary
	for rows.Next() {
		var z models.ZoneSummary
		if err := rows.Scan(&z.Zone, &z.BranchCount); err != nil {
			continue
		}
		zones = append(zones, z)
	}

	// Sort numerically: 1,2,3,...10
	sort.Slice(zones, func(i, j int) bool {
		a, _ := strconv.Atoi(zones[i].Zone)
		b, _ := strconv.Atoi(zones[j].Zone)
		return a < b
	})

	return zones, nil
}

// GetAllOfficesWithGeom retrieves all offices with lat/lng extracted from wkb_geometry column.
// Uses PostGIS ST_Y/ST_X to extract coordinates from the WKB geometry.
// Falls back gracefully if PostGIS functions are unavailable.
func GetAllOfficesWithGeom() ([]models.PwaOffice, error) {
	rows, err := config.PgDB.Query(`
		SELECT 
			pwa_code, 
			name, 
			zone,
			ST_Y(wkb_geometry::geometry) AS lat,
			ST_X(wkb_geometry::geometry) AS lng
		FROM pwa_office.pwa_office234 
		WHERE wkb_geometry IS NOT NULL
		ORDER BY zone, pwa_code
	`)
	if err != nil {
		// Fallback: try alternative WKB decode if direct cast fails
		log.Printf("ST_X/ST_Y query failed, trying fallback: %v", err)
		return getAllOfficesWithGeomFallback()
	}
	defer rows.Close()

	var offices []models.PwaOffice
	for rows.Next() {
		var o models.PwaOffice
		var lat, lng sql.NullFloat64
		if err := rows.Scan(&o.PwaCode, &o.Name, &o.Zone, &lat, &lng); err != nil {
			log.Printf("scan geom row error: %v", err)
			continue
		}
		if lat.Valid && lng.Valid {
			o.Lat = &lat.Float64
			o.Lng = &lng.Float64
		}
		offices = append(offices, o)
	}

	sortOfficesNumeric(offices)
	return offices, nil
}

// getAllOfficesWithGeomFallback uses ST_GeomFromWKB as an alternative approach.
// If that also fails, returns offices without geometry.
func getAllOfficesWithGeomFallback() ([]models.PwaOffice, error) {
	rows, err := config.PgDB.Query(`
		SELECT 
			pwa_code, 
			name, 
			zone,
			ST_Y(ST_GeomFromWKB(wkb_geometry, 4326)) AS lat,
			ST_X(ST_GeomFromWKB(wkb_geometry, 4326)) AS lng
		FROM pwa_office.pwa_office234 
		WHERE wkb_geometry IS NOT NULL
		ORDER BY zone, pwa_code
	`)
	if err != nil {
		log.Printf("Fallback geom query also failed: %v", err)
		// Final fallback: return offices without coordinates
		return GetAllOffices()
	}
	defer rows.Close()

	var offices []models.PwaOffice
	for rows.Next() {
		var o models.PwaOffice
		var lat, lng sql.NullFloat64
		if err := rows.Scan(&o.PwaCode, &o.Name, &o.Zone, &lat, &lng); err != nil {
			continue
		}
		if lat.Valid && lng.Valid {
			o.Lat = &lat.Float64
			o.Lng = &lng.Float64
		}
		offices = append(offices, o)
	}

	sortOfficesNumeric(offices)
	return offices, nil
}

// sortOfficesNumeric sorts offices by zone (numeric ascending) then pwa_code.
func sortOfficesNumeric(offices []models.PwaOffice) {
	sort.Slice(offices, func(i, j int) bool {
		a, _ := strconv.Atoi(offices[i].Zone)
		b, _ := strconv.Atoi(offices[j].Zone)
		if a != b {
			return a < b
		}
		return offices[i].PwaCode < offices[j].PwaCode
	})
}
