package services

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"pwa_gis_tracking/config"

	flatbuffers "github.com/google/flatbuffers/go"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ========================================================================
// FlatGeobuf Binary Writer — Pure Go (no GDAL)
// Spec: https://flatgeobuf.org/
// Uses google/flatbuffers/go for FlatBuffer serialization.
// ========================================================================

// FlatGeobuf magic bytes (8 bytes)
var fgbMagic = [8]byte{0x66, 0x67, 0x62, 0x03, 0x66, 0x67, 0x62, 0x00}

// Geometry type constants (matching FlatGeobuf spec)
const (
	fgbGeomUnknown         byte = 0
	fgbGeomPoint           byte = 1
	fgbGeomLineString      byte = 2
	fgbGeomPolygon         byte = 3
	fgbGeomMultiPoint      byte = 4
	fgbGeomMultiLineString byte = 5
	fgbGeomMultiPolygon    byte = 6
)

// Column type constants
const (
	fgbColByte     byte = 0
	fgbColUByte    byte = 1
	fgbColBool     byte = 2
	fgbColShort    byte = 3
	fgbColUShort   byte = 4
	fgbColInt      byte = 5
	fgbColUInt     byte = 6
	fgbColLong     byte = 7
	fgbColULong    byte = 8
	fgbColFloat    byte = 9
	fgbColDouble   byte = 10
	fgbColString   byte = 11
	fgbColJSON     byte = 12
	fgbColDateTime byte = 13
	fgbColBinary   byte = 14
)

// fgbColumn defines a property column in the FlatGeobuf header.
type fgbColumn struct {
	Name string
	Type byte
}

// fgbFeature holds a single feature for FlatGeobuf serialization.
type fgbFeature struct {
	GeomType byte
	XY       []float64 // interleaved x, y, x, y, ...
	Ends     []uint32  // ring/part end indices (for Polygon, Multi* types)
	Parts    []fgbFeature // sub-geometries for Multi* types
	Props    map[string]interface{}
}

// ExportAsFlatGeobuf queries MongoDB and returns a FlatGeobuf binary file.
func ExportAsFlatGeobuf(pwaCode, layerName, startDate, endDate string) ([]byte, error) {
	collectionID, err := FindCollectionID(pwaCode, layerName)
	if err != nil {
		return nil, fmt.Errorf("collection not found: %s_%s", pwaCode, layerName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	featuresCol := config.GetMongoCollection(fmt.Sprintf("features_%s", collectionID))

	filter := bson.M{}
	if startDate != "" || endDate != "" {
		layerCfg := LayerConfigs[layerName]
		dateField := "properties." + layerCfg.DateField
		dateFilter := buildDateFilter(dateField, startDate, endDate)
		if dateFilter != nil {
			filter = dateFilter
		}
	}

	projection := options.Find().SetProjection(bson.M{
		"geometry":   1,
		"properties": 1,
		"_id":        0,
	})

	cursor, err := featuresCol.Find(ctx, filter, projection)
	if err != nil {
		return nil, fmt.Errorf("query failed: %v", err)
	}
	defer cursor.Close(ctx)

	// Collect features and discover columns
	var features []fgbFeature
	columnOrder := []string{}       // preserve insertion order
	columnSet := map[string]byte{}  // name -> FGB column type
	var primaryGeomType byte = fgbGeomUnknown

	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}

		geomRaw := doc["geometry"]
		if geomRaw == nil {
			continue
		}

		geomMap, ok := geomRaw.(bson.M)
		if !ok {
			continue
		}

		feat := fgbFeature{Props: make(map[string]interface{})}

		// Parse geometry
		if err := parseGeometry(geomMap, &feat); err != nil {
			log.Printf("FGB: skip feature, geom parse error: %v", err)
			continue
		}

		// Set primary geometry type from first feature
		if primaryGeomType == fgbGeomUnknown {
			primaryGeomType = feat.GeomType
		}

		// Parse properties
		if props, ok := doc["properties"].(bson.M); ok {
			for k, v := range props {
				switch val := v.(type) {
				case string:
					feat.Props[k] = val
					if _, exists := columnSet[k]; !exists {
						columnSet[k] = fgbColString
						columnOrder = append(columnOrder, k)
					}
				case int32:
					feat.Props[k] = int64(val)
					if _, exists := columnSet[k]; !exists {
						columnSet[k] = fgbColInt
						columnOrder = append(columnOrder, k)
					}
				case int64:
					feat.Props[k] = val
					if _, exists := columnSet[k]; !exists {
						columnSet[k] = fgbColLong
						columnOrder = append(columnOrder, k)
					}
				case float64:
					feat.Props[k] = val
					if _, exists := columnSet[k]; !exists {
						columnSet[k] = fgbColDouble
						columnOrder = append(columnOrder, k)
					}
				case bool:
					feat.Props[k] = val
					if _, exists := columnSet[k]; !exists {
						columnSet[k] = fgbColBool
						columnOrder = append(columnOrder, k)
					}
				case primitive.DateTime:
					feat.Props[k] = val.Time().Format(time.RFC3339)
					if _, exists := columnSet[k]; !exists {
						columnSet[k] = fgbColDateTime
						columnOrder = append(columnOrder, k)
					}
				case bson.M, bson.A:
					continue // skip nested types
				default:
					feat.Props[k] = fmt.Sprintf("%v", v)
					if _, exists := columnSet[k]; !exists {
						columnSet[k] = fgbColString
						columnOrder = append(columnOrder, k)
					}
				}
			}
		}

		features = append(features, feat)
	}

	if len(features) == 0 {
		return nil, fmt.Errorf("no features found for %s_%s", pwaCode, layerName)
	}

	// Build column list in discovery order
	columns := make([]fgbColumn, len(columnOrder))
	for i, name := range columnOrder {
		columns[i] = fgbColumn{Name: name, Type: columnSet[name]}
	}

	// Build the FlatGeobuf binary
	return buildFlatGeobuf(features, columns, primaryGeomType, layerName)
}

