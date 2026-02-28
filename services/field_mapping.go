package services

// ==========================================
// Field Mapping: MongoDB (PWA GIS Online) -> Postgres
// อ้างอิงจาก เปรียบเทียบ Datadic GIS.xlsx
// ==========================================

// FieldMapping เก็บ mapping ของแต่ละ collection
// key = MongoDB field name (ใน properties)
// value = Postgres field name (ชื่อ key ใหม่ใน response)
var FieldMapping = map[string]map[string]string{

	// ==========================================
	// PIPE - Tab PIPE
	// ==========================================
	"pipe": {
		"PIPE_ID":      "pipe_id",
		"projectNo":    "project_no",
		"promiseDate":  "contrac_date",
		"checkDate":    "cap_date",
		"assetCode":    "asset_code",
		"typeId":       "pipe_type",
		"gradeId":      "grade",
		"sizeId":       "pipe_size",
		"classId":      "class",
		"functionId":   "pipe_func",
		"layingId":     "laying",
		"productId":    "product",
		"depth":        "depth",
		"length":       "pipe_long",
		"yearInstall":  "yearinstall",
		"locate":       "locate",
		"pwaCode":      "pwa_code",
		"recordDate":   "rec_date",
		"remark":       "remark",
		"oldProjectNo": "old_project_no",
		"pipeIdPrev":   "pipe_id_prev",
		"_createdBy":   "password",
	},

	// ==========================================
	// VALVE - Tab VALVE
	// ==========================================
	"valve": {
		"VALVE_ID":    "valve_id",
		"typeId":      "valve_type",
		"sizeId":      "valve_size",
		"statusId":    "valve_status",
		"depth":       "depth",
		"roundOpen":   "round_open",
		"yearInstall": "yearinstall",
		"drawingPath": "drawingpath",
		"picturePath": "picturepath",
		"pwaCode":     "pwa_code",
		"recordDate":  "rec_date",
		"remark":      "remark",
		"_createdBy":  "password",
	},

	// ==========================================
	// FIREHYDRANT - Tab FIREHYDRANT
	// ==========================================
	"firehydrant": {
		"FIRE_ID":         "fire_id",
		"sizeId":          "fire_size",
		"statusId":        "fire_status",
		"pressure":        "pressure",
		"pressureHistory": "pressure_history",
		"picturePath":     "picturepath",
		"pwaCode":         "pwa_code",
		"recordDate":      "rec_date",
		"remark":          "remark",
		"_createdBy":      "password",
	},

	// ==========================================
	// METER - Tab METER
	// ==========================================
	"meter": {
		"BLDG_ID":     	"bldg_id",
		"pipeId":         "pipe_id",
		"custCode":       "custcode",
		"custFullName":   "custname",
		"meterNo":        "meterno",
		"meterSizeCode":  "metersize",
		"beginCustDate":  "bgncustdt",
		"meterRouteCode": "mtrrdroute",
		"meterRouteSeq":  "mtrseq",
		"addressNo":      "addrno",
		"custStat":       "custstat",
		"pwaCode":        "pwa_code",
		"recordDate":     "rec_date",
		"remark":         "remark",
		"_createdBy":     "password",
	},

	// ==========================================
	// BLDG - Tab BLDG
	// ==========================================
	"bldg": {
		"BLDG_ID":            "bldg_id",
		"houseCode":      "housecode",
		"useStatusId":    "use_status",
		"custCode":       "custcode",
		"custFullName":   "custname",
		"useTypeId":      "usetype",
		"buildingTypeId": "bl_type",
		"addressNo":      "addrno",
		"pwaCode":        "pwa_code",
		"building":       "building",
		"floor":          "floor",
		"villageNo":      "villageno",
		"village":        "village",
		"soi":            "soi",
		"road":           "road",
		"subDistrict":    "subdistrict",
		"district":       "district",
		"province":       "province",
		"zipcode":        "zipcode",
		"custCodeOld":    "custcode_old",
		"recordDate":     "rec_date",
		"remark":         "remark",
		"_createdBy":     "password",
	},

	// ==========================================
	// LEAKPOINT - Tab LEAKPOINT
	// ==========================================
	"leakpoint": {
		"LEAK_ID":         "leak_id",
		"leakNo":          "leak_no",
		"leakDatetime":    "leakdate",
		"locate":          "locate",
		"cause":           "leakcause",
		"depth":           "leakdepth",
		"picturePath":     "picturepath",
		"repairBy":        "repairby",
		"repairCost":      "repaircost",
		"repairDatetime":  "repairdate",
		"detail":          "leakdetail",
		"checker":         "leakchecker",
		"pipeId":          "pipe_id",
		"pipeTypeId":      "pipe_type",
		"pipeSizesId":     "pipe_size",
		"informer":        "leak_informer",
		"pwaCode":         "pwa_code",
		"recordDate":      "rec_date",
		"remark":          "remark",
		"typeId":          "leak_type",
		"LEAKCAUSE_ID":    "leakcause_id",
		"LEAK_WOUND":      "leak_wound",
		"_createdBy":      "password",
	},

	// ==========================================
	// PWA_WATERWORKS - Tab PWA_WATERWORKS
	// (ไม่มี mapping fields ใน Excel นอกจาก _id)
	// ==========================================
	"pwa_waterworks": {},
}

// MapProperties - แปลง MongoDB properties ให้ใช้ชื่อ field แบบ Postgres
// เฉพาะ field ที่อยู่ใน mapping เท่านั้นจะถูกส่งออก
func MapProperties(collectionType string, mongoProps map[string]interface{}) map[string]interface{} {
	mapping, exists := FieldMapping[collectionType]
	if !exists {
		return mongoProps
	}

	// ถ้าไม่มี mapping (เช่น pwa_waterworks) ส่งทั้งหมดกลับไป
	if len(mapping) == 0 {
		return mongoProps
	}

	mapped := make(map[string]interface{})

	for mongoKey, pgKey := range mapping {
		if val, ok := mongoProps[mongoKey]; ok {
			mapped[pgKey] = val
		}
	}

	return mapped
}