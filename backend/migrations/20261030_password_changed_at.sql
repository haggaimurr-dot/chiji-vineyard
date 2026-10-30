-- 密码过期策略：记录密码修改时间；既有账号回填为当前时间（从此刻起算有效期）。
ALTER TABLE admins ADD COLUMN password_changed_at DATETIME(3) NULL;
UPDATE admins SET password_changed_at = NOW(3) WHERE password_changed_at IS NULL;
