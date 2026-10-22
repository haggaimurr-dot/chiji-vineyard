
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
