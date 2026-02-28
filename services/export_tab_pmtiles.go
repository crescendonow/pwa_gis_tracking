package services

import (
	"archive/zip"
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ========================================================================
// Export: MapInfo TAB (.tab) and PMTiles (.pmtiles)
//
// Cross-platform (Windows + Linux):
//   - TAB:     ogr2ogr (GDAL) + Go archive/zip (ไม่ต้องมี zip command)
//   - PMTiles: tippecanoe (preferred) → ogr2ogr+pmtiles CLI → error
//
// Prerequisites:
//   - GDAL:        Windows: OSGeo4W installer / Linux: apt install gdal-bin
//   - tippecanoe:  https://github.com/felt/tippecanoe (Linux/WSL)
//   - pmtiles CLI: https://github.com/protomaps/go-pmtiles (optional)
// ========================================================================

// ExportAsMapInfoTAB converts GeoJSON to MapInfo TAB format using ogr2ogr.
// Returns a zip file containing .tab, .dat, .map, .id files.
// Uses Go's archive/zip package (cross-platform, no external zip needed).
func ExportAsMapInfoTAB(pwaCode, collection, startDate, endDate string) ([]byte, error) {
	ogr2ogrPath, err := findOgr2ogr()
	if err != nil {
		return nil, err
	}
	geojsonData, err := ExportFeaturesAsGeoJSON(pwaCode, collection, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("GeoJSON export failed: %w", err)
	}
	return convertGeoJSONToTAB(ogr2ogrPath, geojsonData, pwaCode, collection)
}

// ExportMergedAsMapInfoTAB converts pre-merged GeoJSON to MapInfo TAB.
func ExportMergedAsMapInfoTAB(geojsonData []byte, outputName string) ([]byte, error) {
	ogr2ogrPath, err := findOgr2ogr()
	if err != nil {
		return nil, err
	}
	return convertGeoJSONToTAB(ogr2ogrPath, geojsonData, outputName, "merged")
}

func convertGeoJSONToTAB(ogr2ogrPath string, geojsonData []byte, pwaCode, collection string) ([]byte, error) {
	if len(geojsonData) < 50 {
		return nil, fmt.Errorf("no features to export")
	}

	tmpDir, err := os.MkdirTemp("", "pwa_tab_*")
	if err != nil {
		return nil, fmt.Errorf("temp dir error: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	inputPath := filepath.Join(tmpDir, "input.geojson")
	if err := os.WriteFile(inputPath, geojsonData, 0644); err != nil {
		return nil, fmt.Errorf("write input failed: %w", err)
	}

	outDir := filepath.Join(tmpDir, "tab_out")
	os.MkdirAll(outDir, 0755)
	tabFilename := fmt.Sprintf("%s_%s.tab", pwaCode, collection)
	tabPath := filepath.Join(outDir, tabFilename)

	cmd := exec.Command(ogr2ogrPath,
		"-f", "MapInfo File",
		tabPath,
		inputPath,
		"-lco", "ENCODING=UTF-8",
		"-nln", collection,
		"-overwrite",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ogr2ogr TAB failed: %s — %w", string(output), err)
	}

	zipBuf, err := zipDirectory(outDir, []string{".tab", ".dat", ".map", ".id", ".ind"})
	if err != nil {
		return nil, fmt.Errorf("zip creation failed: %w", err)
	}

	log.Printf("[Export] TAB: %s/%s → %d bytes (zip)", pwaCode, collection, zipBuf.Len())
	return zipBuf.Bytes(), nil
}

// ExportAsPMTiles converts GeoJSON to PMTiles vector tiles.
// Priority: tippecanoe → ogr2ogr+GPKG+pmtiles → error with install hint.
func ExportAsPMTiles(pwaCode, collection, startDate, endDate string) ([]byte, error) {
	geojsonData, err := ExportFeaturesAsGeoJSON(pwaCode, collection, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("GeoJSON export failed: %w", err)
	}
	return convertGeoJSONToPMTiles(geojsonData, pwaCode, collection)
}

// ExportMergedAsPMTiles converts pre-merged GeoJSON to PMTiles.
func ExportMergedAsPMTiles(geojsonData []byte, outputName string) ([]byte, error) {
	return convertGeoJSONToPMTiles(geojsonData, outputName, "merged")
}

func convertGeoJSONToPMTiles(geojsonData []byte, pwaCode, collection string) ([]byte, error) {
	if len(geojsonData) < 50 {
		return nil, fmt.Errorf("no features to export")
	}

	tmpDir, err := os.MkdirTemp("", "pwa_pmtiles_*")
	if err != nil {
		return nil, fmt.Errorf("temp dir error: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	inputPath := filepath.Join(tmpDir, "input.geojson")
	if err := os.WriteFile(inputPath, geojsonData, 0644); err != nil {
		return nil, fmt.Errorf("write input failed: %w", err)
	}

	outputPath := filepath.Join(tmpDir, fmt.Sprintf("%s_%s.pmtiles", pwaCode, collection))

	// ──────────────────────────────────────────
	// Strategy A: tippecanoe (best quality, Linux/WSL only)
	// ──────────────────────────────────────────
	if tippePath, err := exec.LookPath("tippecanoe"); err == nil {
		log.Printf("[Export] PMTiles via tippecanoe: %s/%s", pwaCode, collection)

		cmd := exec.Command(tippePath,
			"-o", outputPath,
			"-z", "14", "-Z", "4",
			"--drop-densest-as-needed",
			"--extend-zooms-if-still-dropping",
			"-l", collection,
			"--force",
			inputPath,
		)
		out, err := cmd.CombinedOutput()
		if err == nil {
			data, readErr := os.ReadFile(outputPath)
			if readErr == nil {
				log.Printf("[Export] PMTiles (tippecanoe): %s/%s → %d bytes", pwaCode, collection, len(data))
				return data, nil
			}
		}
		log.Printf("[Export] tippecanoe failed: %v — %s", err, string(out))
	}

	// ──────────────────────────────────────────
	// Strategy B: ogr2ogr → MBTiles → pmtiles convert
	// ──────────────────────────────────────────
	ogr2ogrPath, ogrErr := findOgr2ogr()
	if ogrErr == nil {
		mbtilesPath := filepath.Join(tmpDir, "output.mbtiles")

		// Try MBTiles first (Linux GDAL usually has it)
		cmd := exec.Command(ogr2ogrPath,
			"-f", "MBTiles",
			mbtilesPath,
			inputPath,
			"-dsco", "MAXZOOM=14",
			"-dsco", "MINZOOM=4",
		)
		out, err := cmd.CombinedOutput()

		if err != nil {
			log.Printf("[Export] ogr2ogr MBTiles not available: %s", string(out))

			// ──────────────────────────────────────────
			// Strategy C: ogr2ogr → GeoPackage (fallback for Windows GDAL)
			// Windows GDAL typically has GPKG but not MBTiles
			// ──────────────────────────────────────────
			gpkgPath := filepath.Join(tmpDir, fmt.Sprintf("%s_%s.gpkg", pwaCode, collection))
			gpkgCmd := exec.Command(ogr2ogrPath,
				"-f", "GPKG",
				gpkgPath,
				inputPath,
				"-nln", collection,
				"-overwrite",
			)
			gpkgOut, gpkgErr := gpkgCmd.CombinedOutput()
			if gpkgErr != nil {
				log.Printf("[Export] ogr2ogr GPKG also failed: %s", string(gpkgOut))
			} else {
				// Return GPKG as fallback (user can convert to PMTiles later)
				data, readErr := os.ReadFile(gpkgPath)
				if readErr == nil {
					log.Printf("[Export] ⚠️ PMTiles not available, returning GeoPackage: %s/%s → %d bytes", pwaCode, collection, len(data))
					return data, nil
				}
			}

			// All GDAL strategies failed
			return nil, fmt.Errorf("PMTiles export ไม่สามารถทำได้บน Windows โดยตรง\n" +
				"ทางเลือก:\n" +
				"1. ติดตั้ง tippecanoe ผ่าน WSL: wsl sudo apt install tippecanoe\n" +
				"2. ใช้ Export GeoJSON แล้วแปลงด้วย https://felt.com/tippecanoe\n" +
				"3. ใช้ Export GeoPackage (.gpkg) แทน")
		}

		// MBTiles succeeded — try pmtiles convert
		if _, pmErr := exec.LookPath("pmtiles"); pmErr == nil {
			convertCmd := exec.Command("pmtiles", "convert", mbtilesPath, outputPath)
			if convertOut, convertErr := convertCmd.CombinedOutput(); convertErr != nil {
				log.Printf("[Export] pmtiles convert failed: %s", string(convertOut))
			} else {
				data, readErr := os.ReadFile(outputPath)
				if readErr == nil {
					log.Printf("[Export] PMTiles (ogr2ogr+pmtiles): %s/%s → %d bytes", pwaCode, collection, len(data))
					return data, nil
				}
			}
		}

		// Return MBTiles as fallback
		data, readErr := os.ReadFile(mbtilesPath)
		if readErr == nil {
			log.Printf("[Export] ⚠️ pmtiles CLI not found, returning MBTiles: %s/%s → %d bytes", pwaCode, collection, len(data))
			return data, nil
		}
	}

	return nil, fmt.Errorf("PMTiles export requires tippecanoe or GDAL with MBTiles driver.\n" +
		"Windows: ติดตั้ง tippecanoe ผ่าน WSL หรือใช้ Export GeoJSON แทน")
}

// ========================================================================
// Helper functions
// ========================================================================

// findOgr2ogr locates the ogr2ogr executable on the system.
// On Windows, checks common installation paths if not in PATH.
func findOgr2ogr() (string, error) {
	// Check PATH first
	if p, err := exec.LookPath("ogr2ogr"); err == nil {
		return p, nil
	}

	// On Windows, check common GDAL installation paths
	if runtime.GOOS == "windows" {
		commonPaths := []string{
			`C:\OSGeo4W64\bin\ogr2ogr.exe`,
			`C:\OSGeo4W\bin\ogr2ogr.exe`,
			`C:\Program Files\GDAL\ogr2ogr.exe`,
			`C:\Program Files (x86)\GDAL\ogr2ogr.exe`,
			`C:\GDAL\ogr2ogr.exe`,
		}
		for _, p := range commonPaths {
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}

	return "", fmt.Errorf("ogr2ogr (GDAL) not found.\n" +
		"Windows: ติดตั้ง OSGeo4W → https://trac.osgeo.org/osgeo4w/\n" +
		"Linux: sudo apt install gdal-bin")
}

// zipDirectory creates a zip archive containing files from dir matching given extensions.
// Uses Go's archive/zip (cross-platform, no external zip command needed).
func zipDirectory(dir string, extensions []string) (*bytes.Buffer, error) {
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	fileCount := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(entry.Name()))
		match := false
		for _, e := range extensions {
			if ext == e {
				match = true
				break
			}
		}
		if !match {
			continue
		}

		// Read file
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}

		// Add to zip
		f, err := w.Create(entry.Name())
		if err != nil {
			continue
		}
		if _, err := f.Write(data); err != nil {
			continue
		}
		fileCount++
	}

	if err := w.Close(); err != nil {
		return nil, err
	}

	if fileCount == 0 {
		return nil, fmt.Errorf("no files to zip")
	}

	return buf, nil
}