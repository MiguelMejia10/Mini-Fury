CREATE TABLE applications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(63) NOT NULL CHECK (name ~ '^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$'),
    repository TEXT NOT NULL,
    port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    environment TEXT NOT NULL CHECK (environment IN ('production', 'staging')),
    owner VARCHAR(120) NOT NULL,
    runtime VARCHAR(80) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT applications_name_environment_key UNIQUE (name, environment)
);
CREATE INDEX applications_created_at_idx ON applications (created_at DESC, id);
