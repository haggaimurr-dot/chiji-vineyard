-- 2026-10-30: 直播预告推送
CREATE TABLE IF NOT EXISTS live_notice_subscriptions (
 openid VARCHAR(80) NOT NULL,
 channel VARCHAR(16) NOT NULL,
 template_id VARCHAR(200) NOT NULL DEFAULT '',
 notice_at VARCHAR(40) NOT NULL DEFAULT '',
 push_status VARCHAR(20) NOT NULL DEFAULT 'READY',
 push_error VARCHAR(240) NOT NULL DEFAULT '',
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 PRIMARY KEY (openid,channel),
 INDEX idx_live_notice_pending (push_status,notice_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