// ========================================================================
// Geometry Parsing (MongoDB BSON → fgbFeature)
// ========================================================================

// parseGeometry converts a GeoJSON-like BSON geometry into fgbFeature fields.
func parseGeometry(geom bson.M, feat *fgbFeature) error {
	geomType, _ := geom["type"].(string)
	coords := geom["coordinates"]

	switch geomType {
	case "Point":
		feat.GeomType = fgbGeomPoint
		c, err := toCoordPair(coords)
		if err != nil {
			return err
		}
		feat.XY = c
		return nil

	case "MultiPoint":
		feat.GeomType = fgbGeomMultiPoint
		xy, err := toCoordArray(coords)
		if err != nil {
			return err
		}
		feat.XY = xy
		return nil

	case "LineString":
		feat.GeomType = fgbGeomLineString
		xy, err := toCoordArray(coords)
		if err != nil {
			return err
		}
		feat.XY = xy
		return nil

	case "MultiLineString":
		feat.GeomType = fgbGeomMultiLineString
		rings, err := toCoordRings(coords)
		if err != nil {
			return err
		}
		feat.XY, feat.Ends = flattenRings(rings)
		return nil

	case "Polygon":
		feat.GeomType = fgbGeomPolygon
		rings, err := toCoordRings(coords)
		if err != nil {
			return err
		}
		feat.XY, feat.Ends = flattenRings(rings)
		return nil

	case "MultiPolygon":
		feat.GeomType = fgbGeomMultiPolygon
		polygons, err := toCoordPolygons(coords)
		if err != nil {
			return err
		}
		// Flatten all rings from all polygons
		var allXY []float64
		var allEnds []uint32
		coordIdx := uint32(0)
		for _, poly := range polygons {
			for _, ring := range poly {
				for _, c := range ring {
					allXY = append(allXY, c...)
				}
				coordIdx += uint32(len(ring))
				allEnds = append(allEnds, coordIdx)
			}
		}
		feat.XY = allXY
		feat.Ends = allEnds
		return nil

	default:
		return fmt.Errorf("unsupported geometry type: %s", geomType)
	}
}

