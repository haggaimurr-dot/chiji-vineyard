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

-- Unique manual document lines protect against a retry using a refreshed version.
CREATE TABLE IF NOT EXISTS inventory_lot_documents (
 sku_id VARCHAR(80) NOT NULL, warehouse_id VARCHAR(64) NOT NULL,
 batch_id VARCHAR(80) NOT NULL, kind VARCHAR(32) NOT NULL, reference VARCHAR(128) NOT NULL,
 PRIMARY KEY(sku_id,warehouse_id,batch_id,kind,reference)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
