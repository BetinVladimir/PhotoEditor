import tempfile
import unittest
from pathlib import Path
from studio_agent.journal import Journal


class QueueStatusTest(unittest.TestCase):
    def test_summary_is_device_scoped_and_preserves_retry_lag(self):
        with tempfile.TemporaryDirectory() as root:
            journal = Journal(Path(root)/'journal.sqlite')
            try:
                journal.observe('device', 'private-name.jpg', 7, 100, now=100)
                journal.observe('device', 'private-name.jpg', 7, 100, now=101)
                journal.transition('device', 1, 'stable', 'hashed', sha256='a'*64)
                journal.delay('device', 1, 200, 'transport_retry')
                journal.observe('other', 'other.jpg', 900, 100, now=100)
                summary = journal.summary('device', 150)
                self.assertEqual(summary['states']['hashed'], 1)
                self.assertEqual(summary['queued_bytes'], 7)
                self.assertEqual(summary['due_count'], 0)
                self.assertEqual(summary['oldest_pending_age_seconds'], 50)
                self.assertEqual(journal.summary('device', 201)['due_count'], 1)
                self.assertNotIn('private-name', str(summary))
                self.assertNotIn('a'*64, str(summary))
                empty = journal.summary('missing', 150)
                self.assertEqual(empty['queued_bytes'], 0)
                self.assertIsNone(empty['oldest_pending_age_seconds'])
            finally:
                journal.close()