// toCoordPair extracts [x, y] from a BSON coordinate value.
func toCoordPair(v interface{}) ([]float64, error) {
	arr, ok := v.(bson.A)
	if !ok || len(arr) < 2 {
		return nil, fmt.Errorf("invalid coordinate pair")
	}
	x, _ := toFloat64(arr[0])
	y, _ := toFloat64(arr[1])
	return []float64{x, y}, nil
}

// toCoordArray extracts [[x,y], [x,y], ...] → interleaved [x1,y1,x2,y2,...].
func toCoordArray(v interface{}) ([]float64, error) {
	arr, ok := v.(bson.A)
	if !ok {
		return nil, fmt.Errorf("invalid coordinate array")
	}
	xy := make([]float64, 0, len(arr)*2)
	for _, item := range arr {
		pair, ok := item.(bson.A)
		if !ok || len(pair) < 2 {
			continue
		}
		x, _ := toFloat64(pair[0])
		y, _ := toFloat64(pair[1])
		xy = append(xy, x, y)
	}
	return xy, nil
}

// toCoordRings extracts [[[x,y],...], [[x,y],...]] (ring arrays).
func toCoordRings(v interface{}) ([][][]float64, error) {
	arr, ok := v.(bson.A)
	if !ok {
		return nil, fmt.Errorf("invalid rings")
	}
	var rings [][][]float64
	for _, ringRaw := range arr {
		ring, ok := ringRaw.(bson.A)
		if !ok {
			continue
		}
		var coords [][]float64
		for _, ptRaw := range ring {
			pt, ok := ptRaw.(bson.A)
			if !ok || len(pt) < 2 {
				continue
			}
			x, _ := toFloat64(pt[0])
			y, _ := toFloat64(pt[1])
			coords = append(coords, []float64{x, y})
		}
		rings = append(rings, coords)
	}
	return rings, nil
}

// toCoordPolygons extracts [[[[x,y],...], ...], ...] (array of polygons).
func toCoordPolygons(v interface{}) ([][][][]float64, error) {
	arr, ok := v.(bson.A)
	if !ok {
		return nil, fmt.Errorf("invalid multi-polygon")
	}
	var polys [][][][]float64
	for _, polyRaw := range arr {
		rings, err := toCoordRings(polyRaw)
		if err != nil {
			continue
		}
		polys = append(polys, rings)
	}
	return polys, nil
}

// flattenRings converts rings to interleaved XY and ends arrays.
func flattenRings(rings [][][]float64) ([]float64, []uint32) {
	var xy []float64
	var ends []uint32
	coordIdx := uint32(0)
	for _, ring := range rings {
		for _, c := range ring {
			xy = append(xy, c...)
		}
		coordIdx += uint32(len(ring))
		ends = append(ends, coordIdx)
	}
	return xy, ends
}

// toFloat64 converts a BSON numeric value to float64.
func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int32:
		return float64(val), true
	case int64:
		return float64(val), true
	default:
		return 0, false
	}
}

// ========================================================================
// FlatGeobuf Binary Assembly
// ========================================================================

// buildFlatGeobuf assembles the complete FlatGeobuf binary file.
func buildFlatGeobuf(features []fgbFeature, columns []fgbColumn, geomType byte, name string) ([]byte, error) {
	var buf bytes.Buffer

	// 1. Write magic bytes
	buf.Write(fgbMagic[:])

	// 2. Build header FlatBuffer
	headerBytes := buildFGBHeader(name, columns, geomType, uint64(len(features)))

	// 3. Write header size prefix + header
	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(headerBytes))); err != nil {
		return nil, err
	}
	buf.Write(headerBytes)

	// 4. Build and write each feature
	for _, f := range features {
		featureBytes := buildFGBFeature(f, columns)
		if err := binary.Write(&buf, binary.LittleEndian, uint32(len(featureBytes))); err != nil {
			return nil, err
		}
		buf.Write(featureBytes)
	}

	return buf.Bytes(), nil
}

// ========================================================================
// FlatBuffer Header Construction
// ========================================================================

