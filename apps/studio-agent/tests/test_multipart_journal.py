import tempfile
import sqlite3
import unittest
from pathlib import Path

from studio_agent.journal import Journal


class MultipartJournalTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / "journal.sqlite"
        self.journal = Journal(self.path)
        self.addCleanup(lambda: self.journal.close())
        self.journal.observe("device", "frame.jpg", 7, 1, now=1)
        self.journal.observe("device", "frame.jpg", 7, 1, now=2)
        self.journal.transition("device", 1, "stable", "hashed", sha256="a" * 64)

    def test_restart_retains_upload_and_exact_completion_parts(self):
        self.journal.bind_upload("device", 1, "upload", 4, 2)
        self.journal.transition("device", 1, "hashed", "uploading")
        self.journal.record_part("device", 1, 2, '"last"', 3)
        self.journal.record_part("device", 1, 1, '"first"', 4)
        self.journal.close()
        self.journal = Journal(self.path)
        self.journal.recover("device")
        entry = self.journal.get("device", 1)
        self.assertEqual((entry.state, entry.upload_id), ("hashed", "upload"))
        self.journal.bind_upload("device", 1, "upload", 4, 2)
        self.assertEqual(self.journal.completed_parts("device", 1), [
            {"part_no": 1, "etag": '"first"'}, {"part_no": 2, "etag": '"last"'}])
        self.assertEqual(self.journal.completed_parts("other-device", 1), [])

    def test_existing_journal_adds_nullable_raw_policy_without_rewriting_entry(self):
        self.journal.close()
        self.journal.db = sqlite3.connect(self.path)
        self.journal.db.execute("ALTER TABLE entries DROP COLUMN raw_policy_json")
        self.journal.db.commit()
        self.journal.db.close()
        self.journal = Journal(self.path)
        entry = self.journal.get("device", 1)
        self.assertEqual((entry.state, entry.sha256, entry.raw_policy_json),
                         ("hashed", "a" * 64, None))
        self.assertIn("raw_policy_json", [row[1] for row in
                      self.journal.db.execute("PRAGMA table_info(entries)")])

    def test_layout_identity_and_acknowledged_part_cannot_change(self):
        with self.assertRaises(ValueError):
            self.journal.bind_upload("device", 1, "upload", 4, 3)
        self.journal.bind_upload("device", 1, "upload", 4, 2)
        with self.assertRaises(ValueError):
            self.journal.bind_upload("device", 1, "other-upload", 4, 2)
        self.journal.transition("device", 1, "hashed", "uploading")
        with self.assertRaises(ValueError):
            self.journal.record_part("device", 1, 2, "etag", 4)
        with self.assertRaises(ValueError):
            self.journal.record_part("device", 1, 3, "etag", 1)
        self.journal.record_part("device", 1, 1, "etag", 4)
        self.journal.record_part("device", 1, 1, "etag", 4)
        with self.assertRaises(ValueError):
            self.journal.record_part("device", 1, 1, "changed", 4)
        with self.assertRaises(ValueError):
            self.journal.transition("device", 1, "uploading", "verified", upload_id="other-upload")
        self.assertEqual(self.journal.get("device", 1).state, "uploading")


    def test_invalid_ack_types_never_enter_durable_checkpoint(self):
        self.journal.bind_upload("device", 1, "upload", 4, 2)
        self.journal.transition("device", 1, "hashed", "uploading")
        invalid = [(True, "etag", 4), (1.0, "etag", 4), (1, "etag", 4.0),
                   (1, "etag", True), (0, "etag", 4), (10001, "etag", 4),
                   (1, "etag", 0), (1, "etag", 16 * 1024**2 + 1),
                   (1, b"etag", 4), (1, None, 4), (1, "", 4),
                   (1, "a\nb", 4), (1, "a\tb", 4), (1, "a\x00b", 4),
                   (1, "a\x7fb", 4)]
        for number, etag, size in invalid:
            with self.subTest(number=number, etag=etag, size=size):
                with self.assertRaises(ValueError):
                    self.journal.record_part("device", 1, number, etag, size)
                self.assertFalse(self.journal.db.in_transaction)
                self.assertEqual(self.journal.completed_parts("device", 1), [])
                self.assertEqual(self.journal.get("device", 1).state, "uploading")
        self.journal.record_part("device", 1, 1, "valid-etag", 4)
        self.assertEqual(self.journal.completed_parts("device", 1), [
            {"part_no": 1, "etag": "valid-etag"}])


    def test_frozen_source_hash_cannot_change_at_later_state_transitions(self):
        self.journal.bind_upload("device", 1, "upload", 4, 2)
        for before, after in (("hashed", "uploading"), ("uploading", "verified"),
                              ("verified", "receipt"), ("receipt", "done")):
            with self.subTest(before=before, after=after):
                with self.assertRaises(ValueError):
                    self.journal.transition("device", 1, before, after,
                                            sha256="b" * 64, upload_id="upload")
                current = self.journal.get("device", 1)
                self.assertEqual((current.state, current.sha256), (before, "a" * 64))
                self.journal.transition("device", 1, before, after, upload_id="upload")
        self.assertEqual(self.journal.get("device", 1).sha256, "a" * 64)

    def test_generic_transition_cannot_create_upload_admission(self):
        before = self.journal.get("device", 1)
        with self.assertRaises(ValueError):
            self.journal.transition("device", 1, "hashed", "uploading",
                                    upload_id="unadmitted")
        self.assertEqual(self.journal.get("device", 1), before)
        self.assertFalse(self.journal.db.in_transaction)
        self.assertEqual(self.journal.db.execute(
            "SELECT COUNT(*) FROM multipart_uploads").fetchone()[0], 0)
        self.journal.bind_upload("device", 1, "admitted", 4, 2)
        self.journal.transition("device", 1, "hashed", "uploading",
                                upload_id="admitted")
        self.assertEqual(self.journal.get("device", 1).upload_id, "admitted")

    def test_hash_transition_cannot_smuggle_upload_identity(self):
        self.journal.observe("device", "second.jpg", 7, 1, now=1)
        self.journal.observe("device", "second.jpg", 7, 1, now=2)
        before = self.journal.get("device", 2)
        with self.assertRaises(ValueError):
            self.journal.transition("device", 2, "stable", "hashed",
                                    sha256="b" * 64, upload_id="unadmitted")
        self.assertEqual(self.journal.get("device", 2), before)
        self.journal.transition("device", 2, "stable", "hashed", sha256="b" * 64)
        self.assertIsNone(self.journal.get("device", 2).upload_id)

    def test_verified_state_requires_previously_bound_upload(self):
        self.journal.transition("device", 1, "hashed", "uploading")
        with self.assertRaises(ValueError):
            self.journal.transition("device", 1, "uploading", "verified", upload_id="unadmitted")
        current = self.journal.get("device", 1)
        self.assertEqual(current.state, "uploading")
        self.assertIsNone(current.upload_id)
        self.assertEqual(self.journal.completed_parts("device", 1), [])


    def test_retry_schedule_rejects_nonfinite_values_without_losing_due_entry(self):
        before = self.journal.get("device", 1)
        for value in (float("inf"), float("-inf"), float("nan"), True, -1, "later"):
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    self.journal.delay("device", 1, value, "transport_retry")
                with self.assertRaises(ValueError):
                    self.journal.due("device", value)
                with self.assertRaises(ValueError):
                    self.journal.stable_due("device", value)
                self.assertEqual(self.journal.get("device", 1), before)
                self.assertEqual([entry.sequence for entry in self.journal.due("device", 10)], [1])
        for limit in (True, 1.0, 0, 101):
            with self.assertRaises(ValueError):
                self.journal.due("device", 10, limit)
        self.journal.delay("device", 1, 20, "transport_retry")
        self.journal.close()
        self.journal = Journal(self.path)
        self.assertEqual(self.journal.due("device", 19), [])
        self.assertEqual([entry.sequence for entry in self.journal.due("device", 20)], [1])


if __name__ == "__main__":
    unittest.main()
