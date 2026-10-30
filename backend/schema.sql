CREATE TABLE IF NOT EXISTS users (
  openid VARCHAR(64) PRIMARY KEY,
  nickname VARCHAR(80) NOT NULL DEFAULT '葡萄好友',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  last_seen_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS addresses (
  openid VARCHAR(64) PRIMARY KEY,
  payload JSON NOT NULL,
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS products (
  id VARCHAR(80) PRIMARY KEY,
  category VARCHAR(20) NOT NULL,
  status VARCHAR(20) NOT NULL,
  sort_order INT NOT NULL DEFAULT 0,
  payload JSON NOT NULL,
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  INDEX idx_products_status_sort (status, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS inventory_alert_settings (
  sku_id VARCHAR(64) PRIMARY KEY,
  threshold INT NOT NULL DEFAULT 10,
  enabled BOOLEAN NOT NULL DEFAULT TRUE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS inventory_movements (
  id VARCHAR(64) PRIMARY KEY,
  product_id VARCHAR(64) NOT NULL,
  product_name VARCHAR(255) NOT NULL,
  sku_id VARCHAR(64) NOT NULL,
  sku_name VARCHAR(255) NOT NULL,
  kind VARCHAR(32) NOT NULL,
  reference VARCHAR(128) NOT NULL DEFAULT '',
  username VARCHAR(80) NOT NULL,
  reason VARCHAR(500) NOT NULL,
  before_stock INT NOT NULL,
  after_stock INT NOT NULL,
  before_reserved INT NOT NULL,
  after_reserved INT NOT NULL,
  before_sold INT NOT NULL,
  after_sold INT NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  INDEX idx_inventory_time (created_at,id),
  INDEX idx_inventory_sku (sku_id,created_at,id),
  INDEX idx_inventory_reference (reference,created_at,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS skus (
  id VARCHAR(80) PRIMARY KEY,
  product_id VARCHAR(80) NOT NULL,
  name VARCHAR(80) NOT NULL,
  price INT NOT NULL,
  compare_at_price INT NOT NULL DEFAULT 0,
  stock INT NOT NULL DEFAULT 0,
  reserved INT NOT NULL DEFAULT 0,
  sold INT NOT NULL DEFAULT 0,
  limit_per_order INT NOT NULL DEFAULT 1,
  weight_label VARCHAR(80) NOT NULL DEFAULT '',
  active BOOLEAN NOT NULL DEFAULT TRUE,
  sort_order INT NOT NULL DEFAULT 0,
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  INDEX idx_skus_product (product_id, active, sort_order),
  CONSTRAINT fk_skus_product FOREIGN KEY (product_id) REFERENCES products(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS carts (
  openid VARCHAR(64) NOT NULL,
  sku_id VARCHAR(80) NOT NULL,
  product_id VARCHAR(80) NOT NULL,
  quantity INT NOT NULL,
  selected BOOLEAN NOT NULL DEFAULT TRUE,
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (openid, sku_id),
  INDEX idx_carts_openid_updated (openid, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS orders (
  id VARCHAR(80) PRIMARY KEY,
  order_no VARCHAR(32) NOT NULL UNIQUE,
  openid VARCHAR(64) NOT NULL,
  order_status VARCHAR(24) NOT NULL,
  payment_status VARCHAR(24) NOT NULL,
  fulfillment_status VARCHAR(24) NOT NULL,
  refund_status VARCHAR(24) NOT NULL,
  total INT NOT NULL,
  payload JSON NOT NULL,
  payment_attempted BOOLEAN NOT NULL DEFAULT FALSE,
  transaction_id VARCHAR(80) NOT NULL DEFAULT '',
  expires_at DATETIME(3) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  INDEX idx_orders_user_created (openid, created_at),
  INDEX idx_orders_expire (order_status, payment_status, expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS refunds (
  id VARCHAR(80) PRIMARY KEY,
  order_id VARCHAR(80) NOT NULL,
  order_no VARCHAR(32) NOT NULL,
  openid VARCHAR(64) NOT NULL,
  amount INT NOT NULL,
  reason VARCHAR(100) NOT NULL,
  note VARCHAR(240) NOT NULL DEFAULT '',
  status VARCHAR(24) NOT NULL,
  stock_restored BOOLEAN NOT NULL DEFAULT FALSE,
  accepted_at DATETIME(3) NULL,
  provider_refund_id VARCHAR(80) NOT NULL DEFAULT '',
  reject_reason VARCHAR(200) NOT NULL DEFAULT '',
  reviewed_by VARCHAR(80) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  INDEX idx_refunds_order (order_id),
  INDEX idx_refunds_created (created_at),
  INDEX idx_refunds_accepted (accepted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS articles (
  id VARCHAR(80) PRIMARY KEY,
  status VARCHAR(20) NOT NULL,
  stage VARCHAR(50) NOT NULL,
  variety VARCHAR(50) NOT NULL,
  published_at DATETIME(3) NOT NULL,
  payload JSON NOT NULL,
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  INDEX idx_articles_filter (status, stage, variety, published_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS partner_leads (
  id VARCHAR(80) PRIMARY KEY,
  phone_hash CHAR(64) NOT NULL,
  status VARCHAR(24) NOT NULL,
  payload JSON NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  INDEX idx_leads_phone_created (phone_hash, created_at),
  INDEX idx_leads_status_created (status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS partner_lead_history (
  id VARCHAR(80) PRIMARY KEY,
  lead_id VARCHAR(80) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  payload JSON NOT NULL,
  INDEX idx_lead_history_created (lead_id, created_at),
  FOREIGN KEY (lead_id) REFERENCES partner_leads(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS admins (
  id VARCHAR(80) PRIMARY KEY,
  username VARCHAR(50) NOT NULL UNIQUE,
  password_hash VARCHAR(300) NOT NULL,
  role VARCHAR(24) NOT NULL,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  last_login_at DATETIME(3) NULL,
  password_changed_at DATETIME(3) NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS audit_logs (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  admin_id VARCHAR(80) NOT NULL,
  username VARCHAR(50) NOT NULL,
  action VARCHAR(80) NOT NULL,
  target_id VARCHAR(100) NOT NULL,
  detail JSON NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  INDEX idx_audit_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS configs (
  config_key VARCHAR(120) PRIMARY KEY,
  payload JSON NOT NULL,
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 配置版本：既有数据库也可重复执行此 CREATE；首次保存自动保留当前生效配置。
CREATE TABLE IF NOT EXISTS config_versions (
  id VARCHAR(80) PRIMARY KEY,
  config_key VARCHAR(50) NOT NULL,
  version INT NOT NULL,
  action VARCHAR(20) NOT NULL,
  source_version INT NULL,
  admin_id VARCHAR(80) NOT NULL,
  username VARCHAR(50) NOT NULL,
  payload JSON NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY idx_config_versions_key_version (config_key, version),
  INDEX idx_config_versions_created (created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS media (
  id VARCHAR(80) PRIMARY KEY,
  mime VARCHAR(40) NOT NULL,
  content LONGBLOB NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;


CREATE TABLE IF NOT EXISTS shipment_tracking (
 cache_key VARCHAR(100) PRIMARY KEY,
 payload JSON NOT NULL,
 checked_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS product_attention (
  openid VARCHAR(64) NOT NULL,
  product_id VARCHAR(80) NOT NULL,
  kind VARCHAR(16) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (openid, product_id, kind)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS product_reviews (
  id VARCHAR(80) PRIMARY KEY,
  order_id VARCHAR(80) NOT NULL,
  product_id VARCHAR(80) NOT NULL,
  openid VARCHAR(64) NOT NULL,
  rating INT NOT NULL,
  content VARCHAR(500) NOT NULL,
  created_at VARCHAR(40) NOT NULL,
  UNIQUE KEY idx_reviews_order_product (order_id, product_id),
  INDEX idx_reviews_product_created (product_id, created_at),
  FOREIGN KEY (order_id) REFERENCES orders(id),
  FOREIGN KEY (product_id) REFERENCES products(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Coupon campaigns start empty in production; configure them in the admin console.
INSERT IGNORE INTO configs(config_key,payload) VALUES('coupons','{"items":[]}');
CREATE TABLE IF NOT EXISTS invoice_requests (
 order_id VARCHAR(80) PRIMARY KEY,
 payload JSON NOT NULL,
 FOREIGN KEY (order_id) REFERENCES orders(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Private invoice PDFs; run on existing databases before enabling upload.
-- 2026-10-30: 开票抬头记忆（item17），按客户存档
-- 2026-10-30: 常见问题快捷回复（item18）
-- 2026-10-30: 直播预告推送（item 直播预告）
CREATE TABLE IF NOT EXISTS live_notice_subscriptions (
 openid VARCHAR(80) NOT NULL,
 channel VARCHAR(16) NOT NULL,
 template_id VARCHAR(200) NOT NULL DEFAULT '',
 notice_at VARCHAR(40) NOT NULL DEFAULT '',
 push_status VARCHAR(20) NOT NULL DEFAULT 'READY',
 push_error VARCHAR(240) NOT NULL DEFAULT '',
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 PRIMARY KEY (openid,channel),
 INDEX idx_live_notice_pending (push_status,notice_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS faqs (
 id VARCHAR(80) PRIMARY KEY,
 title VARCHAR(60) NOT NULL,
 content VARCHAR(2000) NOT NULL,
 sort INT NOT NULL DEFAULT 0,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 INDEX idx_faq_sort (sort,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

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

CREATE TABLE IF NOT EXISTS invoice_files (
 order_id VARCHAR(80) PRIMARY KEY,
 content MEDIUMBLOB NOT NULL,
 FOREIGN KEY (order_id) REFERENCES invoice_requests(order_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Private after-sales evidence; also run this CREATE on existing databases.
CREATE TABLE IF NOT EXISTS refund_evidence (
 id VARCHAR(80) PRIMARY KEY,
 order_id VARCHAR(80) NOT NULL,
 openid VARCHAR(64) NOT NULL,
 refund_id VARCHAR(80) NOT NULL DEFAULT '',
 mime VARCHAR(32) NOT NULL,
 content MEDIUMBLOB NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 INDEX idx_evidence_order (order_id,refund_id),
 INDEX idx_evidence_refund (refund_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 到货通知：每个有效提醒最多推送一次；取消后重新设置需重新授权。
CREATE TABLE IF NOT EXISTS restock_notifications (
  openid VARCHAR(80) NOT NULL,
  product_id VARCHAR(80) NOT NULL,
  template_id VARCHAR(200) NOT NULL,
  push_status VARCHAR(20) NOT NULL DEFAULT 'READY',
  push_error VARCHAR(240) NOT NULL DEFAULT '',
  clicked_at DATETIME(3) NULL,
  clicked_at DATETIME(3) NULL,
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (openid,product_id),
  INDEX idx_restock_pending (push_status,updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 降价提醒：按用户选择的同一规格保存关注时实售价；授权后最多推送一次。
CREATE TABLE IF NOT EXISTS price_drop_notifications (
 openid VARCHAR(64) NOT NULL,
 product_id VARCHAR(80) NOT NULL,
 sku_id VARCHAR(80) NOT NULL,
 baseline_price INT NOT NULL,
 target_price INT NOT NULL DEFAULT 0,
 template_id VARCHAR(200) NOT NULL DEFAULT '',
 push_status VARCHAR(20) NOT NULL DEFAULT 'NONE',
 push_error VARCHAR(240) NOT NULL DEFAULT '',
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 PRIMARY KEY (openid,product_id),
 INDEX idx_price_drop_pending (push_status,updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 发货通知：每个订单仅有一条站内通知；订阅消息发送状态独立于物流同步。
CREATE TABLE IF NOT EXISTS shipping_notifications (
  order_id VARCHAR(80) NOT NULL PRIMARY KEY,
  openid VARCHAR(80) NOT NULL,
  notified_at DATETIME(3) NULL,
  read_at DATETIME(3) NULL,
  template_id VARCHAR(200) NOT NULL DEFAULT '',
  push_status VARCHAR(20) NOT NULL DEFAULT 'NONE',
  push_error VARCHAR(240) NOT NULL DEFAULT '',
  INDEX idx_shipping_notices_user (openid, notified_at, read_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 为旧版已发货订单补齐站内通知，不补发微信消息、不覆盖已读状态。
INSERT IGNORE INTO shipping_notifications(order_id,openid,notified_at)
SELECT id,openid,created_at FROM orders
WHERE fulfillment_status IN ('SHIPPED','RECEIVED') AND JSON_EXTRACT(payload,'$.shipment.trackingNo') IS NOT NULL;

-- 内容版本、定时发布和阅读统计：旧数据库也需执行下面三个 CREATE，无需 ALTER articles。
CREATE TABLE IF NOT EXISTS article_versions (
  article_id VARCHAR(80) NOT NULL,
  version INT NOT NULL,
  payload JSON NOT NULL,
  created_by VARCHAR(80) NOT NULL,
  reason VARCHAR(80) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (article_id,version),
  FOREIGN KEY (article_id) REFERENCES articles(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS article_schedules (
  article_id VARCHAR(80) PRIMARY KEY,
  scheduled_at DATETIME(3) NOT NULL,
  INDEX idx_article_schedules_due (scheduled_at,article_id),
  FOREIGN KEY (article_id) REFERENCES articles(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS article_favorites (
  openid VARCHAR(80) NOT NULL,
  article_id VARCHAR(80) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (openid,article_id),
  INDEX idx_article_favorites_recent (openid,created_at,article_id),
  FOREIGN KEY (article_id) REFERENCES articles(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS article_reads (
  article_id VARCHAR(80) NOT NULL,
  reader_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  event_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  read_date DATE NOT NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (article_id,reader_hash,event_id),
  INDEX idx_article_reads_day (article_id,read_date,reader_hash),
  INDEX idx_article_reads_reader (reader_hash,article_id,created_at),
  FOREIGN KEY (article_id) REFERENCES articles(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 私有电子面单；旧环境执行以下两个 CREATE 后再部署新后端。
-- 每订单只有一个下单记录。CREATING/UNCERTAIN 不可自动重新取号。
CREATE TABLE IF NOT EXISTS order_waybills (
  id VARCHAR(80) PRIMARY KEY,
  order_id VARCHAR(80) NOT NULL,
  order_no VARCHAR(32) NOT NULL,
  carrier VARCHAR(40) NOT NULL,
  carrier_code VARCHAR(20) NOT NULL,
  tracking_no VARCHAR(50) NOT NULL DEFAULT '',
  status VARCHAR(20) NOT NULL,
  error VARCHAR(240) NOT NULL DEFAULT '',
  provider_order_id VARCHAR(100) NOT NULL DEFAULT '',
  provider_task_id VARCHAR(100) NOT NULL DEFAULT '',
  provider_label_url VARCHAR(1200) NOT NULL DEFAULT '',
  mime VARCHAR(32) NOT NULL DEFAULT '',
  content MEDIUMBLOB NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY idx_waybills_order (order_id),
  FOREIGN KEY (order_id) REFERENCES orders(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 手填发货与电子面单共用运单号预占，防止同承运商/单号绑定不同订单。
CREATE TABLE IF NOT EXISTS shipment_numbers (
  carrier_code VARCHAR(20) NOT NULL,
  tracking_no VARCHAR(50) NOT NULL,
  order_id VARCHAR(80) NOT NULL,
  PRIMARY KEY (carrier_code, tracking_no),
  INDEX idx_shipment_numbers_order (order_id),
  FOREIGN KEY (order_id) REFERENCES orders(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 商品评价晒图、追评及商家回复（已有数据库也需执行）
CREATE TABLE IF NOT EXISTS review_details (
 review_id VARCHAR(64) PRIMARY KEY,
 payload JSON NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 首页活动预约：每位微信用户对同一活动仅保留一份预约。
CREATE TABLE IF NOT EXISTS activity_reservations (
  openid VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  activity_id VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (openid, activity_id),
  INDEX idx_activity_reservations_activity (activity_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Durable purchase intent keys; deploy before the new order.create handler.
CREATE TABLE IF NOT EXISTS order_idempotency (
  openid VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  idempotency_key VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  request_hash CHAR(64) NOT NULL,
  order_id VARCHAR(80) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (openid, idempotency_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Run before deploying the identity/permissions update on existing databases.
CREATE TABLE IF NOT EXISTS login_limits (
 bucket_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 attempts INT UNSIGNED NOT NULL,
 expires_at DATETIME(3) NOT NULL,
 INDEX idx_login_limits_expiry (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS payment_reconciliations (
 bill_date DATE PRIMARY KEY,
 status VARCHAR(24) NOT NULL,
 payload JSON NOT NULL,
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS payment_alerts (
 id CHAR(64) PRIMARY KEY,
 kind VARCHAR(40) NOT NULL,
 target VARCHAR(80) NOT NULL,
 message VARCHAR(240) NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'OPEN',
 occurrences INT NOT NULL DEFAULT 1,
 last_seen DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 resolution_note VARCHAR(240) NOT NULL DEFAULT '',
 resolved_by VARCHAR(80) NOT NULL DEFAULT '',
 INDEX idx_payment_alert_status(status,last_seen)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 2026-10-30: 差评自动提醒（item12）
CREATE TABLE IF NOT EXISTS review_alerts (
 id VARCHAR(80) PRIMARY KEY,
 product_id VARCHAR(80) NOT NULL,
 order_id VARCHAR(80) NOT NULL,
 rating INT NOT NULL,
 content VARCHAR(500) NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'OPEN',
 created_at VARCHAR(40) NOT NULL,
 resolved_by VARCHAR(80) NOT NULL DEFAULT '',
 INDEX idx_review_alert_status(status,created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS admin_security (
 admin_id VARCHAR(80) PRIMARY KEY,
 payload JSON NOT NULL,
 FOREIGN KEY (admin_id) REFERENCES admins(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS product_batches (
 id VARCHAR(80) PRIMARY KEY,
 product_id VARCHAR(80) NOT NULL,
 batch_number VARCHAR(120) NOT NULL,
 status VARCHAR(20) NOT NULL,
 version INT NOT NULL DEFAULT 1,
 payload JSON NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 UNIQUE KEY uq_product_batch (product_id,batch_number),
 INDEX idx_batch_product (product_id,status,created_at),
 CONSTRAINT fk_batch_product FOREIGN KEY (product_id) REFERENCES products(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS after_sales_tickets (
 id VARCHAR(80) PRIMARY KEY,
 order_id VARCHAR(80) NOT NULL,
 openid VARCHAR(64) NOT NULL,
 status VARCHAR(20) NOT NULL,
 payload JSON NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 INDEX idx_ticket_order_status (order_id,status),
 INDEX idx_ticket_user_created (openid,created_at),
 INDEX idx_ticket_status_created (status,created_at),
 FOREIGN KEY (order_id) REFERENCES orders(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

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

CREATE TABLE IF NOT EXISTS classroom_progress (
 openid VARCHAR(64) NOT NULL,
 article_id VARCHAR(80) NOT NULL,
 completed BOOLEAN NOT NULL DEFAULT FALSE,
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 PRIMARY KEY (openid,article_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS classroom_questions (
 id VARCHAR(80) PRIMARY KEY,
 openid VARCHAR(64) NOT NULL,
 article_id VARCHAR(80) NOT NULL,
 event_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 question TEXT NOT NULL,
 answer TEXT NOT NULL,
 answered_by VARCHAR(80) NOT NULL DEFAULT '',
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 answered_at DATETIME(3) NULL,
 UNIQUE KEY uq_classroom_question_event (openid,event_id),
 INDEX idx_classroom_question_user (openid,created_at,id),
 INDEX idx_classroom_question_article (article_id,created_at,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS activity_notices (
 id VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 openid VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 activity_id VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 title VARCHAR(80) NOT NULL,
 start_date VARCHAR(10) NOT NULL,
 end_date VARCHAR(10) NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 is_read BOOLEAN NOT NULL DEFAULT FALSE,
 kind VARCHAR(20) NOT NULL DEFAULT 'RESERVED',
 INDEX idx_activity_notices_user (openid, created_at, id),
 INDEX idx_activity_notices_unread (openid, is_read)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Existing quantities stay unallocated; no historical movements are fabricated.
CREATE TABLE IF NOT EXISTS inventory_warehouses (
 id VARCHAR(64) PRIMARY KEY, name VARCHAR(80) NOT NULL UNIQUE,
 active BOOLEAN NOT NULL DEFAULT TRUE, version INT NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS inventory_lots (
 sku_id VARCHAR(80) NOT NULL, warehouse_id VARCHAR(64) NOT NULL, batch_id VARCHAR(80) NOT NULL,
 quantity INT NOT NULL DEFAULT 0, version INT NOT NULL DEFAULT 0,
 PRIMARY KEY(sku_id,warehouse_id,batch_id),
 FOREIGN KEY(sku_id) REFERENCES skus(id),
 FOREIGN KEY(warehouse_id) REFERENCES inventory_warehouses(id),
 FOREIGN KEY(batch_id) REFERENCES product_batches(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS inventory_lot_movements (
 id VARCHAR(64) PRIMARY KEY, sku_id VARCHAR(80) NOT NULL, warehouse_id VARCHAR(64) NOT NULL,
 batch_id VARCHAR(80) NOT NULL, kind VARCHAR(32) NOT NULL, reference VARCHAR(128) NOT NULL,
 username VARCHAR(80) NOT NULL, reason VARCHAR(500) NOT NULL,
 before_quantity INT NOT NULL, after_quantity INT NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 INDEX idx_lot_time(created_at,id), INDEX idx_lot_sku(sku_id,created_at,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS activity_waitlist (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 openid VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 activity_id VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 UNIQUE KEY uq_activity_waitlist_user (openid, activity_id),
 INDEX idx_activity_waitlist_queue (activity_id, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
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

CREATE TABLE IF NOT EXISTS game_farms (
 owner VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
 state JSON NOT NULL,
 version BIGINT UNSIGNED NOT NULL DEFAULT 0,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS game_commands (
 owner VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 request_id VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 request_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 result JSON NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 PRIMARY KEY (owner, request_id),
 CONSTRAINT fk_game_commands_farm FOREIGN KEY (owner) REFERENCES game_farms(owner) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS payment_finance_runs (
 bill_date DATE NOT NULL,
 kind VARCHAR(16) NOT NULL,
 status VARCHAR(24) NOT NULL,
 payload JSON NOT NULL,
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 PRIMARY KEY (bill_date,kind)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS user_coupons (
 openid VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 code VARCHAR(40) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 payload JSON NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 PRIMARY KEY (openid, code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS classroom_courses (
 id VARCHAR(80) PRIMARY KEY,
 status VARCHAR(20) NOT NULL,
 sort_order INT NOT NULL DEFAULT 0,
 version INT NOT NULL DEFAULT 1,
 payload JSON NOT NULL,
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 INDEX idx_courses_status (status,sort_order,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS classroom_video_progress (
 openid VARCHAR(64) NOT NULL,
 article_id VARCHAR(80) NOT NULL,
 source TEXT NOT NULL,
 position DOUBLE NOT NULL DEFAULT 0,
 duration DOUBLE NOT NULL DEFAULT 0,
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 PRIMARY KEY (openid,article_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;


-- Durable CRM external notification claims; SENDING/UNCERTAIN require gateway verification.
CREATE TABLE IF NOT EXISTS partner_lead_deliveries (
  id CHAR(64) PRIMARY KEY,
  lead_id VARCHAR(80) NOT NULL,
  status VARCHAR(24) NOT NULL,
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  INDEX idx_lead_deliveries (lead_id, updated_at),
  FOREIGN KEY (lead_id) REFERENCES partner_leads(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Unique manual document lines protect against a retry using a refreshed version.
CREATE TABLE IF NOT EXISTS inventory_lot_documents (
 sku_id VARCHAR(80) NOT NULL, warehouse_id VARCHAR(64) NOT NULL,
 batch_id VARCHAR(80) NOT NULL, kind VARCHAR(32) NOT NULL, reference VARCHAR(128) NOT NULL,
 PRIMARY KEY(sku_id,warehouse_id,batch_id,kind,reference)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS waybill_history (
 id VARCHAR(80) PRIMARY KEY, order_id VARCHAR(80) NOT NULL, payload JSON NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 INDEX idx_waybill_history_order(order_id,created_at), FOREIGN KEY(order_id) REFERENCES orders(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS logistics_tasks (
 id VARCHAR(80) PRIMARY KEY, order_id VARCHAR(80) NOT NULL,
 carrier_code VARCHAR(20) NOT NULL, tracking_no VARCHAR(50) NOT NULL, kind VARCHAR(20) NOT NULL,
 status VARCHAR(20) NOT NULL, payload JSON NOT NULL,
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 INDEX idx_logistics_status(status,updated_at), INDEX idx_logistics_order(order_id),
 FOREIGN KEY(order_id) REFERENCES orders(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS account_closures (
 subject_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 closed_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- UTC session required, matching MYSQL_DSN. Re-running preserves coverage start.
CREATE TABLE IF NOT EXISTS commerce_activity (
  activity_date DATE NOT NULL,
  openid VARCHAR(80) NOT NULL,
  visited_at DATETIME(3) NOT NULL,
  cart_added_at DATETIME(3) NULL,
  PRIMARY KEY (activity_date, openid),
  KEY idx_commerce_visit (visited_at, openid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS commerce_analytics_state (
  id TINYINT NOT NULL PRIMARY KEY,
  started_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
INSERT IGNORE INTO commerce_analytics_state(id,started_at) VALUES(1,UTC_TIMESTAMP(3));

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
-- 2026-10-30: 配置变更审批（item15）
CREATE TABLE IF NOT EXISTS config_change_orders (
 id VARCHAR(80) PRIMARY KEY,
 config_key VARCHAR(40) NOT NULL,
 payload JSON NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'PENDING',
 created_by VARCHAR(80) NOT NULL,
 created_at VARCHAR(40) NOT NULL,
 decided_by VARCHAR(80) NOT NULL DEFAULT '',
 decided_at VARCHAR(40) NOT NULL DEFAULT '',
 note VARCHAR(500) NOT NULL DEFAULT '',
 INDEX idx_config_order_status(status,created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