// buildFGBHeader creates the FlatBuffer-encoded Header table.
func buildFGBHeader(name string, columns []fgbColumn, geomType byte, featureCount uint64) []byte {
	builder := flatbuffers.NewBuilder(4096)

	// Pre-create strings and sub-tables (must be done before StartObject)
	nameOffset := builder.CreateString(name)

	// Build Crs table: EPSG:4326
	crsOrgOffset := builder.CreateString("EPSG")
	crsNameOffset := builder.CreateString("WGS 84")
	// Crs table: 6 fields (org=0, code=1, name=2, description=3, wkt=4, code_string=5)
	builder.StartObject(6)
	builder.PrependUOffsetTSlot(0, crsOrgOffset, 0)
	builder.PrependInt32Slot(1, 4326, 0)
	builder.PrependUOffsetTSlot(2, crsNameOffset, 0)
	crsOffset := builder.EndObject()

	// Build Column tables
	colOffsets := make([]flatbuffers.UOffsetT, len(columns))
	for i := len(columns) - 1; i >= 0; i-- {
		colNameOffset := builder.CreateString(columns[i].Name)
		// Column table: 11 fields (name=0, type=1, ...)
		builder.StartObject(11)
		builder.PrependUOffsetTSlot(0, colNameOffset, 0)
		builder.PrependByteSlot(1, columns[i].Type, 0)
		builder.PrependBoolSlot(7, true, false) // nullable = true
		colOffsets[i] = builder.EndObject()
	}

	// Build columns vector
	builder.StartVector(4, len(columns), 4) // UOffsetT = 4 bytes each
	for i := len(columns) - 1; i >= 0; i-- {
		builder.PrependUOffsetT(colOffsets[i])
	}
	columnsVecOffset := builder.EndVector(len(columns))

	// Header table: 14 fields
	// (name=0, envelope=1, geometry_type=2, has_z=3, has_m=4, has_t=5, has_tm=6,
	//  columns=7, features_count=8, index_node_size=9, crs=10, title=11, description=12, metadata=13)
	builder.StartObject(14)
	builder.PrependUOffsetTSlot(0, nameOffset, 0)         // name
	builder.PrependByteSlot(2, geomType, 0)               // geometry_type
	builder.PrependBoolSlot(3, false, false)               // has_z
	builder.PrependBoolSlot(4, false, false)               // has_m
	builder.PrependBoolSlot(5, false, false)               // has_t
	builder.PrependBoolSlot(6, false, false)               // has_tm
	builder.PrependUOffsetTSlot(7, columnsVecOffset, 0)    // columns
	builder.PrependUint64Slot(8, featureCount, 0)          // features_count
	builder.PrependUint16Slot(9, 0, 16)                    // index_node_size = 0 (no index)
	builder.PrependUOffsetTSlot(10, crsOffset, 0)          // crs
	headerOffset := builder.EndObject()

	builder.Finish(headerOffset)
	return builder.FinishedBytes()
}

// ========================================================================
// FlatBuffer Feature Construction
// ========================================================================

// buildFGBFeature creates the FlatBuffer-encoded Feature table.
func buildFGBFeature(f fgbFeature, columns []fgbColumn) []byte {
	builder := flatbuffers.NewBuilder(2048)

	// 1. Build Geometry sub-table
	geomOffset := buildFGBGeometry(builder, f)

	// 2. Build properties byte array
	propsBytes := encodeProperties(f.Props, columns)
	propsOffset := builder.CreateByteVector(propsBytes)

	// 3. Build Feature table: 3 fields (geometry=0, properties=1, columns=2)
	builder.StartObject(3)
	builder.PrependUOffsetTSlot(0, geomOffset, 0) // geometry
	builder.PrependUOffsetTSlot(1, propsOffset, 0) // properties
	featureOffset := builder.EndObject()

	builder.Finish(featureOffset)
	return builder.FinishedBytes()
}

