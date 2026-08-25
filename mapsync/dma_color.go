package mapsync

import (
	"context"
	"database/sql"
)

// DMAColorSource reads DMA colours from the authoritative PostGIS database
// (pgweb_gis2), which lives on a different server than the mirror. Values
// are normalised through NormalizeDMAColors so the mirror never stores an
// unsafe or empty colour.
type DMAColorSource struct{ DB *sql.DB }

func (source DMAColorSource) Load(ctx context.Context) ([]DMAColor, error) {
	rows, err := source.DB.QueryContext(ctx, `SELECT pwa_code::text, dma_id::text, sld_color_fill, sld_color_stroke FROM pwa_dma.dma_boundary`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	colors := make([]DMAColor, 0)
	for rows.Next() {
		var color DMAColor
		var fill, stroke sql.NullString
		if err := rows.Scan(&color.PwaCode, &color.DMAID, &fill, &stroke); err != nil {
			return nil, err
		}
		color.Fill, color.Stroke = NormalizeDMAColors(fill.String, stroke.String)
		colors = append(colors, color)
	}
	return colors, rows.Err()
}
