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
