-- 2026-10-30: 开票抬头记忆（item17）
CREATE TABLE IF NOT EXISTS invoice_titles (
 openid VARCHAR(80) NOT NULL,
 kind VARCHAR(16) NOT NULL DEFAULT 'personal',
 title VARCHAR(100) NOT NULL,
 tax_id VARCHAR(20) NOT NULL DEFAULT '',
 email VARCHAR(200) NOT NULL DEFAULT '',
 last_used_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 PRIMARY KEY (openid,kind,title),
 INDEX idx_invoice_title_used (openid,last_used_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
