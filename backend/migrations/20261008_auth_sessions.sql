CREATE TABLE IF NOT EXISTS auth_sessions (
 id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 subject VARCHAR(80) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
 kind VARCHAR(10) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 credential VARCHAR(64) NOT NULL DEFAULT '',
 user_agent VARCHAR(300) NOT NULL DEFAULT '',
 ip_address VARCHAR(64) NOT NULL DEFAULT '',
 created_at BIGINT NOT NULL,
 expires_at BIGINT NOT NULL,
 revoked_at BIGINT NULL,
 INDEX idx_auth_sessions_owner (subject,kind,created_at,id),
 INDEX idx_auth_sessions_expiry (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
