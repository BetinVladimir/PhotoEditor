import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from studio_agent.process_lock import JournalLock


class JournalLockTest(unittest.TestCase):
    def test_same_journal_rejected_and_released_on_exception(self):
        with tempfile.TemporaryDirectory() as root:
            journal = Path(root)/'journal.sqlite'
            with self.assertRaises(ValueError):
                with JournalLock(journal):
                    with self.assertRaises(RuntimeError):
                        with JournalLock(journal):
                            self.fail('duplicate acquired')
                    raise ValueError('simulate shutdown')
            with JournalLock(journal):
                pass

    def test_other_process_cannot_acquire_active_journal(self):
        with tempfile.TemporaryDirectory() as root:
            journal = Path(root)/'journal.sqlite'
            code = 'import sys; from pathlib import Path; from studio_agent.process_lock import JournalLock; lock=JournalLock(Path(sys.argv[1])); lock.__enter__()'
            with JournalLock(journal):
                result = subprocess.run([sys.executable,'-c',code,str(journal)],capture_output=True,timeout=5)
                self.assertNotEqual(result.returncode,0)
            result = subprocess.run([sys.executable,'-c',code,str(journal)],capture_output=True,timeout=5)
            self.assertEqual(result.returncode,0,result.stderr.decode())
            with JournalLock(journal):
                pass
