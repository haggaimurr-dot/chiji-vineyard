-- 2026-10-30: 配置变更审批（item15）
-- 2026-10-30: 配置变更审批（item15）
CREATE TABLE IF NOT EXISTS config_change_orders (
 id VARCHAR(80) PRIMARY KEY,
 config_key VARCHAR(40) NOT NULL,
 payload JSON NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'PENDING',
 created_by VARCHAR(80) NOT NULL,
 created_at VARCHAR(40) NOT NULL,
 decided_by VARCHAR(80) NOT NULL DEFAULT '',
 decided_at VARCHAR(40) NOT NULL DEFAULT '',
 note VARCHAR(500) NOT NULL DEFAULT '',
 INDEX idx_config_order_status(status,created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
