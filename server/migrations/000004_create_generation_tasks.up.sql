CREATE TABLE generation_tasks (
  id                BIGSERIAL PRIMARY KEY,
  user_id           BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  mode              VARCHAR(16) NOT NULL,   -- imggen|imgedit|t2v|i2v|r2v
  model             VARCHAR(64) NOT NULL,
  status            VARCHAR(16) NOT NULL,   -- PENDING|RUNNING|SUCCEEDED|FAILED|CANCELED
  upstream_task_id  VARCHAR(128),           -- 视频异步任务；图片同步则为空
  prompt            TEXT NOT NULL DEFAULT '',
  params            JSONB NOT NULL DEFAULT '{}',
  input_urls        JSONB NOT NULL DEFAULT '[]',
  result_urls       JSONB NOT NULL DEFAULT '[]',
  usage             JSONB,
  note              TEXT,                  -- t8star 的模型散文
  error_code        VARCHAR(64),
  error_message     TEXT,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  -- updated_at 由应用层维护（每条 UPDATE 显式 SET updated_at = now()），
  -- 没有数据库触发器兜底。新增写入路径时务必记得。
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tasks_user_created ON generation_tasks (user_id, created_at DESC);
CREATE INDEX idx_tasks_polling ON generation_tasks (status) WHERE status IN ('PENDING', 'RUNNING');
