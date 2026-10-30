-- 活动签到核销
CREATE TABLE IF NOT EXISTS activity_checkins (
 activity_id VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 openid VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 code VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'PENDING',
 checked_in_at DATETIME(3) NULL,
 checked_by VARCHAR(80) NOT NULL DEFAULT '',
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 PRIMARY KEY (activity_id,openid),
 UNIQUE KEY uq_activity_checkin_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
