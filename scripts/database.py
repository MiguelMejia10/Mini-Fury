#!/usr/bin/env python3
"""Local pg_dump backups and isolated restores; never overwrite an existing DB."""
import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import uuid
ROOT = Path(__file__).resolve().parents[1]
DOCKER = ['docker', 'compose', 'exec', '-T', 'postgres']
TABLES = ['schema_migrations', 'users', 'applications', 'audit_events']
COLLABORATION_TABLES = ['teams', 'team_members', 'team_invitations', 'application_dependencies', 'platform_alerts']
def command(args, **kwargs):
    return subprocess.run(DOCKER + args, cwd=ROOT, check=True, **kwargs)
def sql(database, text):
    return command(['psql', '-X', '-v', 'ON_ERROR_STOP=1', '-U', 'mini_fury', '-d', database, '-Atc', text], stdout=subprocess.PIPE, text=True).stdout.strip()
def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as f:
        for block in iter(lambda: f.read(1024 * 1024), b''): h.update(block)
    return h.hexdigest()
def backup():
    directory = ROOT / '.local' / 'backups'
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    path = directory / (dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + uuid.uuid4().hex[:8] + '.dump')
    try:
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, 'wb') as output:
            command(['pg_dump', '-U', 'mini_fury', '-d', 'mini_fury', '-Fc', '--no-owner', '--no-acl'], stdout=output)
        manifest = {'format': 1, 'sha256': digest(path), 'createdAt': dt.datetime.now(dt.timezone.utc).isoformat(), 'database': 'mini_fury'}
        with path.with_suffix('.json').open('x') as f:
            os.chmod(f.name, 0o600)
            json.dump(manifest, f, indent=2)
    except Exception:
        path.unlink(missing_ok=True)
        raise
    print(path)
    return path

def restore(path, target=None, keep=False):
    path = path.resolve(strict=True)
    manifest = json.loads(path.with_suffix('.json').read_text())
    if manifest.get('format') != 1 or digest(path) != manifest.get('sha256'):
        raise ValueError('El checksum del respaldo no coincide. No se restauró nada.')
    database = target or 'mini_fury_restore_' + uuid.uuid4().hex[:16]
    if not re.fullmatch(r'mini_fury_restore_[a-z0-9_]{1,40}', database):
        raise ValueError('El destino debe ser nuevo y empezar con mini_fury_restore_.')
    created = False
    completed = False
    try:
        # createdb fails if the name exists. No --clean and no DROP on existing databases.
        command(['createdb', '-U', 'mini_fury', database])
        created = True
        with path.open('rb') as stream:
            command(['pg_restore', '-U', 'mini_fury', '-d', database, '--single-transaction', '--exit-on-error', '--no-owner', '--no-acl'], stdin=stream)
        counts = {table: int(sql(database, 'SELECT count(*) FROM ' + table)) for table in TABLES}
        if counts['schema_migrations'] < 2:
            raise ValueError('El esquema restaurado no corresponde al proyecto.')
        if sql(database, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version='004_teams_quality_operations.sql')") == 't':
            counts.update({table: int(sql(database, 'SELECT count(*) FROM ' + table)) for table in COLLABORATION_TABLES})
        # Restoring a backup must not revive previously revoked login sessions.
        sql(database, 'DELETE FROM sessions; DELETE FROM auth_attempts;')
        print(json.dumps({'database': database, 'verified': True, 'rows': counts, 'sessionsRevoked': True}, ensure_ascii=False))
        completed = True
        if keep:
            print('Base restaurada conservada. La API sigue usando mini_fury; no se cambió su conexión.')
            return database
    finally:
        if created and (not keep or not completed):
            command(['dropdb', '-U', 'mini_fury', database])

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='action', required=True)
    sub.add_parser('backup')
    verify = sub.add_parser('verify'); verify.add_argument('file', type=Path)
    restore_cmd = sub.add_parser('restore'); restore_cmd.add_argument('file', type=Path); restore_cmd.add_argument('--database', required=True)
    args = parser.parse_args()
    if args.action == 'backup': backup()
    else: restore(args.file, getattr(args, 'database', None), keep=args.action == 'restore')
if __name__ == '__main__':
    try: main()
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        print('Operación no completada:', error, file=sys.stderr); sys.exit(1)
