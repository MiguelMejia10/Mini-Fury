CREATE TABLE users (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 email VARCHAR(254) NOT NULL UNIQUE,
 name VARCHAR(120) NOT NULL,
 password_hash TEXT NOT NULL,
 role TEXT NOT NULL CHECK(role IN ('admin','developer')),
 active BOOLEAN NOT NULL DEFAULT true,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE sessions (
 token_hash TEXT PRIMARY KEY,
 user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sessions_expiry_idx ON sessions(expires_at);
CREATE INDEX sessions_user_idx ON sessions(user_id);
CREATE TABLE auth_attempts (
 key TEXT PRIMARY KEY,
 window_start TIMESTAMPTZ NOT NULL,
 attempts INTEGER NOT NULL
);
CREATE INDEX auth_attempts_window_idx ON auth_attempts(window_start);
CREATE TABLE audit_events (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
 actor_email VARCHAR(254) NOT NULL,
 action TEXT NOT NULL,
 resource_id TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_created_idx ON audit_events(created_at DESC,id DESC);
ALTER TABLE applications ADD COLUMN created_by UUID REFERENCES users(id);
CREATE INDEX applications_creator_idx ON applications(created_by,created_at DESC,id);
