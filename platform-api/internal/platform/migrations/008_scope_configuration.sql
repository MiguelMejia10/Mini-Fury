CREATE TABLE runtime_configurations (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),scope_id UUID NOT NULL REFERENCES runtime_scopes(id),
 revision BIGINT NOT NULL,variables JSONB NOT NULL DEFAULT '{}',secret_refs JSONB NOT NULL DEFAULT '{}',
 created_by UUID NOT NULL REFERENCES users(id),created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(scope_id,revision)
);
CREATE INDEX runtime_configurations_scope_idx ON runtime_configurations(scope_id,revision DESC);
