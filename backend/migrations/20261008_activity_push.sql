CREATE TABLE IF NOT EXISTS activity_push_subscriptions (
 openid VARCHAR(80) NOT NULL,
 activity_id VARCHAR(80) NOT NULL,
 template_id VARCHAR(100) NOT NULL,
 reservation_created_at DATETIME(3) NOT NULL,
 push_status VARCHAR(20) NOT NULL DEFAULT 'READY',
 push_error VARCHAR(500) NOT NULL DEFAULT '',
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 PRIMARY KEY (openid,activity_id),
 INDEX idx_activity_push_queue (activity_id,template_id,push_status,updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
