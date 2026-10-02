import tempfile
import unittest
import threading
from pathlib import Path

from studio_agent.journal import Journal
from studio_agent.transfer import Transfer, SourceChanged
from studio_agent.watchfolder import WatchFolder
from studio_agent.runner import Runner
from studio_agent.http import APIError


class FakeAPI:
    def __init__(self):
        self.state = "uploading"
        self.parts = []
        self.keys = []
        self.lose_complete_reply = False
        self.lose_receipt_reply = False

    def begin(self, entry, key):
        self.keys.append(key)
        return {"id": "upload", "part_bytes": 4, "parts_total": 2}

    def get(self, upload_id):
        return {"id": upload_id, "state": self.state, "part_bytes": 4, "parts_total": 2}

    def upload_part(self, upload_id, number, data):
        self.parts.append((number, data))
        return f"etag-{number}"

    def complete(self, upload_id, parts, size, key):
        self.keys.append(key)
        self.state = "verified"
        if self.lose_complete_reply:
            raise ConnectionError("reply lost after completion accepted")

    def receipt(self, entry, key):
        self.keys.append(key)
        if self.lose_receipt_reply:
            self.lose_receipt_reply = False
            raise ConnectionError("reply lost after receipt accepted")
        return {"device_id": entry.device_id, "sequence": str(entry.sequence), "content_sha": entry.sha256, "upload_id": entry.upload_id}


class TransferTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "camera"
        self.root.mkdir()
        self.source = self.root / "frame.jpg"
        self.source.write_bytes(b"1234567")
        self.path = Path(self.temp.name) / "journal.sqlite"
        self.journal = Journal(self.path)
        self.addCleanup(lambda: self.journal.close())
        self.watch = WatchFolder(self.root, self.journal, "device")
        self.watch.scan(1)
        self.watch.scan(2)
        self.api = FakeAPI()
        self.transfer = Transfer(self.watch, self.api)

    def test_mismatched_receipt_upload_never_marks_capture_done(self):
        self.api.state = "verified"
        self.journal.bind_upload("device", 1, "upload", 4, 2)
        self.journal.transition("device", 1, "hashed", "uploading")
        self.api.receipt = lambda entry, key: {"device_id": entry.device_id,
            "sequence": str(entry.sequence), "content_sha": entry.sha256,
            "upload_id": "another-upload"}
        with self.assertRaises(ValueError):
            self.transfer.process("device", 1)
        self.assertEqual(self.journal.get("device", 1).state, "receipt")

    def test_operator_retry_preserves_multipart_identity_and_due_schedule(self):
        self.journal.bind_upload("device", 1, "upload", 4, 2)
        self.journal.transition("device", 1, "hashed", "uploading")
        self.journal.record_part("device", 1, 1, "etag-1", 4)
        self.journal.delay("device", 1, 9999, "transport_retry")
        self.journal.fail("device", 1, "uploading", "api_rejected")
        entry = self.watch.retry_failed(1)
        self.assertEqual(entry.state, "hashed")
        self.assertEqual(entry.upload_id, "upload")
        self.assertEqual(entry.sequence, 1)
        self.assertEqual(len(self.journal.due("device", 10)), 1)
        self.transfer.process("device", 1)
        self.assertEqual([number for number, _ in self.api.parts], [2])
        self.assertEqual(self.api.keys, ["capture/device/1/complete"])

    def test_operator_retry_refuses_changed_source_and_keeps_error(self):
        self.journal.fail("device", 1, "hashed", "source_changed")
        self.source.write_bytes(b"changed")
        with self.assertRaises(ValueError):
            self.watch.retry_failed(1)
        self.assertEqual(self.journal.get("device", 1).state, "error")
        self.assertEqual(self.api.keys, [])

    def test_lost_completion_and_receipt_replies_resume_without_reupload(self):
        self.api.lose_complete_reply = True
        with self.assertRaises(ConnectionError):
            self.transfer.process("device", 1)
        self.journal.close()
        self.journal = Journal(self.path)
        self.watch.journal = self.journal
        self.journal.recover("device")
        self.transfer = Transfer(self.watch, self.api)
        self.api.lose_receipt_reply = True
        with self.assertRaises(ConnectionError):
            self.transfer.process("device", 1)
        self.assertEqual(self.journal.get("device", 1).state, "receipt")
        self.assertEqual(self.transfer.process("device", 1).state, "done")
        self.assertEqual(sorted(self.api.parts), [(1, b"1234"), (2, b"567")])
        self.assertEqual(self.api.keys[-2:], ["capture/device/1/receipt"] * 2)
        self.assertEqual(self.source.read_bytes(), b"1234567")

    def test_source_change_prevents_network_admission(self):
        self.source.write_bytes(b"changed")
        with self.assertRaises(SourceChanged):
            self.transfer.process("device", 1)
        self.assertEqual(self.api.keys, [])
        self.assertEqual(self.journal.get("device", 1).state, "hashed")

    def test_two_parallel_parts_checkpoint_successful_sibling_on_failure(self):
        barrier = threading.Barrier(2)
        original = self.api.upload_part
        def partial_failure(upload_id, number, data):
            barrier.wait(timeout=2)
            if number == 1:
                raise ConnectionError("first part lost")
            return original(upload_id, number, data)
        self.api.upload_part = partial_failure
        with self.assertRaises(ConnectionError):
            self.transfer.process("device", 1)
        self.assertEqual(self.journal.completed_parts("device", 1), [{"part_no": 2, "etag": "etag-2"}])
        self.api.upload_part = original
        self.transfer.process("device", 1)
        self.assertEqual(sorted(self.api.parts), [(1, b"1234"), (2, b"567")])

    def test_retry_due_time_survives_restart(self):
        def unavailable(upload_id):
            raise ConnectionError("offline")
        self.api.get = unavailable
        runner = Runner(self.transfer, clock=lambda: 10)
        self.assertEqual(runner.tick(), 1)
        self.assertEqual(self.journal.get("device", 1).retries, 1)
        self.journal.close()
        self.journal = Journal(self.path)
        self.assertEqual(self.journal.due("device", 10), [])
        self.assertEqual(len(self.journal.due("device", 20)), 1)
        self.assertEqual(self.journal.get("device", 1).upload_id, "upload")

    def test_revoked_device_stops_runner_without_retry_storm(self):
        def revoked(upload_id):
            raise APIError(401, "INVALID_CREDENTIALS")
        self.api.get = revoked
        with self.assertRaises(RuntimeError):
            Runner(self.transfer, clock=lambda: 10).tick()
        self.assertEqual(self.journal.get("device", 1).retries, 0)


if __name__ == "__main__":
    unittest.main()
