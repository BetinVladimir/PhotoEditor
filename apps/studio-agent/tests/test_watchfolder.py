import hashlib
import os
import stat
from contextlib import contextmanager
import io
from unittest.mock import patch
import tempfile
import unittest
from pathlib import Path

from studio_agent.journal import Journal
from studio_agent.watchfolder import WatchFolder
from studio_agent.raw_policy import canonical_raw_policy


class WatchFolderTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "camera"
        self.root.mkdir()
        self.journal = Journal(Path(self.temp.name) / "journal.sqlite")
        self.addCleanup(self.journal.close)
        self.watch = WatchFolder(self.root, self.journal, "device")

    def test_hashing_bounds_reads_for_a_continuously_growing_source(self):
        class GrowingFile:
            def __init__(self):
                self.reads = []
            def __enter__(self):
                return self
            def __exit__(self, *args):
                pass
            def read(self, size):
                self.reads.append(size)
                return b"x" * size
        file = GrowingFile()
        with patch("studio_agent.watchfolder.os.fdopen", return_value=file):
            self.assertIsNone(self.watch._hash_exact(123, 2 * 1024**2 + 7))
        self.assertEqual(file.reads, [1024**2, 1024**2, 7, 1])

    def test_exact_hash_rejects_truncation_and_accepts_exact_source(self):
        for content, expected in ((b"ab", None), (b"abcd", None),
                                  (b"abc", hashlib.sha256(b"abc").hexdigest())):
            with self.subTest(content=content):
                with patch("studio_agent.watchfolder.os.fdopen", return_value=io.BytesIO(content)):
                    self.assertEqual(self.watch._hash_exact(123, 3), expected)

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

    def test_raw_choice_freezes_with_hash_and_survives_restart(self):
        policy = {"schema_version": 1, "demosaic": "dcb", "wb_source": "daylight",
                  "exposure_ev": 1, "highlight_policy": "reconstruct",
                  "decoder_version": "rawpy-0.27.1/libraw-0.21.4"}
        frozen = canonical_raw_policy(policy)
        watch = WatchFolder(self.root, self.journal, "device", raw_policy_json=frozen)
        (self.root / "capture.NEF").write_bytes(b"raw original")
        watch.scan(10)
        raw = watch.scan(11)[0]
        self.assertEqual(raw.raw_policy_json, frozen)
        watch.raw_policy_json = None
        self.journal.close()
        self.journal = Journal(Path(self.temp.name) / "journal.sqlite")
        self.assertEqual(self.journal.get("device", raw.sequence).raw_policy_json, frozen)
        self.journal.fail("device", raw.sequence, "hashed", "api_rejected")
        watch.journal = self.journal
        retried = watch.retry_failed(raw.sequence)
        self.assertEqual((retried.state, retried.raw_policy_json), ("hashed", frozen))
        (self.root / "preview.jpg").write_bytes(b"jpeg original")
        watch.scan(20)
        jpeg = watch.scan(21)[0]
        self.assertIsNone(jpeg.raw_policy_json)

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

    def test_hardlinks_to_files_outside_watch_root_are_not_admitted(self):
        outside = Path(self.temp.name) / "private.jpg"
        outside.write_bytes(b"outside content")
        os.link(outside, self.root / "linked.jpg")
        self.assertEqual(self.watch.scan(1), [])
        self.assertEqual(self.watch.scan(2), [])
        self.assertIsNone(self.journal.get("device", 1))
        with self.assertRaises(ValueError):
            self.watch._open_no_symlinks("linked.jpg")
        self.assertEqual(outside.read_bytes(), b"outside content")

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

    def test_growing_unhashed_file_reuses_observation_and_resets_stability(self):
        self.watch = WatchFolder(self.root, self.journal, "device", max_queued_bytes=8)
        source = self.root / "growing.jpg"
        source.write_bytes(b"12")
        self.watch.scan(1)
        source.write_bytes(b"1234")
        self.watch.scan(2)
        self.assertEqual(self.journal.queued_bytes("device"), 4)
        self.assertIsNone(self.journal.get("device", 2))
        self.assertEqual(self.watch.scan(2.5), [])
        ready = self.watch.scan(3)
        self.assertEqual([(entry.sequence, entry.size) for entry in ready], [(1, 4)])
        self.assertEqual(ready[0].sha256, hashlib.sha256(b"1234").hexdigest())
        other = self.root / "other.jpg"
        other.write_bytes(b"abc")
        self.watch.scan(4)
        self.assertEqual(self.watch.scan(5)[0].sequence, 2)

    def test_changed_stable_unhashed_observation_restarts_stability(self):
        self.journal.observe("device", "frame.jpg", 2, 1, now=1)
        self.journal.observe("device", "frame.jpg", 2, 1, now=2)
        entry = self.journal.observe("device", "frame.jpg", 3, 2, now=3, max_queued_bytes=3)
        self.assertEqual((entry.sequence, entry.state, entry.size), (1, "discovered", 3))
        self.assertEqual(self.journal.observe("device", "frame.jpg", 3, 2, now=3.5).state, "discovered")
        self.assertEqual(self.journal.observe("device", "frame.jpg", 3, 2, now=4).state, "stable")

    def test_growing_observation_cannot_exceed_queue_budget(self):
        from studio_agent.journal import QueueFull
        self.journal.observe("device", "frame.jpg", 2, 1, now=1, max_queued_bytes=3)
        with self.assertRaises(QueueFull):
            self.journal.observe("device", "frame.jpg", 4, 2, now=2, max_queued_bytes=3)
        entry = self.journal.get("device", 1)
        self.assertEqual((entry.size, entry.mtime_ns), (2, 1))
        self.assertEqual(self.journal.queued_bytes("device"), 2)

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
        self.journal.bind_upload("device", 1, "upload", 5, 1)
        self.journal.transition("device", 1, "hashed", "uploading")
        self.journal.transition("device", 1, "uploading", "verified", upload_id="upload")
        self.journal.transition("device", 1, "verified", "receipt")
        self.journal.transition("device", 1, "receipt", "done")
        self.watch.scan(4)
        self.assertEqual(self.watch.scan(5)[0].sequence, 2)


    def test_nested_scan_reopens_directories_without_following_replacement(self):
        nested = self.root / "nested"
        nested.mkdir()
        outside = Path(self.temp.name) / "outside"
        outside.mkdir()
        (outside / "private.jpg").write_bytes(b"not a camera file")
        real_scandir = os.scandir
        replaced = False

        @contextmanager
        def replace_after_discovery(fd):
            nonlocal replaced
            with real_scandir(fd) as entries:
                yield entries
            if not replaced:
                nested.rmdir()
                nested.symlink_to(outside, target_is_directory=True)
                replaced = True

        with patch("studio_agent.watchfolder.os.scandir", replace_after_discovery):
            self.assertEqual(list(self.watch._files()), [])
        self.assertTrue(replaced)
        self.assertIsNone(self.journal.get("device", 1))

    def test_descriptor_scan_preserves_nested_camera_paths(self):
        directory = self.root / "one" / "two"
        directory.mkdir(parents=True)
        image = directory / "FRAME.JPG"
        image.write_bytes(b"camera")
        self.assertEqual(list(self.watch._files()), [image])
        self.watch.scan(1)
        ready = self.watch.scan(2)
        self.assertEqual(len(ready), 1)
        self.assertEqual(ready[0].relative_path, "one/two/FRAME.JPG")


    def test_watch_root_ancestor_symlink_replacement_cannot_redirect_reads(self):
        parent = Path(self.temp.name) / "capture-parent"
        root = parent / "camera"
        root.mkdir(parents=True)
        (root / "frame.jpg").write_bytes(b"original")
        watch = WatchFolder(root, self.journal, "other-device")
        outside = Path(self.temp.name) / "outside-parent"
        (outside / "camera").mkdir(parents=True)
        (outside / "camera" / "frame.jpg").write_bytes(b"replacement")
        parent.rename(Path(self.temp.name) / "saved-parent")
        parent.symlink_to(outside, target_is_directory=True)
        with self.assertRaises(OSError):
            watch._open_no_symlinks("frame.jpg")
        self.assertEqual(list(watch._files()), [])

    def test_real_watch_root_replacement_requires_agent_restart(self):
        original = self.root / "frame.jpg"
        original.write_bytes(b"original")
        self.root.rename(Path(self.temp.name) / "saved-camera")
        self.root.mkdir()
        (self.root / "frame.jpg").write_bytes(b"replacement")
        with self.assertRaisesRegex(OSError, "watch root identity changed"):
            self.watch._open_no_symlinks("frame.jpg")
        with self.assertRaisesRegex(OSError, "watch root identity changed"):
            list(self.watch._files())


    def test_discovered_file_replacement_is_refused_before_journal_observation(self):
        for replacement in ("symlink", "fifo", "directory"):
            with self.subTest(replacement=replacement):
                image = self.root / (replacement + ".jpg")
                image.write_bytes(b"camera frame")
                image.unlink()
                if replacement == "symlink":
                    outside = Path(self.temp.name) / "private.jpg"
                    outside.write_bytes(b"outside metadata")
                    image.symlink_to(outside)
                elif replacement == "fifo":
                    os.mkfifo(image)
                else:
                    image.mkdir()
                with patch.object(self.watch, "_files", return_value=iter([image])):
                    self.assertEqual(self.watch.scan(1, max_hashes=2), [])
                self.assertIsNone(self.journal.get("device", 1))

    def test_scan_does_not_use_path_stat_after_discovery(self):
        image = self.root / "frame.jpg"
        image.write_bytes(b"camera frame")
        with patch.object(self.watch, "_files", return_value=iter([image])):
            with patch.object(Path, "stat", side_effect=AssertionError("borrowed path stat")):
                self.assertEqual(self.watch.scan(1, max_hashes=2), [])
        entry = self.journal.get("device", 1)
        self.assertEqual(entry.relative_path, "frame.jpg")
        self.assertEqual(entry.size, len(b"camera frame"))


    def test_file_descriptor_closes_when_type_check_fstat_fails(self):
        image = self.root / "frame.jpg"
        image.write_bytes(b"camera")
        real_fstat = os.fstat
        failed_fds = []

        def fail_file_stat(fd):
            info = real_fstat(fd)
            if stat.S_ISREG(info.st_mode):
                failed_fds.append(fd)
                raise OSError("file metadata unavailable")
            return info

        with patch("studio_agent.watchfolder.os.fstat", fail_file_stat):
            with self.assertRaisesRegex(OSError, "file metadata unavailable"):
                self.watch._open_no_symlinks("frame.jpg")
        self.assertEqual(len(failed_fds), 1)
        with self.assertRaises(OSError):
            real_fstat(failed_fds[0])
        self.assertIsNone(self.journal.get("device", 1))


    def test_invalid_observation_never_creates_queue_or_transaction(self):
        invalid = [dict(size=True), dict(size=7.0), dict(size=2**63),
                   dict(mtime_ns=True), dict(mtime_ns=1.0), dict(mtime_ns=2**63),
                   dict(now=float("inf")), dict(now=float("nan")), dict(now=True),
                   dict(now=-1), dict(max_queued_bytes=True), dict(max_queued_bytes=0),
                   dict(max_queued_bytes=1.0), dict(max_queued_bytes=2**63)]
        for change in invalid:
            values = dict(size=7, mtime_ns=1, now=1, max_queued_bytes=100)
            values.update(change)
            with self.subTest(change=change):
                with self.assertRaises(ValueError):
                    self.journal.observe("device", "frame.jpg", **values)
                self.assertFalse(self.journal.db.in_transaction)
                self.assertIsNone(self.journal.get("device", 1))
                self.assertEqual(self.journal.queued_bytes("device"), 0)
        self.journal.observe("device", "frame.jpg", 7, 1, now=1)
        self.assertEqual(self.journal.observe("device", "frame.jpg", 7, 1, now=2).state, "stable")


if __name__ == "__main__":
    unittest.main()
