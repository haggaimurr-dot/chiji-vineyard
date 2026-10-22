CREATE TABLE IF NOT EXISTS payment_finance_runs (
 bill_date DATE NOT NULL,
 kind VARCHAR(16) NOT NULL,
 status VARCHAR(24) NOT NULL,
 payload JSON NOT NULL,
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 PRIMARY KEY (bill_date,kind)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE refunds ADD COLUMN accepted_at DATETIME(3) NULL;
CREATE INDEX idx_refunds_accepted ON refunds(accepted_at);
