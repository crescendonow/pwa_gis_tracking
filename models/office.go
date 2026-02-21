package models

// PwaOffice represents a PWA branch office from pwa_office.pwa_office234 table.
type PwaOffice struct {
	PwaCode string   `json:"pwa_code" db:"pwa_code"`
	Name    string   `json:"name" db:"name"`
	Zone    string   `json:"zone" db:"zone"`
	Lat     *float64 `json:"lat,omitempty"`
	Lng     *float64 `json:"lng,omitempty"`
}

// ZoneSummary holds the count of branches per zone.
type ZoneSummary struct {
	Zone        string `json:"zone"`
	BranchCount int    `json:"branch_count"`
}

// CollectionCount holds the feature count for a specific collection.
type CollectionCount struct {
	Collection string `json:"collection"`
	PwaCode    string `json:"pwa_code"`
	BranchName string `json:"branch_name"`
	Zone       string `json:"zone"`
	Count      int64  `json:"count"`
}

// DashboardSummary represents the overall dashboard summary.
type DashboardSummary struct {
	TotalBranches int                 `json:"total_branches"`
	ZoneSummary   []ZoneSummaryDetail `json:"zone_summary"`
	LayerCounts   map[string]int64    `json:"layer_counts"`
}

// ZoneSummaryDetail holds detailed summary per zone.
type ZoneSummaryDetail struct {
	Zone        string `json:"zone"`
	BranchCount int    `json:"branch_count"`
	TotalLayers int64  `json:"total_layers"`
}

// FeatureCountByBranch holds feature counts per layer for a single branch.
type FeatureCountByBranch struct {
	PwaCode    string           `json:"pwa_code"`
	BranchName string           `json:"branch_name"`
	Zone       string           `json:"zone"`
	Layers     map[string]int64 `json:"layers"`
	Total      int64            `json:"total"`
}

// DateFilterParams holds date filter parameters from API requests.
type DateFilterParams struct {
	StartDate string `form:"startDate"` // format: 2024-01-01
	EndDate   string `form:"endDate"`   // format: 2024-12-31
	Year      string `form:"year"`      // e.g. 2024
}

// ExportRequest holds the request body for geodata export.
type ExportRequest struct {
	PwaCode    string `json:"pwa_code"`
	Collection string `json:"collection"`
	StartDate  string `json:"start_date"`
	EndDate    string `json:"end_date"`
	Format     string `json:"format"` // gpkg, geojson
}