// buildFGBGeometry creates a Geometry FlatBuffer table inside the given builder.
func buildFGBGeometry(builder *flatbuffers.Builder, f fgbFeature) flatbuffers.UOffsetT {
	// Build XY vector (double[])
	var xyOffset flatbuffers.UOffsetT
	if len(f.XY) > 0 {
		builder.StartVector(8, len(f.XY), 8) // 8 bytes per double
		for i := len(f.XY) - 1; i >= 0; i-- {
			builder.PrependFloat64(f.XY[i])
		}
		xyOffset = builder.EndVector(len(f.XY))
	}

	// Build ends vector (uint32[])
	var endsOffset flatbuffers.UOffsetT
	if len(f.Ends) > 0 {
		builder.StartVector(4, len(f.Ends), 4) // 4 bytes per uint32
		for i := len(f.Ends) - 1; i >= 0; i-- {
			builder.PrependUint32(f.Ends[i])
		}
		endsOffset = builder.EndVector(len(f.Ends))
	}

	// Geometry table: 8 fields (ends=0, xy=1, z=2, m=3, t=4, tm=5, type=6, parts=7)
	builder.StartObject(8)
	if len(f.Ends) > 0 {
		builder.PrependUOffsetTSlot(0, endsOffset, 0)
	}
	if len(f.XY) > 0 {
		builder.PrependUOffsetTSlot(1, xyOffset, 0)
	}
	builder.PrependByteSlot(6, f.GeomType, 0)
	return builder.EndObject()
}

// ========================================================================
// Property Encoding (FlatGeobuf binary properties format)
// ========================================================================

// encodeProperties encodes feature properties into the FlatGeobuf binary format.
// Format: repeated [uint16 column_index][value]
//   - String/DateTime: uint32 length + UTF-8 bytes
//   - Int:             int32 (4 bytes LE)
//   - Long:            int64 (8 bytes LE)
//   - Double/Float:    float64 (8 bytes LE)
//   - Bool:            1 byte (0x00 or 0x01)
func encodeProperties(props map[string]interface{}, columns []fgbColumn) []byte {
	var buf bytes.Buffer

	for colIdx, col := range columns {
		val, exists := props[col.Name]
		if !exists || val == nil {
			continue
		}

		// Write column index (uint16 LE)
		binary.Write(&buf, binary.LittleEndian, uint16(colIdx))

		switch col.Type {
		case fgbColString, fgbColDateTime, fgbColJSON:
			s := fmt.Sprintf("%v", val)
			b := []byte(s)
			binary.Write(&buf, binary.LittleEndian, uint32(len(b)))
			buf.Write(b)

		case fgbColInt:
			var n int32
			switch v := val.(type) {
			case int64:
				n = int32(v)
			case float64:
				n = int32(v)
			default:
				n = 0
			}
			binary.Write(&buf, binary.LittleEndian, n)

		case fgbColLong:
			var n int64
			switch v := val.(type) {
			case int64:
				n = v
			case float64:
				n = int64(v)
			default:
				n = 0
			}
			binary.Write(&buf, binary.LittleEndian, n)

		case fgbColDouble, fgbColFloat:
			var n float64
			switch v := val.(type) {
			case float64:
				n = v
			case int64:
				n = float64(v)
			default:
				n = 0
			}
			binary.Write(&buf, binary.LittleEndian, n)

		case fgbColBool:
			b, _ := val.(bool)
			if b {
				buf.WriteByte(1)
			} else {
				buf.WriteByte(0)
			}

		default:
			// Fallback: encode as string
			s := fmt.Sprintf("%v", val)
			b := []byte(s)
			binary.Write(&buf, binary.LittleEndian, uint32(len(b)))
			buf.Write(b)
		}
	}

	return buf.Bytes()
}

// ========================================================================
// Merged Export: GeoJSON bytes → FlatGeobuf (Pure Go, no GDAL)
// ========================================================================

// geojsonFC is a minimal GeoJSON FeatureCollection for parsing.
type geojsonFC struct {
	Type     string        `json:"type"`
	Features []geojsonFeat `json:"features"`
}

type geojsonFeat struct {
	Type       string                 `json:"type"`
	Geometry   geojsonGeom            `json:"geometry"`
	Properties map[string]interface{} `json:"properties"`
}

