CREATE TABLE users (
  id            BIGSERIAL PRIMARY KEY,
  username      VARCHAR(64)  NOT NULL UNIQUE,
  password_hash VARCHAR(255) NOT NULL,
  display_name  VARCHAR(64)  NOT NULL DEFAULT '',
  role          VARCHAR(16)  NOT NULL DEFAULT 'user',
  status        VARCHAR(16)  NOT NULL DEFAULT 'active',
  created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
  CONSTRAINT users_role_check   CHECK (role IN ('admin', 'user')),
  CONSTRAINT users_status_check CHECK (status IN ('active', 'disabled'))
);

CREATE INDEX idx_users_status ON users (status);
