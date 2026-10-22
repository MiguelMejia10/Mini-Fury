import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import database
class RestoreSafety(unittest.TestCase):
    def fixture(self, directory):
        path=Path(directory)/'test.dump';path.write_bytes(b'not-a-real-dump')
        path.with_suffix('.json').write_text(json.dumps({'format':1,'sha256':hashlib.sha256(path.read_bytes()).hexdigest()}))
        return path
    def test_existing_live_database_is_never_a_target(self):
        with tempfile.TemporaryDirectory() as d, patch.object(database,'command') as command:
            with self.assertRaises(ValueError): database.restore(self.fixture(d),'mini_fury',True)
            command.assert_not_called()
    def test_tampered_backup_is_not_restored(self):
        with tempfile.TemporaryDirectory() as d, patch.object(database,'command') as command:
            path=self.fixture(d);path.write_bytes(b'tampered')
            with self.assertRaises(ValueError):database.restore(path)
            command.assert_not_called()
    def test_createdb_failure_never_drops_existing_database(self):
        with tempfile.TemporaryDirectory() as d, patch.object(database,'command',side_effect=RuntimeError('exists')) as command:
            with self.assertRaises(RuntimeError):database.restore(self.fixture(d),'mini_fury_restore_existing')
            self.assertEqual(command.call_count,1)
    def test_failed_verification_cleans_only_new_database(self):
        with tempfile.TemporaryDirectory() as d, patch.object(database,'command') as command, patch.object(database,'sql',side_effect=RuntimeError('verification failed')):
            with self.assertRaises(RuntimeError):database.restore(self.fixture(d),'mini_fury_restore_test')
            self.assertEqual(command.call_args.args[0],['dropdb','-U','mini_fury','mini_fury_restore_test'])
if __name__=='__main__':unittest.main()
