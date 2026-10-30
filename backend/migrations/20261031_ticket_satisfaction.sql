-- 客服满意度评价
CREATE TABLE IF NOT EXISTS ticket_satisfaction (
 ticket_id VARCHAR(80) PRIMARY KEY,
 order_id VARCHAR(80) NOT NULL,
 openid VARCHAR(64) NOT NULL,
 rating TINYINT NOT NULL,
 comment VARCHAR(200) NOT NULL DEFAULT '',
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 INDEX idx_satis_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
