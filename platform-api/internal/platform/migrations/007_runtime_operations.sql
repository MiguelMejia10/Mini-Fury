CREATE TABLE runtime_versions (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),application_id UUID NOT NULL REFERENCES applications(id),
 name VARCHAR(80) NOT NULL,source_revision VARCHAR(64) NOT NULL,image TEXT NOT NULL,
 provenance TEXT NOT NULL CHECK(provenance IN ('local','github','gitlab','import')),
 build_id UUID,created_by UUID NOT NULL REFERENCES users(id),created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(application_id,name),UNIQUE(application_id,image)
);
CREATE INDEX runtime_versions_app_idx ON runtime_versions(application_id,created_at DESC,id);
CREATE TABLE runtime_jobs (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),application_id UUID NOT NULL REFERENCES applications(id),scope_id UUID REFERENCES runtime_scopes(id),
 kind TEXT NOT NULL CHECK(kind IN ('build','deploy','rollback','restart','scale','configure')),
 state TEXT NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','running','succeeded','failed','cancelled')),
 phase TEXT NOT NULL DEFAULT 'queued',message TEXT NOT NULL DEFAULT '',payload JSONB NOT NULL,
 idempotency_key VARCHAR(80) NOT NULL,requested_by UUID NOT NULL REFERENCES users(id),parent_id UUID REFERENCES runtime_jobs(id),
 attempt INTEGER NOT NULL DEFAULT 0,owner_token TEXT NOT NULL DEFAULT '',lease_until TIMESTAMPTZ,
 cancel_requested BOOLEAN NOT NULL DEFAULT false,provider_run_id TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),started_at TIMESTAMPTZ,completed_at TIMESTAMPTZ,
 UNIQUE(application_id,idempotency_key)
);
CREATE UNIQUE INDEX runtime_one_scope_job_idx ON runtime_jobs(scope_id) WHERE state='running';
CREATE UNIQUE INDEX runtime_one_build_idx ON runtime_jobs(application_id) WHERE kind='build' AND state='running';
CREATE INDEX runtime_jobs_queue_idx ON runtime_jobs(created_at,id) WHERE state IN ('queued','running');
CREATE INDEX runtime_jobs_app_idx ON runtime_jobs(application_id,created_at DESC,id);
ALTER TABLE runtime_versions ADD CONSTRAINT runtime_version_build_fk FOREIGN KEY(build_id) REFERENCES runtime_jobs(id);
CREATE TABLE runtime_job_events(id BIGSERIAL PRIMARY KEY,job_id UUID NOT NULL REFERENCES runtime_jobs(id) ON DELETE CASCADE,state TEXT NOT NULL,phase TEXT NOT NULL,message TEXT NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT now());
CREATE INDEX runtime_job_events_job_idx ON runtime_job_events(job_id,id DESC);
