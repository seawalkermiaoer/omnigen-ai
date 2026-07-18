CREATE TABLE app_settings (
  key         VARCHAR(64) PRIMARY KEY,
  value       TEXT NOT NULL,
  is_secret   BOOLEAN NOT NULL DEFAULT false,
  updated_by  BIGINT REFERENCES users(id),
  -- updated_at 由应用层维护（每条 UPDATE/UPSERT 显式 SET updated_at = now()），
  -- 没有数据库触发器兜底。新增写入路径时务必记得。
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
