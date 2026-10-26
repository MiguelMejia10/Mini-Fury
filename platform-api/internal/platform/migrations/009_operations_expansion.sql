-- Points 6–10: operational records are scoped to the application's existing ACL.
CREATE TABLE operational_incidents (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),application_id UUID NOT NULL REFERENCES applications(id),scope_id UUID REFERENCES runtime_scopes(id),
 title VARCHAR(160) NOT NULL,severity TEXT NOT NULL CHECK(severity IN ('critical','warning','info')),
 state TEXT NOT NULL DEFAULT 'open' CHECK(state IN ('open','acknowledged','resolved')),
 assignee_id UUID REFERENCES users(id),runbook TEXT NOT NULL DEFAULT '',source_key TEXT,
 version BIGINT NOT NULL DEFAULT 1,muted_until TIMESTAMPTZ,escalate_at TIMESTAMPTZ,escalated_at TIMESTAMPTZ,
 notification_state TEXT NOT NULL DEFAULT 'not_configured',created_by UUID NOT NULL REFERENCES users(id),created_at TIMESTAMPTZ NOT NULL DEFAULT now(),updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),resolved_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX incidents_active_source_idx ON operational_incidents(application_id,source_key) WHERE state<>'resolved' AND source_key IS NOT NULL;
CREATE INDEX incidents_app_idx ON operational_incidents(application_id,updated_at DESC,id);
CREATE INDEX incidents_escalation_idx ON operational_incidents(escalate_at) WHERE state<>'resolved' AND escalated_at IS NULL;
CREATE TABLE incident_events(id BIGSERIAL PRIMARY KEY,incident_id UUID NOT NULL REFERENCES operational_incidents(id) ON DELETE CASCADE,actor_id UUID REFERENCES users(id),kind TEXT NOT NULL,message VARCHAR(2000) NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT now());
CREATE INDEX incident_events_history_idx ON incident_events(incident_id,id DESC);
CREATE TABLE scope_slos (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),scope_id UUID NOT NULL REFERENCES runtime_scopes(id),name VARCHAR(80) NOT NULL,
 indicator TEXT NOT NULL CHECK(indicator IN ('availability','latency')),target_ppm INTEGER NOT NULL CHECK(target_ppm BETWEEN 900000 AND 999990),
 latency_ms INTEGER NOT NULL CHECK(latency_ms IN (100,300,1000)),window_days INTEGER NOT NULL CHECK(window_days IN (1,7,28)),
 version BIGINT NOT NULL DEFAULT 1,enabled BOOLEAN NOT NULL DEFAULT true,created_by UUID NOT NULL REFERENCES users(id),created_at TIMESTAMPTZ NOT NULL DEFAULT now(),UNIQUE(scope_id,name)
);
CREATE TABLE slo_counter_state(slo_id UUID REFERENCES scope_slos(id) ON DELETE CASCADE,pod_uid TEXT NOT NULL,epoch TEXT NOT NULL,total BIGINT NOT NULL,good BIGINT NOT NULL,sampled_at TIMESTAMPTZ NOT NULL,PRIMARY KEY(slo_id,pod_uid));
CREATE TABLE slo_measurements(id BIGSERIAL PRIMARY KEY,slo_id UUID NOT NULL REFERENCES scope_slos(id) ON DELETE CASCADE,total BIGINT NOT NULL CHECK(total>=0),good BIGINT NOT NULL CHECK(good>=0 AND good<=total),observed_pods INTEGER NOT NULL,expected_pods INTEGER NOT NULL,complete BOOLEAN NOT NULL,measured_at TIMESTAMPTZ NOT NULL DEFAULT now());
CREATE INDEX slo_measurements_window_idx ON slo_measurements(slo_id,measured_at DESC);
CREATE TABLE application_scaffolds(id UUID PRIMARY KEY DEFAULT gen_random_uuid(),application_id UUID NOT NULL REFERENCES applications(id),template_id TEXT NOT NULL,template_version TEXT NOT NULL,created_by UUID NOT NULL REFERENCES users(id),created_at TIMESTAMPTZ NOT NULL DEFAULT now(),UNIQUE(application_id));
CREATE TABLE managed_services (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),scope_id UUID NOT NULL REFERENCES runtime_scopes(id),name VARCHAR(40) NOT NULL,profile TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'requested' CHECK(state IN ('requested','ready','failed','suspended')),
 storage_gib INTEGER NOT NULL CHECK(storage_gib BETWEEN 1 AND 10),backup_policy TEXT NOT NULL CHECK(backup_policy IN ('development-only','external-required')),
 version BIGINT NOT NULL DEFAULT 1,created_by UUID NOT NULL REFERENCES users(id),created_at TIMESTAMPTZ NOT NULL DEFAULT now(),updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),UNIQUE(scope_id,name)
);
CREATE INDEX managed_services_scope_idx ON managed_services(scope_id,id);
ALTER TABLE runtime_jobs DROP CONSTRAINT runtime_jobs_kind_check;
ALTER TABLE runtime_jobs ADD CONSTRAINT runtime_jobs_kind_check CHECK(kind IN ('build','deploy','rollback','restart','scale','configure','service-provision','service-suspend','service-resume'));
CREATE TABLE application_cost_policies (
 application_id UUID PRIMARY KEY REFERENCES applications(id),currency TEXT NOT NULL DEFAULT 'USD' CHECK(currency='USD'),
 cpu_core_hour NUMERIC(12,6) NOT NULL CHECK(cpu_core_hour BETWEEN 0 AND 100),memory_gib_hour NUMERIC(12,6) NOT NULL CHECK(memory_gib_hour BETWEEN 0 AND 100),storage_gib_month NUMERIC(12,6) NOT NULL CHECK(storage_gib_month BETWEEN 0 AND 100),
 monthly_budget NUMERIC(12,2) NOT NULL CHECK(monthly_budget BETWEEN 1 AND 1000000),version BIGINT NOT NULL DEFAULT 1,updated_by UUID NOT NULL REFERENCES users(id),updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
