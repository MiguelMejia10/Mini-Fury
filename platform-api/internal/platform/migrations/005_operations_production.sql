CREATE TABLE password_resets (
 token_hash TEXT PRIMARY KEY, user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 credential_version BIGINT NOT NULL, expires_at TIMESTAMPTZ NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX password_resets_user_idx ON password_resets(user_id);
CREATE INDEX password_resets_expiry_idx ON password_resets(expires_at);
CREATE TABLE delivery_jobs (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), dedupe_key TEXT NOT NULL UNIQUE,
 kind TEXT NOT NULL CHECK(kind IN ('email','webhook')), payload BYTEA NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','processing','sent','failed')),
 attempts INTEGER NOT NULL DEFAULT 0, available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 locked_until TIMESTAMPTZ, expires_at TIMESTAMPTZ NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 completed_at TIMESTAMPTZ, last_error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX delivery_jobs_pending_idx ON delivery_jobs(available_at,created_at) WHERE state IN ('pending','processing');
CREATE TABLE alert_transitions (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), alert_id UUID NOT NULL REFERENCES platform_alerts(id) ON DELETE CASCADE,
 active BOOLEAN NOT NULL, occurrences INTEGER NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX alert_transitions_alert_idx ON alert_transitions(alert_id,created_at DESC);
CREATE FUNCTION record_alert_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' OR OLD.active IS DISTINCT FROM NEW.active THEN
  INSERT INTO alert_transitions(alert_id,active,occurrences) VALUES(NEW.id,NEW.active,NEW.occurrences);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER platform_alert_transition AFTER INSERT OR UPDATE OF active ON platform_alerts FOR EACH ROW EXECUTE FUNCTION record_alert_transition();
