-- 2026-10-30: auth.permissions.<管理员ID> 键长可达 51 字符，超出原 VARCHAR(50)，导致运营人员权限保存失败。
ALTER TABLE configs MODIFY COLUMN config_key VARCHAR(120) NOT NULL;
