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
