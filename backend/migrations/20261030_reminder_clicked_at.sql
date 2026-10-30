-- 2026-10-30: 提醒点击回流统计（item16）
ALTER TABLE restock_notifications ADD COLUMN clicked_at DATETIME(3) NULL;
ALTER TABLE price_drop_notifications ADD COLUMN clicked_at DATETIME(3) NULL;
