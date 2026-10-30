-- 异常订单提醒
CREATE TABLE IF NOT EXISTS order_alerts (
 id VARCHAR(80) PRIMARY KEY,
 order_id VARCHAR(80) NOT NULL,
 kind VARCHAR(40) NOT NULL,
 detail VARCHAR(500) NOT NULL DEFAULT '',
 status VARCHAR(16) NOT NULL DEFAULT 'OPEN',
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 resolved_at DATETIME(3) NULL,
 resolved_by VARCHAR(80) NOT NULL DEFAULT '',
 INDEX idx_order_alert_status(status,created_at),
 UNIQUE KEY uq_order_alert_order_kind (order_id,kind)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
