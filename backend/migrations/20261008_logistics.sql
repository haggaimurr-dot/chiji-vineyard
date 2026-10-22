-- Run once on existing databases before deploying.
ALTER TABLE order_waybills ADD COLUMN provider_order_id VARCHAR(100) NOT NULL DEFAULT '';
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
