CREATE TABLE runtime_scopes (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), application_id UUID NOT NULL REFERENCES applications(id),
 name VARCHAR(40) NOT NULL CHECK(name ~ '^[a-z][a-z0-9-]{0,39}$'),
 environment TEXT NOT NULL CHECK(environment IN ('staging','production')),
 criticality TEXT NOT NULL CHECK(criticality IN ('low','medium','high')),
 region VARCHAR(80) NOT NULL DEFAULT '', domain VARCHAR(253) NOT NULL DEFAULT '',
 cluster_id VARCHAR(64) NOT NULL, namespace VARCHAR(63) NOT NULL, workload VARCHAR(63) NOT NULL UNIQUE,
 replicas INTEGER NOT NULL CHECK(replicas BETWEEN 1 AND 10), cpu_milli INTEGER NOT NULL CHECK(cpu_milli BETWEEN 50 AND 2000),
 memory_mib INTEGER NOT NULL CHECK(memory_mib BETWEEN 64 AND 2048), version BIGINT NOT NULL DEFAULT 1,
 created_by UUID NOT NULL REFERENCES users(id),created_at TIMESTAMPTZ NOT NULL DEFAULT now(),updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(application_id,name)
);
CREATE INDEX runtime_scopes_app_idx ON runtime_scopes(application_id,created_at DESC,id);
