#!/bin/sh
set -eu
umask 077
: "${RESTIC_REPOSITORY:?Set an authorized restic destination}"
: "${RESTIC_PASSWORD:?Set an independent encryption password}"
: "${PGPASSWORD:?Set the database password}"
export PGHOST="${PGHOST:-postgres}" PGUSER="${PGUSER:-mini_fury}" PGDATABASE="${PGDATABASE:-mini_fury}"
work=$(mktemp -d)
verify_db=
cleanup() { if [ -n "$verify_db" ]; then dropdb --if-exists "$verify_db" >/dev/null; fi; rm -rf "$work"; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
backup() {
 pg_dump -Fc --no-owner --no-acl > "$work/database.dump" || return 1
 restic backup --stdin --stdin-filename database.dump --host mini-fury --tag mini-fury-database --json < "$work/database.dump" > "$work/result.json" || return 1
 # Only prune snapshots belonging to this application's fixed host/tag.
 restic forget --host mini-fury --tag mini-fury-database --keep-last 7 --keep-daily 14 --keep-weekly 8 --keep-monthly 6 --prune >/dev/null || return 1
 restic check >/dev/null || return 1
 printf 'Encrypted backup and repository verification completed.\n'
}
case "${1:-backup}" in
 init) restic init ;;
 backup) backup ;;
 verify)
  restic dump --host mini-fury --tag mini-fury-database latest /database.dump > "$work/restored.dump"
  candidate_db="mini_fury_restore_$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')"
  createdb "$candidate_db"
  verify_db="$candidate_db"
  pg_restore -d "$verify_db" --single-transaction --exit-on-error --no-owner --no-acl "$work/restored.dump"
  psql -X -v ON_ERROR_STOP=1 -d "$verify_db" -c 'SELECT count(*) AS migrations FROM schema_migrations; SELECT count(*) AS applications FROM applications; DELETE FROM sessions; DELETE FROM auth_attempts; DELETE FROM password_resets; DELETE FROM delivery_jobs;'
  psql -X -v ON_ERROR_STOP=1 -d "$verify_db" <<'SQL'
DO $$ BEGIN
  IF to_regclass('public.runtime_jobs') IS NOT NULL THEN
    UPDATE runtime_jobs SET state='cancelled',phase='restore_cancelled',message='Operación detenida al restaurar el respaldo',cancel_requested=true,owner_token='',lease_until=NULL,completed_at=now() WHERE state IN ('queued','running');
    UPDATE runtime_jobs SET owner_token='',lease_until=NULL WHERE owner_token<>'' OR lease_until IS NOT NULL;
  END IF;
  IF to_regclass('public.scope_slos') IS NOT NULL THEN
    DELETE FROM slo_counter_state;
    UPDATE scope_slos SET enabled=false,version=version+1;
    UPDATE operational_incidents SET escalate_at=NULL,version=version+1 WHERE state<>'resolved';
    UPDATE managed_services SET state='failed',version=version+1,updated_at=now();
  END IF;
END $$;
SQL

  printf 'Isolated recovery verified; the temporary database will now be removed.\n'
  ;;
 loop)
  interval="${BACKUP_INTERVAL_SECONDS:-86400}"
  case "$interval" in *[!0-9]*|'') exit 2;;esac
  if [ "$interval" -lt 300 ]; then echo 'Backup interval must be at least 300 seconds.' >&2; exit 2; fi
  while true; do if ! backup; then echo 'backup_failed: inspect backup container and repository' >&2; fi; sleep "$interval" & wait $!; done
  ;;
 *) echo 'Use init, backup, verify or loop.' >&2; exit 2 ;;
esac
