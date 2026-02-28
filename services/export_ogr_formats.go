package services

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// ExportAsGeoPackage converts GeoJSON to GeoPackage (.gpkg) using ogr2ogr.
// Returns the raw .gpkg file bytes.
func ExportAsGeoPackage(pwaCode, collection, startDate, endDate string) ([]byte, error) {
	ogr2ogrPath, err := findOgr2ogr()
	if err != nil {
		return nil, err
	}

	geojsonData, err := ExportFeaturesAsGeoJSON(pwaCode, collection, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("GeoJSON export failed: %w", err)
	}
	if len(geojsonData) < 50 {
		return nil, fmt.Errorf("no features to export")
	}

	tmpDir, err := os.MkdirTemp("", "pwa_gpkg_*")
	if err != nil {
		return nil, fmt.Errorf("temp dir error: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	inputPath := filepath.Join(tmpDir, "input.geojson")
	if err := os.WriteFile(inputPath, geojsonData, 0644); err != nil {
		return nil, fmt.Errorf("write input failed: %w", err)
	}

	gpkgPath := filepath.Join(tmpDir, fmt.Sprintf("%s_%s.gpkg", pwaCode, collection))

	cmd := exec.Command(ogr2ogrPath,
		"-f", "GPKG",
		gpkgPath,
		inputPath,
		"-nln", collection,
		"-overwrite",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ogr2ogr GPKG failed: %s — %w", string(output), err)
	}

	data, err := os.ReadFile(gpkgPath)
	if err != nil {
		return nil, fmt.Errorf("read GPKG failed: %w", err)
	}

	log.Printf("[Export] GPKG: %s/%s → %d bytes", pwaCode, collection, len(data))
	return data, nil
}

// ExportAsShapefile converts GeoJSON to ESRI Shapefile using ogr2ogr.
// Returns a zip containing .shp, .shx, .dbf, .prj files.
func ExportAsShapefile(pwaCode, collection, startDate, endDate string) ([]byte, error) {
	ogr2ogrPath, err := findOgr2ogr()
	if err != nil {
		return nil, err
	}

	geojsonData, err := ExportFeaturesAsGeoJSON(pwaCode, collection, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("GeoJSON export failed: %w", err)
	}
	if len(geojsonData) < 50 {
		return nil, fmt.Errorf("no features to export")
	}

	tmpDir, err := os.MkdirTemp("", "pwa_shp_*")
	if err != nil {
		return nil, fmt.Errorf("temp dir error: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	inputPath := filepath.Join(tmpDir, "input.geojson")
	if err := os.WriteFile(inputPath, geojsonData, 0644); err != nil {
		return nil, fmt.Errorf("write input failed: %w", err)
	}

	outDir := filepath.Join(tmpDir, "shp_out")
	os.MkdirAll(outDir, 0755)

	// ogr2ogr outputs multiple files (.shp, .shx, .dbf, .prj, .cpg)
	cmd := exec.Command(ogr2ogrPath,
		"-f", "ESRI Shapefile",
		outDir,
		inputPath,
		"-nln", collection,
		"-lco", "ENCODING=UTF-8",
		"-overwrite",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ogr2ogr Shapefile failed: %s — %w", string(output), err)
	}

	// Zip all shapefile components (uses zipDirectory from export_tab_pmtiles.go)
	zipBuf, err := zipDirectory(outDir, []string{".shp", ".shx", ".dbf", ".prj", ".cpg"})
	if err != nil {
		return nil, fmt.Errorf("zip failed: %w", err)
	}

	log.Printf("[Export] Shapefile: %s/%s → %d bytes (zip)", pwaCode, collection, zipBuf.Len())
	return zipBuf.Bytes(), nil
}