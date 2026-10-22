CREATE TABLE IF NOT EXISTS activity_notices (
 id VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 openid VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 activity_id VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 title VARCHAR(80) NOT NULL,
 start_date VARCHAR(10) NOT NULL,
 end_date VARCHAR(10) NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 is_read BOOLEAN NOT NULL DEFAULT FALSE,
 INDEX idx_activity_notices_user (openid, created_at, id),
 INDEX idx_activity_notices_unread (openid, is_read)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
