import os
from pathlib import Path
import subprocess
import tempfile
import unittest

WORKER=Path(__file__).resolve().parents[1]/'infra/backup/backup.sh'
class BackupWorkerSafety(unittest.TestCase):
    def run_fixture(self,action,programs):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);log=root/'calls';binpath=root/'bin';binpath.mkdir()
            for name,body in programs.items():
                p=binpath/name;p.write_text('#!/bin/sh\n'+body+'\n');p.chmod(0o700)
            env={**os.environ,'PATH':str(binpath)+':'+os.environ['PATH'],'RESTIC_REPOSITORY':'/fixture','RESTIC_PASSWORD':'fixture-only','PGPASSWORD':'fixture-only','FIXTURE_CALLS':str(log)}
            result=subprocess.run(['/bin/sh',str(WORKER),action],env=env,capture_output=True,text=True)
            return result.returncode,log.read_text() if log.exists() else ''
    def test_failed_createdb_never_drops_existing_database(self):
        code,calls=self.run_fixture('verify',{'restic':'printf fake-dump','createdb':'exit 1','dropdb':'echo unexpected-drop >> "$FIXTURE_CALLS"'})
        self.assertNotEqual(code,0);self.assertNotIn('unexpected-drop',calls)
    def test_failed_dump_never_uploads_or_prunes(self):
        code,calls=self.run_fixture('backup',{'pg_dump':'exit 1','restic':'echo unexpected-restic >> "$FIXTURE_CALLS"'})
        self.assertNotEqual(code,0);self.assertNotIn('unexpected-restic',calls)
    def test_failed_upload_never_prunes(self):
        code,calls=self.run_fixture('backup',{'pg_dump':'printf fixture','restic':'echo "$1" >> "$FIXTURE_CALLS"; exit 1'})
        self.assertNotEqual(code,0);self.assertIn('backup',calls);self.assertNotIn('forget',calls)
if __name__=='__main__':unittest.main()