type geojsonGeom struct {
	Type        string      `json:"type"`
	Coordinates interface{} `json:"coordinates"`
}

// ExportMergedAsFlatGeobuf converts pre-merged GeoJSON bytes to FlatGeobuf binary.
// Uses the same pure-Go FGB writer as single export (no ogr2ogr needed).
func ExportMergedAsFlatGeobuf(geojsonData []byte, outputName string) ([]byte, error) {
	var fc geojsonFC
	if err := json.Unmarshal(geojsonData, &fc); err != nil {
		return nil, fmt.Errorf("parse GeoJSON failed: %w", err)
	}
	if len(fc.Features) == 0 {
		return nil, fmt.Errorf("no features in merged GeoJSON")
	}

	// Convert GeoJSON features → fgbFeature list + discover columns
	var features []fgbFeature
	columnOrder := []string{}
	columnSet := map[string]byte{}
	var primaryGeomType byte = fgbGeomUnknown

	for _, gf := range fc.Features {
		if gf.Geometry.Type == "" {
			continue
		}

		feat := fgbFeature{Props: make(map[string]interface{})}

		// Re-encode geometry coordinates to bson.M for parseGeometry reuse
		geomBson := bson.M{
			"type":        gf.Geometry.Type,
			"coordinates": convertCoords(gf.Geometry.Coordinates),
		}
		if err := parseGeometry(geomBson, &feat); err != nil {
			continue
		}

		if primaryGeomType == fgbGeomUnknown {
			primaryGeomType = feat.GeomType
		}

		// Parse properties
		for k, v := range gf.Properties {
			if v == nil {
				continue
			}
			switch val := v.(type) {
			case string:
				feat.Props[k] = val
				if _, exists := columnSet[k]; !exists {
					columnSet[k] = fgbColString
					columnOrder = append(columnOrder, k)
				}
			case float64:
				// JSON numbers are always float64; detect ints
				if val == float64(int64(val)) && val >= -2147483648 && val <= 2147483647 {
					feat.Props[k] = int64(val)
					if _, exists := columnSet[k]; !exists {
						columnSet[k] = fgbColInt
						columnOrder = append(columnOrder, k)
					}
				} else {
					feat.Props[k] = val
					if _, exists := columnSet[k]; !exists {
						columnSet[k] = fgbColDouble
						columnOrder = append(columnOrder, k)
					}
				}
			case bool:
				feat.Props[k] = val
				if _, exists := columnSet[k]; !exists {
					columnSet[k] = fgbColBool
					columnOrder = append(columnOrder, k)
				}
			default:
				feat.Props[k] = fmt.Sprintf("%v", v)
				if _, exists := columnSet[k]; !exists {
					columnSet[k] = fgbColString
					columnOrder = append(columnOrder, k)
				}
			}
		}

		features = append(features, feat)
	}

	if len(features) == 0 {
		return nil, fmt.Errorf("no valid features after parsing")
	}

	columns := make([]fgbColumn, len(columnOrder))
	for i, name := range columnOrder {
		columns[i] = fgbColumn{Name: name, Type: columnSet[name]}
	}

	log.Printf("[Export] FGB (merged, pure Go): %s → %d features", outputName, len(features))
	return buildFlatGeobuf(features, columns, primaryGeomType, outputName)
}

// convertCoords recursively converts JSON-decoded coordinates ([]interface{})
// to the types that parseGeometry expects (matching bson.M from MongoDB).
// JSON arrays decode as []interface{} which is compatible with bson.A.
func convertCoords(v interface{}) interface{} {
	switch val := v.(type) {
	case []interface{}:
		// Check if this is a coordinate pair [lon, lat] (array of numbers)
		if len(val) >= 2 {
			if _, ok := val[0].(float64); ok {
				// It's a coordinate — return as primitive.A for bson compatibility
				a := primitive.A{}
				for _, n := range val {
					a = append(a, n)
				}
				return a
			}
		}
		// It's a nested array — recurse
		a := primitive.A{}
		for _, item := range val {
			a = append(a, convertCoords(item))
		}
		return a
	default:
		return v
	}
}