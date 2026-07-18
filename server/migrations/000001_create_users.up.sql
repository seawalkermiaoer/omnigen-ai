CREATE TABLE users (
  id            BIGSERIAL PRIMARY KEY,
  username      VARCHAR(64)  NOT NULL UNIQUE,
  password_hash VARCHAR(255) NOT NULL,
  display_name  VARCHAR(64)  NOT NULL DEFAULT '',
  role          VARCHAR(16)  NOT NULL DEFAULT 'user',
  status        VARCHAR(16)  NOT NULL DEFAULT 'active',
  created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
  -- updated_at 由应用层维护（每条 UPDATE 显式 SET updated_at = now()），
  -- 没有数据库触发器兜底。新增写入路径时务必记得。
  updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
  CONSTRAINT users_role_check   CHECK (role IN ('admin', 'user')),
  CONSTRAINT users_status_check CHECK (status IN ('active', 'disabled'))
);

-- 服务 CountActiveAdmins 的 WHERE role='admin' AND status='active'。
-- 列顺序 (role, status) 与该查询一致。
CREATE INDEX idx_users_role_status ON users (role, status);
