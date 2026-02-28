-- ================================================================
-- PWA GIS Online Tracking — Audit Log Table
-- PostgreSQL 9.4 compatible
-- ================================================================

-- 1. Create schema
CREATE SCHEMA IF NOT EXISTS audit_logs;

-- 2. Create table
CREATE TABLE IF NOT EXISTS audit_logs.pwagis_track_log (
    id              SERIAL PRIMARY KEY,
    user_id         VARCHAR(20),           -- รหัสพนักงาน (employee number)
    user_name       VARCHAR(200),          -- ชื่อ-สกุล
    pwa_code        VARCHAR(7),           -- รหัสสาขาของผู้ใช้
    permission_level VARCHAR(20),          -- "all", "reg", "branch"
    action          VARCHAR(100) NOT NULL, -- e.g. 'login', 'view_detail', 'export_geojson'
    target_type     VARCHAR(50),           -- 'branch', 'layer', 'export', 'map'
    target_value    TEXT,                  -- JSON or comma-separated values
    ip_address      VARCHAR(50),
    user_agent      TEXT,
    request_path    TEXT,
    request_method  VARCHAR(10),
    response_status INTEGER,
    duration_ms     INTEGER,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW()
);

-- 3. Indexes for common queries (ลบ IF NOT EXISTS ออกสำหรับ PG 9.4)
CREATE INDEX idx_audit_user_id    ON audit_logs.pwagis_track_log (user_id);
CREATE INDEX idx_audit_action     ON audit_logs.pwagis_track_log (action);
CREATE INDEX idx_audit_pwa_code   ON audit_logs.pwagis_track_log (pwa_code);
CREATE INDEX idx_audit_created_at ON audit_logs.pwagis_track_log (created_at);
CREATE INDEX idx_audit_target     ON audit_logs.pwagis_track_log (target_type, target_value);

-- 4. Comment
COMMENT ON TABLE audit_logs.pwagis_track_log IS 'ตารางบันทึกการใช้งานระบบ PWA GIS Online Tracking — อ้างอิงจาก API Intranet PWA';
COMMENT ON COLUMN audit_logs.pwagis_track_log.action IS 'login | logout | view_detail | view_map | export_geojson | export_gpkg | export_shp | export_fgb | export_tab | export_pmtiles | export_excel | click_layer_modal';
COMMENT ON COLUMN audit_logs.pwagis_track_log.permission_level IS 'all=สำนักงานใหญ่ | reg=เขต | branch=สาขา';

-- 5. Utility view: daily usage summary (เปลี่ยน FILTER เป็น SUM CASE WHEN)
CREATE OR REPLACE VIEW audit_logs.daily_usage AS
SELECT
    created_at::date AS log_date,
    user_id,
    user_name,
    pwa_code,
    permission_level,
    COUNT(*) AS total_actions,
    SUM(CASE WHEN action LIKE 'export_%' THEN 1 ELSE 0 END) AS export_count,
    SUM(CASE WHEN action = 'view_map' THEN 1 ELSE 0 END) AS map_views,
    MIN(created_at) AS first_action,
    MAX(created_at) AS last_action
FROM audit_logs.pwagis_track_log
GROUP BY created_at::date, user_id, user_name, pwa_code, permission_level
ORDER BY log_date DESC, total_actions DESC;