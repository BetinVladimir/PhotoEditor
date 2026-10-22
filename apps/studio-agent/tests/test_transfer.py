import tempfile
import os
import unittest
import threading
import json
import uuid
from datetime import datetime, timedelta, timezone
from unittest.mock import patch
from pathlib import Path

from studio_agent.journal import Journal
from studio_agent.transfer import Transfer, SourceChanged
from studio_agent.watchfolder import WatchFolder
from studio_agent.runner import Runner
from studio_agent.http import APIError, HTTPClient


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

    def test_remote_state_cannot_replace_durable_upload_identity_or_layout(self):
        self.journal.bind_upload("device", 1, "upload", 4, 2)
        self.journal.transition("device", 1, "hashed", "uploading")
        for state in ("uploading", "initializing", "completing", "verifying", "verified"):
            for change in ({"id": "foreign"}, {"part_bytes": 5},
                           {"parts_total": True}, {"part_bytes": None},
                           {"part_bytes": 4.0}):
                with self.subTest(state=state, change=change):
                    response = {"id": "upload", "state": state,
                                "part_bytes": 4, "parts_total": 2, **change}
                    self.api.get = lambda upload_id, result=response: result
                    with self.assertRaises(ValueError):
                        self.transfer.process("device", 1)
                    self.assertEqual(self.journal.get("device", 1).state, "uploading")
                    self.assertEqual(self.journal.get("device", 1).upload_id, "upload")
                    self.assertEqual(self.api.parts, [])
                    self.assertEqual(self.api.keys, [])

    def test_malformed_receipt_never_marks_capture_done(self):
        self.api.state = "verified"
        self.journal.bind_upload("device", 1, "upload", 4, 2)
        self.journal.transition("device", 1, "hashed", "uploading")
        for value in (None, [], "receipt", 1):
            with self.subTest(value=value):
                self.api.receipt = lambda entry, key, result=value: result
                with self.assertRaises(ValueError):
                    self.transfer.process("device", 1)
                self.assertEqual(self.journal.get("device", 1).state, "receipt")

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

    def test_initializing_replays_original_begin_then_uploads_on_next_poll(self):
        self.journal.bind_upload("device", 1, "upload", 4, 2)
        self.journal.transition("device", 1, "hashed", "uploading")
        self.api.state = "initializing"
        original = self.api.begin
        def finish_initialization(entry, key):
            response = original(entry, key)
            self.api.state = "uploading"
            return response
        self.api.begin = finish_initialization
        self.assertEqual(self.transfer.process("device", 1).state, "uploading")
        self.assertEqual(self.api.parts, [])
        self.assertEqual(self.api.keys, ["capture/device/1/begin"])
        self.transfer.process("device", 1)
        self.assertEqual(sorted(self.api.parts), [(1, b"1234"), (2, b"567")])
        self.assertEqual(self.transfer.process("device", 1).state, "done")

    def test_initialization_replay_cannot_replace_admitted_upload(self):
        self.journal.bind_upload("device", 1, "upload", 4, 2)
        self.journal.transition("device", 1, "hashed", "uploading")
        self.api.state = "initializing"
        self.api.begin = lambda entry, key: {"id": "foreign", "part_bytes": 4, "parts_total": 2}
        with self.assertRaises(ValueError):
            self.transfer.process("device", 1)
        self.assertEqual(self.journal.get("device", 1).upload_id, "upload")
        self.assertEqual(self.journal.get("device", 1).state, "uploading")
        self.assertEqual(self.api.parts, [])

    def test_initialization_transport_failure_keeps_durable_identity_for_retry(self):
        self.journal.bind_upload("device", 1, "upload", 4, 2)
        self.journal.transition("device", 1, "hashed", "uploading")
        self.api.state = "initializing"
        def offline(entry, key):
            raise ConnectionError("initialization reply lost")
        self.api.begin = offline
        Runner(self.transfer, clock=lambda: 10).tick()
        self.assertEqual(self.journal.get("device", 1).upload_id, "upload")
        self.assertEqual(self.journal.get("device", 1).state, "uploading")
        self.assertEqual(self.journal.due("device", 10), [])
        self.assertEqual(self.api.parts, [])

    def test_lost_begin_reply_restarts_with_same_admission_key(self):
        original = self.api.begin
        def lost_begin(entry, key):
            original(entry, key)
            raise ConnectionError("begin accepted but reply lost")
        self.api.begin = lost_begin
        with self.assertRaises(ConnectionError):
            self.transfer.process("device", 1)
        self.assertIsNone(self.journal.get("device", 1).upload_id)
        self.assertEqual(self.api.parts, [])
        self.journal.close()
        self.journal = Journal(self.path)
        self.watch.journal = self.journal
        self.journal.recover("device")
        self.api.begin = original
        self.transfer = Transfer(self.watch, self.api)
        self.transfer.process("device", 1)
        self.assertEqual(self.api.keys[:2], ["capture/device/1/begin"] * 2)
        self.assertEqual(self.journal.get("device", 1).upload_id, "upload")
        self.assertEqual(sorted(self.api.parts), [(1, b"1234"), (2, b"567")])

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

    def test_same_size_mtime_replacement_stops_before_multipart_put(self):
        entry = self.journal.get("device", 1)
        self.journal.bind_upload("device", 1, "upload", 4, 2)
        self.journal.transition("device", 1, "hashed", "uploading")
        self.source.write_bytes(b"7654321")
        os.utime(self.source, ns=(self.source.stat().st_atime_ns, entry.mtime_ns))
        with self.assertRaises(SourceChanged):
            self.transfer.process("device", 1)
        self.assertEqual(self.api.parts, [])
        self.assertEqual(self.api.keys, [])
        self.assertEqual(self.journal.get("device", 1).state, "uploading")

    def test_restart_keeps_acknowledged_part_but_refuses_changed_remainder(self):
        entry = self.journal.get("device", 1)
        self.journal.bind_upload("device", 1, "upload", 4, 2)
        self.journal.transition("device", 1, "hashed", "uploading")
        self.journal.record_part("device", 1, 1, "etag-1", 4)
        self.journal.close()
        self.journal = Journal(self.path)
        self.watch.journal = self.journal
        self.journal.recover("device")
        self.transfer = Transfer(self.watch, self.api)
        self.source.write_bytes(b"7654321")
        os.utime(self.source, ns=(self.source.stat().st_atime_ns, entry.mtime_ns))
        with self.assertRaises(SourceChanged):
            self.transfer.process("device", 1)
        self.assertEqual(self.journal.get("device", 1).upload_id, "upload")
        self.assertEqual(self.journal.completed_parts("device", 1),
                         [{"part_no": 1, "etag": "etag-1"}])
        self.assertEqual(self.api.parts, [])
        self.assertEqual(self.api.keys, [])

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

    def test_expired_part_capability_retries_same_upload_and_retains_sibling(self):
        original = self.api.upload_part
        attempts = {1: 0, 2: 0}
        def upload(upload_id, number, data):
            attempts[number] += 1
            if number == 2 and attempts[number] == 1:
                raise ConnectionError("capture signed part expired; retry required")
            return original(upload_id, number, data)
        self.api.upload_part = upload
        now = [10]
        runner = Runner(self.transfer, clock=lambda: now[0])
        self.assertEqual(runner.tick(), 1)
        pending = self.journal.get("device", 1)
        self.assertEqual(pending.state, "uploading")
        self.assertEqual(pending.upload_id, "upload")
        self.assertEqual(pending.last_error, "transport_retry")
        self.assertEqual(self.journal.completed_parts("device", 1), [{"part_no": 1, "etag": "etag-1"}])
        now[0] = 100
        self.assertEqual(runner.tick(), 1)
        now[0] = 200
        self.assertEqual(runner.tick(), 1)
        self.assertEqual(self.journal.get("device", 1).state, "done")
        self.assertEqual(attempts, {1: 1, 2: 2})
        self.assertEqual(self.api.keys.count("capture/device/1/begin"), 1)
        self.assertEqual(self.source.read_bytes(), b"1234567")

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

    def test_runner_records_malformed_receipt_as_protocol_failure(self):
        self.api.state = "verified"
        self.journal.bind_upload("device", 1, "upload", 4, 2)
        self.journal.transition("device", 1, "hashed", "uploading")
        self.api.receipt = lambda entry, key: None
        self.assertEqual(Runner(self.transfer, clock=lambda: 10).tick(), 1)
        failed = self.journal.get("device", 1)
        self.assertEqual(failed.state, "error")
        self.assertEqual(failed.last_error, "protocol_invalid")
        self.assertEqual(failed.upload_id, "upload")
        self.assertEqual(failed.retries, 1)
        self.assertEqual(self.journal.due("device", 10000), [])

    def test_revoked_device_stops_runner_without_retry_storm(self):
        def revoked(upload_id):
            raise APIError(401, "INVALID_CREDENTIALS")
        self.api.get = revoked
        with self.assertRaises(RuntimeError):
            Runner(self.transfer, clock=lambda: 10).tick()
        self.assertEqual(self.journal.get("device", 1).retries, 0)

    def test_request_timeout_and_too_early_keep_durable_upload_for_retry(self):
        now = [10]
        runner = Runner(self.transfer, clock=lambda: now[0])
        for status, instant in ((408, 10), (425, 100)):
            with self.subTest(status=status):
                now[0] = instant
                def unavailable(upload_id, code=status):
                    raise APIError(code, "TEMPORARY")
                self.api.get = unavailable
                self.assertEqual(runner.tick(), 1)
                entry = self.journal.get("device", 1)
                self.assertEqual(entry.state, "uploading")
                self.assertEqual(entry.upload_id, "upload")
                self.assertEqual(entry.last_error, "api_retry")
        self.assertEqual(self.journal.get("device", 1).retries, 2)
        self.assertEqual(self.api.keys, ["capture/device/1/begin"])

    def test_begin_reconciliation_holds_same_upload_until_operator_recovers(self):
        self.journal.bind_upload("device", 1, "upload", 4, 2)
        self.journal.transition("device", 1, "hashed", "uploading")
        self.api.state = "initializing"
        original = self.api.begin
        def unresolved(entry, key):
            raise APIError(409, "CAPTURE_BEGIN_RECONCILIATION_REQUIRED")
        self.api.begin = unresolved
        now = [10]
        runner = Runner(self.transfer, clock=lambda: now[0])
        self.assertEqual(runner.tick(), 1)
        held = self.journal.get("device", 1)
        self.assertEqual((held.state, held.upload_id, held.retries, held.last_error),
                         ("uploading", "upload", 0, "begin_reconciliation"))
        self.assertEqual(self.journal.due("device", 309), [])
        self.api.begin = original
        self.api.state = "uploading"
        now[0] = 310
        self.assertEqual(runner.tick(), 1)
        self.assertEqual(self.journal.get("device", 1).upload_id, "upload")

    def test_missing_raw_qualification_holds_frozen_capture_without_retry_storm(self):
        original = self.api.begin
        def unqualified(entry, key):
            raise APIError(503, "QUALIFICATION_UNAVAILABLE")
        self.api.begin = unqualified
        now = [10]
        runner = Runner(self.transfer, clock=lambda: now[0])
        self.assertEqual(runner.tick(), 1)
        held = self.journal.get("device", 1)
        self.assertEqual((held.state, held.upload_id, held.retries, held.last_error),
                         ("hashed", None, 0, "qualification_unavailable"))
        self.assertEqual(self.journal.due("device", 309), [])
        self.api.begin = original
        now[0] = 310
        self.assertEqual(runner.tick(), 1)
        self.assertEqual(self.api.keys, ["capture/device/1/begin", "capture/device/1/complete"])
        self.assertEqual(self.journal.get("device", 1).upload_id, "upload")
        self.assertEqual(self.journal.get("device", 1).retries, 0)
        self.assertEqual(len(self.journal.completed_parts("device", 1)), 2)


    @unittest.skipUnless(hasattr(os, "mkfifo"), "requires POSIX named pipes")
    def test_fifo_source_replacement_refuses_before_network_admission(self):
        self.source.unlink()
        os.mkfifo(self.source)
        with self.assertRaisesRegex(ValueError, "not a regular file"):
            self.watch._open_no_symlinks("frame.jpg")
        entry = self.journal.get("device", 1)
        self.assertFalse(self.watch.rehash_before_complete(entry))
        with self.assertRaises(SourceChanged):
            self.transfer.process("device", 1)
        self.assertEqual(self.api.keys, [])
        self.assertEqual(self.api.parts, [])
        self.assertEqual(self.journal.get("device", 1).state, "hashed")


    def test_storage_auth_retry_after_restart_reuses_successful_sibling(self):
        ids = [str(uuid.uuid4()) for _ in range(5)]
        upload_id = str(uuid.uuid4())
        client = HTTPClient("https://api.example.test", *ids, lambda value: bytes(64),
                            ["https://storage.example.test"])
        client.token = "fixture-device-token"
        client._authenticate = lambda: None
        watch = WatchFolder(self.root, self.journal, ids[4])
        watch.scan(10)
        watch.scan(11)
        signed, puts, completions = [], [], []
        counters = {1: 0, 2: 0}
        state = ["uploading"]
        mutex = threading.Lock()

        def request(method, url, body, headers):
            payload = json.loads(body) if method == "POST" else None
            if method == "PUT":
                number = int(url.split("/part-")[1].split("?")[0])
                with mutex:
                    puts.append((number, url, body))
                self.assertNotIn("Authorization", headers)
                if number == 1 and "signature=1" in url:
                    raise APIError(403, "HTTP_ERROR")
                return b"", {"ETag": "etag-" + str(number)}
            if url.endswith("/parts"):
                number = payload["part_numbers"][0]
                with mutex:
                    counters[number] += 1
                    signed.append(number)
                    generation = counters[number]
                return json.dumps({"upload_id": upload_id, "items": [{"part_no": number,
                    "expires_at": (datetime.now(timezone.utc) + timedelta(minutes=10)).isoformat(),
                    "url": f"https://storage.example.test/part-{number}?signature={generation}"}]}).encode(), {}
            if url.endswith("/complete"):
                completions.append(payload)
                state[0] = "verified"
                return json.dumps({"job_id": upload_id, "status": "queued"}).encode(), {}
            if url.endswith("/capture-receipts"):
                return json.dumps({"resource_type": "capture_receipts", "id": str(uuid.uuid4()),
                    "tenant_id": ids[0], "project_id": ids[1], "session_id": ids[2],
                    "device_id": ids[4], "sequence": "1", "content_sha": watch.journal.get(ids[4], 1).sha256,
                    "upload_id": upload_id}).encode(), {}
            return json.dumps({"id": upload_id, "project_id": ids[1], "collection_id": ids[3],
                "filename": "frame.jpg", "expected_size": 7, "part_bytes": 4, "parts_total": 2, "state": state[0]}).encode(), {}

        client._request = request
        now = [12]
        runner = Runner(Transfer(watch, client), clock=lambda: now[0])
        with patch("studio_agent.runner.random.uniform", return_value=0):
            self.assertEqual(runner.tick(), 1)
        failed = self.journal.get(ids[4], 1)
        self.assertEqual((failed.state, failed.upload_id, failed.retries), ("uploading", upload_id, 1))
        self.assertEqual(self.journal.completed_parts(ids[4], 1), [{"part_no": 2, "etag": "etag-2"}])
        self.journal.close()
        self.journal = Journal(self.path)
        self.journal.recover(ids[4])
        watch.journal = self.journal
        now[0] = 1000
        runner = Runner(Transfer(watch, client), clock=lambda: now[0])
        self.assertEqual(runner.tick(), 1)
        self.assertEqual(counters, {1: 2, 2: 1})
        self.assertEqual(sum(number == 2 for number, _, _ in puts), 1)
        self.assertEqual(completions, [{"parts": [{"part_no": 1, "etag": "etag-1"},
            {"part_no": 2, "etag": "etag-2"}], "expected_size": 7}])
        now[0] = 1006
        self.assertEqual(runner.tick(), 1)
        self.assertEqual(self.journal.get(ids[4], 1).state, "done")
        self.assertEqual(client.token, "fixture-device-token")


if __name__ == "__main__":
    unittest.main()
