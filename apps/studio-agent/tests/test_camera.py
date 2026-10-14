import tempfile
import os
import unittest
from dataclasses import replace
from pathlib import Path

from studio_agent.camera import WatchFolderAdapter
from studio_agent.journal import Journal
from studio_agent.watchfolder import WatchFolder


class WatchAdapterTest(unittest.TestCase):
    def test_real_file_adapter_lifecycle_and_source_guard(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / 'camera'
            root.mkdir()
            source = root / 'frame.jpg'
            source.write_bytes(b'original capture')
            journal = Journal(Path(directory) / 'journal.sqlite')
            try:
                adapter = WatchFolderAdapter(WatchFolder(root, journal, 'device'))
                self.assertEqual(adapter.list_devices()[0].adapter, 'watch-folder')
                with self.assertRaises(ValueError):
                    adapter.capture_events(1)
                with self.assertRaises(ValueError):
                    adapter.connect('other')
                adapter.connect('device')
                self.assertEqual(adapter.capture_events(1), [])
                entry = adapter.capture_events(2)[0]
                with adapter.read_file(entry) as file:
                    self.assertEqual(file.read(), b'original capture')
                journal.bind_upload('device', entry.sequence, 'upload', 8, 2)
                current = journal.transition('device', entry.sequence,
                                             'hashed', 'uploading')
                with self.assertRaises(ValueError):
                    adapter.read_file(entry)
                with adapter.read_file(current) as file:
                    self.assertEqual(file.read(), b'original capture')
                with self.assertRaises(ValueError):
                    adapter.read_file(replace(entry, device_id='other'))
                original_mtime = source.stat().st_mtime_ns
                source.write_bytes(b'altered  capture')
                os.utime(source, ns=(source.stat().st_atime_ns, original_mtime))
                self.assertEqual((source.stat().st_size, source.stat().st_mtime_ns),
                                 (entry.size, entry.mtime_ns))
                with self.assertRaises(ValueError):
                    adapter.read_file(current)
                source.write_bytes(b'changed')
                with self.assertRaises(ValueError):
                    adapter.read_file(current)
                adapter.disconnect()
                with self.assertRaises(ValueError):
                    adapter.read_file(current)
                self.assertEqual(source.read_bytes(), b'changed')
            finally:
                journal.close()

    def test_capture_hash_batches_preserve_every_discovery(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / 'camera'
            root.mkdir()
            for number in range(5):
                (root / f'{number}.jpg').write_bytes(f'frame-{number}'.encode())
            journal = Journal(Path(directory) / 'journal.sqlite')
            try:
                watch = WatchFolder(root, journal, 'device')
                adapter = WatchFolderAdapter(watch)
                adapter.connect('device')
                self.assertEqual(adapter.capture_events(1), [])
                batches = [adapter.capture_events(now) for now in (2, 3, 4)]
                self.assertEqual([len(batch) for batch in batches], [2, 2, 1])
                self.assertEqual(len({entry.sequence for batch in batches for entry in batch}), 5)
                self.assertEqual(len(journal.pending('device', 'hashed', 100)), 5)
                self.assertEqual(adapter.capture_events(5), [])
            finally:
                journal.close()

    def test_unreadable_hash_is_deferred_without_starving_other_frames(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / 'camera'
            root.mkdir()
            for number in range(4):
                (root / f'{number}.jpg').write_bytes(b'frame')
            journal = Journal(Path(directory) / 'journal.sqlite')
            try:
                watch = WatchFolder(root, journal, 'device')
                watch.scan(1, max_hashes=2)
                real_hash = watch.hash_stable
                attempted = []
                def hash_one(entry):
                    attempted.append(entry.sequence)
                    return None if entry.sequence == 1 else real_hash(entry)
                watch.hash_stable = hash_one
                self.assertEqual(len(watch.scan(2, max_hashes=2)), 1)
                self.assertEqual(len(watch.scan(3, max_hashes=2)), 2)
                self.assertEqual(attempted, [1, 2, 3, 4])
                self.assertEqual(journal.get('device', 1).state, 'stable')
                self.assertEqual(journal.stable_due('device', 3), [])
                self.assertEqual([entry.sequence for entry in journal.stable_due('device', 7)], [1])
            finally:
                journal.close()
