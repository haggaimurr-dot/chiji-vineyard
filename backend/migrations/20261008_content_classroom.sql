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

-- One-time conversion of the previous stage/variety series to editable courses.
-- Reruns keep existing courses and editorial changes intact.
SET SESSION group_concat_max_len = 1048576;
INSERT IGNORE INTO classroom_courses(id,status,sort_order,version,payload)
SELECT CONCAT('CR_LEGACY_',SHA2(CONCAT(stage,CHAR(0),variety),256)), 'PUBLISHED',0,1,
 JSON_OBJECT('id',CONCAT('CR_LEGACY_',SHA2(CONCAT(stage,CHAR(0),variety),256)),
 'title',CONCAT(stage,'系列课'),'summary','','stage',stage,'variety',variety,
 'status','PUBLISHED','sortOrder',0,'version',1,
 'articleIds',CAST(CONCAT('[',GROUP_CONCAT(JSON_QUOTE(id) ORDER BY published_at,id SEPARATOR ','),']') AS JSON))
FROM articles WHERE status='PUBLISHED' GROUP BY stage,variety;
