"""Meter status keyword mapping."""

# custStat: 1=ปกติ, 2=ฝากมาตร, 3=หยุดจ่ายน้ำ, 4=ตัดมาตร, 5=ยกเลิกถาวร
# TODO(Q1 ใน note/18_plan_for_edit_text_to_sql.md §6): ตรวจสอบข้อมูลจริง 3 สาขา (2026-08-22)
# ไม่พบค่า '3' เลย แต่พบค่า '6' ที่ไม่มีในเอกสาร schema — รอทีมข้อมูลยืนยันตารางรหัสที่ถูกต้อง
METER_STATUS_KW = {
    "มาตรตาย": ("3", "หยุดจ่ายน้ำ"),
    "หยุดจ่ายน้ำ": ("3", "หยุดจ่ายน้ำ"),
    "มาตรปกติ": ("1", "ปกติ"),
    "มาตรใช้งาน": ("1", "ปกติ"),
    "ฝากมาตร": ("2", "ฝากมาตร"),
    "ตัดมาตร": ("4", "ตัดมาตร"),
    "ยกเลิกถาวร": ("5", "ยกเลิกถาวร"),
}
_SORTED_METER_STATUS_KW = sorted(METER_STATUS_KW.keys(), key=len, reverse=True)


def cust_stat_filter(code):
    """คืน filter ที่ match ได้ทั้ง '1' (string) และ 1 (int) — custStat ใน DB ส่วนใหญ่เป็น
    string แต่บาง instance อาจเก็บเป็น int (ตามแบบ services/mongo_service.go:449)"""
    try:
        return {"$in": [str(code), int(code)]}
    except (TypeError, ValueError):
        return str(code)
