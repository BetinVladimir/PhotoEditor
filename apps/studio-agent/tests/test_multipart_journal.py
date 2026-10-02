import tempfile
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


if __name__ == "__main__":
    unittest.main()
