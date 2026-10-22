-- Run after 20261007_activity_notices.sql. Existing reservations remain confirmed.
-- MySQL 5.7/8 compatible and rerunnable; do not drop user history on rollback.
CREATE TABLE IF NOT EXISTS activity_waitlist (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 openid VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 activity_id VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 UNIQUE KEY uq_activity_waitlist_user (openid, activity_id),
 INDEX idx_activity_waitlist_queue (activity_id, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
SET @has_kind = (SELECT COUNT(*) FROM information_schema.columns
 WHERE table_schema=DATABASE() AND table_name='activity_notices' AND column_name='kind');
SET @kind_sql = IF(@has_kind=0, "ALTER TABLE activity_notices ADD COLUMN kind VARCHAR(20) NOT NULL DEFAULT 'RESERVED'", 'SELECT 1');
PREPARE activity_migration FROM @kind_sql;
EXECUTE activity_migration;
DEALLOCATE PREPARE activity_migration;
