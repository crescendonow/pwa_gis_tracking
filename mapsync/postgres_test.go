package mapsync

import (
	"strings"
	"testing"
)

func TestBuildUpsertStatementPlaceholders(t *testing.T) {
	statement := buildUpsertStatement(3)
	if count := strings.Count(statement, "$"); count != 30 {
		t.Fatalf("placeholder count = %d, want 30 for 3 rows x 10 params: %s", count, statement)
	}
	if !strings.Contains(statement, "ON CONFLICT (source_collection, source_id) DO UPDATE") {
		t.Fatalf("statement missing ON CONFLICT clause: %s", statement)
	}
	if !strings.Contains(statement, "$21") || !strings.Contains(statement, "$30") {
		t.Fatalf("statement missing expected placeholders for row 3: %s", statement)
	}
	if !strings.Contains(statement, "ST_SetSRID(ST_GeomFromGeoJSON($5),4326)") {
		t.Fatalf("statement missing geometry conversion for row 1: %s", statement)
	}
}

func TestBuildUpsertStatementSingleRow(t *testing.T) {
	statement := buildUpsertStatement(1)
	if count := strings.Count(statement, "$"); count != 10 {
		t.Fatalf("placeholder count = %d, want 10 for 1 row: %s", count, statement)
	}
}

func TestUpsertDMAColorsStatement(t *testing.T) {
	statement := buildDMAColorUpsertStatement(2)
	if count := strings.Count(statement, "$"); count != 8 {
		t.Fatalf("placeholder count = %d, want 8 for 2 rows x 4 params: %s", count, statement)
	}
	if !strings.Contains(statement, "ON CONFLICT (pwa_code, dma_id) DO UPDATE") {
		t.Fatalf("statement missing ON CONFLICT clause: %s", statement)
	}
	if !strings.Contains(statement, "$5") || !strings.Contains(statement, "$8") {
		t.Fatalf("statement missing expected placeholders for row 2: %s", statement)
	}
}
