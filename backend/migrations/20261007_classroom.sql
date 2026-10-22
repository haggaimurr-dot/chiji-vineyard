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
