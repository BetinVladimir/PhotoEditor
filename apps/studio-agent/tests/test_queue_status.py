import sqlite3
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
                self.assertEqual(summary['schema_version'], 2)
                self.assertEqual(summary['operator_holds'],
                                 {'begin_reconciliation': 0, 'qualification_unavailable': 0})
                self.assertEqual(summary['oldest_pending_age_seconds'], 50)
                self.assertEqual(journal.summary('device', 201)['due_count'], 1)
                self.assertNotIn('private-name', str(summary))
                self.assertNotIn('a'*64, str(summary))
                empty = journal.summary('missing', 150)
                self.assertEqual(empty['queued_bytes'], 0)
                self.assertIsNone(empty['oldest_pending_age_seconds'])
            finally:
                journal.close()

    def test_operator_holds_are_counted_without_source_identifiers(self):
        with tempfile.TemporaryDirectory() as root:
            journal = Journal(Path(root) / 'held.sqlite')
            try:
                journal.observe('device', 'private.NEF', 7, 100, now=100)
                journal.observe('device', 'private.NEF', 7, 100, now=101)
                journal.transition('device', 1, 'stable', 'hashed', sha256='a' * 64)
                journal.hold_for_qualification('device', 1, 500)
                summary = journal.summary('device', 150)
                self.assertEqual(summary['operator_holds']['qualification_unavailable'], 1)
                self.assertEqual(summary['due_count'], 0)
                self.assertNotIn('private.NEF', str(summary))
                self.assertNotIn('a' * 64, str(summary))
                self.assertEqual(journal.summary('other', 150)['operator_holds']['qualification_unavailable'], 0)
            finally:
                journal.close()

    def test_readonly_status_sees_live_wal_and_cannot_mutate_queue(self):
        with tempfile.TemporaryDirectory() as root:
            path = Path(root) / 'journal #1.sqlite'
            writer = Journal(path)
            reader = Journal(path, readonly=True)
            try:
                writer.observe('device', 'frame.jpg', 7, 100, now=100)
                self.assertEqual(reader.summary('device', 150)['states']['discovered'], 1)
                with self.assertRaises(sqlite3.OperationalError):
                    reader.observe('device', 'other.jpg', 8, 100, now=100)
                self.assertEqual(writer.summary('device', 150)['queued_bytes'], 7)
            finally:
                reader.close()
                writer.close()

    def test_readonly_status_never_creates_missing_database_or_parent(self):
        with tempfile.TemporaryDirectory() as root:
            path = Path(root) / 'absent' / 'journal.sqlite'
            with self.assertRaises(sqlite3.OperationalError):
                Journal(path, readonly=True)
            self.assertFalse(path.parent.exists())

    def test_readonly_status_does_not_migrate_old_schema(self):
        with tempfile.TemporaryDirectory() as root:
            path = Path(root) / 'old.sqlite'
            db = sqlite3.connect(path)
            db.execute('CREATE TABLE entries (device_id TEXT)')
            db.commit()
            db.close()
            reader = Journal(path, readonly=True)
            try:
                with self.assertRaises(sqlite3.OperationalError):
                    reader.summary('device', 150)
                columns = [row[1] for row in reader.db.execute('PRAGMA table_info(entries)')]
                self.assertEqual(columns, ['device_id'])
            finally:
                reader.close()
