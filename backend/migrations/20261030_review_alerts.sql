-- 2026-10-30: 差评自动提醒（item12）
CREATE TABLE IF NOT EXISTS review_alerts (
 id VARCHAR(80) PRIMARY KEY,
 product_id VARCHAR(80) NOT NULL,
 order_id VARCHAR(80) NOT NULL,
 rating INT NOT NULL,
 content VARCHAR(500) NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'OPEN',
 created_at VARCHAR(40) NOT NULL,
 resolved_by VARCHAR(80) NOT NULL DEFAULT '',
 INDEX idx_review_alert_status(status,created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
