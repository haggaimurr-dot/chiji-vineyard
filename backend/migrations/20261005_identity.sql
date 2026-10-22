-- Run before deploying the identity/permissions update on existing databases.
CREATE TABLE IF NOT EXISTS login_limits (
 bucket_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 attempts INT UNSIGNED NOT NULL,
 expires_at DATETIME(3) NOT NULL,
 INDEX idx_login_limits_expiry (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
