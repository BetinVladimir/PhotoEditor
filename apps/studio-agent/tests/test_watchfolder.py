import hashlib
import tempfile
import unittest
from pathlib import Path

from studio_agent.journal import Journal
from studio_agent.watchfolder import WatchFolder


class WatchFolderTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "camera"
        self.root.mkdir()
        self.journal = Journal(Path(self.temp.name) / "journal.sqlite")
        self.addCleanup(self.journal.close)
        self.watch = WatchFolder(self.root, self.journal, "device")

    def test_two_poll_stability_and_restart_sequence(self):
        image = self.root / "A.JPG"
        image.write_bytes(b"camera original")
        self.assertEqual(self.watch.scan(10), [])
        self.assertEqual(self.watch.scan(10.5), [])
        ready = self.watch.scan(11)
        self.assertEqual(len(ready), 1)
        self.assertEqual(ready[0].sequence, 1)
        self.assertEqual(ready[0].sha256, hashlib.sha256(b"camera original").hexdigest())
        self.assertTrue(self.watch.rehash_before_complete(ready[0]))
        self.assertEqual(self.watch.scan(12), [])
        self.journal.transition("device", 1, "hashed", "uploading")
        self.journal.close()
        self.journal = Journal(Path(self.temp.name) / "journal.sqlite")
        self.watch.journal = self.journal
        self.journal.recover("device")
        self.assertEqual(self.journal.get("device", 1).state, "hashed")
        second = self.root / "B.NEF"
        second.write_bytes(b"another original")
        self.watch.scan(20)
        self.assertEqual(self.watch.scan(21)[0].sequence, 2)
        self.assertEqual(image.read_bytes(), b"camera original")

    def test_symlinks_are_never_scanned_or_opened(self):
        outside = Path(self.temp.name) / "outside.jpg"
        outside.write_bytes(b"secret")
        (self.root / "escape.jpg").symlink_to(outside)
        (self.root / "escape-dir").symlink_to(outside.parent, target_is_directory=True)
        self.assertEqual(self.watch.scan(1), [])
        self.assertEqual(self.watch.scan(2), [])
        self.assertIsNone(self.journal.get("device", 1))
        with self.assertRaises(ValueError):
            self.watch._open_no_symlinks("../outside.jpg")
        with self.assertRaises(ValueError):
            self.watch._open_no_symlinks(str(outside))

    def test_content_change_before_complete_is_rejected(self):
        image = self.root / "a.jpg"
        image.write_bytes(b"first")
        self.watch.scan(1)
        entry = self.watch.scan(2)[0]
        image.write_bytes(b"second")
        self.assertFalse(self.watch.rehash_before_complete(entry))
        self.watch.scan(3)
        self.assertEqual(self.watch.scan(4)[0].sequence, 2)

    def test_journal_state_is_compare_and_swap(self):
        image = self.root / "a.jpg"
        image.write_bytes(b"image")
        self.watch.scan(1)
        entry = self.watch.scan(2)[0]
        self.journal.transition("device", entry.sequence, "hashed", "uploading")
        with self.assertRaises(ValueError):
            self.journal.transition("device", entry.sequence, "hashed", "uploading")
        self.journal.fail("device", entry.sequence, "uploading", "network")
        self.assertEqual(self.journal.get("device", entry.sequence).retries, 1)

    def test_queue_byte_budget_delays_new_file_without_losing_sequence(self):
        self.watch = WatchFolder(self.root, self.journal, "device", max_queued_bytes=5)
        first = self.root / "a.jpg"
        first.write_bytes(b"12345")
        self.watch.scan(1)
        self.watch.scan(2)
        second = self.root / "b.jpg"
        second.write_bytes(b"abc")
        self.watch.scan(3)
        self.assertIsNone(self.journal.get("device", 2))
        self.journal.transition("device", 1, "hashed", "uploading")
        self.journal.transition("device", 1, "uploading", "verified", upload_id="upload")
        self.journal.transition("device", 1, "verified", "receipt")
        self.journal.transition("device", 1, "receipt", "done")
        self.watch.scan(4)
        self.assertEqual(self.watch.scan(5)[0].sequence, 2)


if __name__ == "__main__":
    unittest.main()
