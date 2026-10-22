-- 已有数据库执行一次；新库使用 schema.sql。
ALTER TABLE price_drop_notifications ADD COLUMN target_price INT NOT NULL DEFAULT 0 AFTER baseline_price;

-- 提醒发送历史：保留商品/规格快照，不随取消关注或商品下架删除。
CREATE TABLE IF NOT EXISTS product_reminder_history (
 id VARCHAR(80) NOT NULL PRIMARY KEY,
 openid VARCHAR(80) NOT NULL,
 product_id VARCHAR(80) NOT NULL,
 product_name VARCHAR(200) NOT NULL,
 kind VARCHAR(20) NOT NULL,
 sku_name VARCHAR(200) NOT NULL DEFAULT '',
 current_price INT NOT NULL DEFAULT 0,
 target_price INT NOT NULL DEFAULT 0,
 status VARCHAR(20) NOT NULL,
 detail VARCHAR(240) NOT NULL DEFAULT '',
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 INDEX idx_reminder_history_user (openid,created_at,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
