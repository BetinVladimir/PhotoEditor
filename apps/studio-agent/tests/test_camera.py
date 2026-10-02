import tempfile
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
                with self.assertRaises(ValueError):
                    adapter.read_file(replace(entry, device_id='other'))
                source.write_bytes(b'changed')
                with self.assertRaises(ValueError):
                    adapter.read_file(entry)
                adapter.disconnect()
                with self.assertRaises(ValueError):
                    adapter.read_file(entry)
                self.assertEqual(source.read_bytes(), b'changed')
            finally:
                journal.close()
