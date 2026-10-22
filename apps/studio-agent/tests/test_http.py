import base64
import http.client
from dataclasses import replace
import io
import json
import threading
from concurrent.futures import ThreadPoolExecutor
import unittest
import urllib.error
import uuid
from datetime import datetime, timezone, timedelta
from unittest.mock import patch

from studio_agent.http import HTTPClient, origin, APIError
from studio_agent.journal import Entry
from studio_agent.raw_policy import canonical_raw_policy


class Response(io.BytesIO):
    def __init__(self, payload, headers=None):
        super().__init__(json.dumps(payload).encode())
        self.headers = headers or {}


class HTTPTransportTest(unittest.TestCase):
    def setUp(self):
        self.ids = [str(uuid.uuid4()) for _ in range(5)]
        self.challenge_id = str(uuid.uuid4())
        self.upload_id = str(uuid.uuid4())
        self.nonce = base64.urlsafe_b64encode(bytes(32)).rstrip(b"=").decode()
        self.signed = []
        self.client = HTTPClient("https://api.example.test", *self.ids,
            lambda payload: self.signed.append(payload) or bytes(64), ["https://storage.example.test"])
        self.calls = []
        self.client.opener = self

    def open(self, request, timeout):
        self.calls.append(request)
        if request.full_url.endswith("/challenges"):
            return Response({"challenge_id": self.challenge_id, "nonce": self.nonce, "credential_epoch": "2"})
        if request.full_url.endswith("/token"):
            return Response({"access_token": "capture-token", "expires_in": 900})
        if request.full_url.endswith("/capture-receipts"):
            response = {"resource_type": "capture_receipts", "id": self.challenge_id,
                "tenant_id": self.ids[0], "project_id": self.ids[1],
                "session_id": self.ids[2], "device_id": self.ids[4],
                "sequence": "1", "content_sha": "a"*64, "upload_id": self.upload_id}
            response.update(getattr(self, "receipt_override", {}))
            return Response(response)
        if request.full_url.endswith("/parts"):
            return Response({"upload_id": self.upload_id, "items": [{"part_no": 1, "url": "https://storage.example.test/object?signature=secret", "expires_at": (datetime.now(timezone.utc)+timedelta(minutes=10)).isoformat()}]})
        if request.full_url.endswith("/complete"):
            response = {"job_id": self.upload_id, "status": "queued"}
            response.update(getattr(self, "completion_override", {}))
            return Response(response)
        if request.get_method() == "PUT":
            return Response({}, {"ETag": '"part-etag"'})
        response = {"id": self.upload_id, "state": "uploading", "part_bytes": 16777216,
                    "parts_total": 1, "project_id": self.ids[1],
                    "collection_id": self.ids[3], "expected_size": 3}
        if request.get_method() == "POST":
            response["filename"] = json.loads(request.data)["filename"].strip()
        response.update(getattr(self, "upload_override", {}))
        return Response(response)

    def test_receipt_invalid_local_scope_never_authenticates_or_sends(self):
        invalid = [
            (str(uuid.uuid4()), 1, "a" * 64, self.upload_id),
            (self.ids[4], True, "a" * 64, self.upload_id),
            (self.ids[4], 1.0, "a" * 64, self.upload_id),
            (self.ids[4], 0, "a" * 64, self.upload_id),
            (self.ids[4], 9223372036854775808, "a" * 64, self.upload_id),
            (self.ids[4], 1, None, self.upload_id),
            (self.ids[4], 1, "A" * 64, self.upload_id),
            (self.ids[4], 1, "a" * 63, self.upload_id),
            (self.ids[4], 1, "a" * 64, None),
            (self.ids[4], 1, "a" * 64, str(uuid.UUID(int=0))),
        ]
        for device, sequence, digest, upload in invalid:
            with self.subTest(device=device, sequence=sequence, digest=digest, upload=upload):
                entry = Entry(device, sequence, "frame.jpg", 3, 1, "receipt",
                              digest, upload, 0, None)
                with self.assertRaises(ValueError):
                    self.client.receipt(entry, "receipt-key")
                self.assertEqual(self.calls, [])
                self.assertEqual(self.signed, [])
        valid = Entry(self.ids[4], 1, "frame.jpg", 3, 1, "receipt",
                      "a" * 64, self.upload_id, 0, None)
        self.assertEqual(self.client.receipt(valid, "receipt-key")["upload_id"], self.upload_id)

    def test_begin_invalid_source_never_authenticates_or_creates_session(self):
        valid = Entry(self.ids[4], 1, "camera/frame.JPG", 3, 1, "hashed", "a" * 64, None, 0, None)
        changes = [{"sequence": True}, {"sequence": 0}, {"sequence": 2**63},
                   {"size": True}, {"size": 3.0}, {"size": 0}, {"size": 2**53},
                   {"sha256": None}, {"sha256": "A" * 64},
                   {"relative_path": None}, {"relative_path": ""},
                   {"relative_path": "/camera/frame.jpg"},
                   {"relative_path": "../frame.jpg"}, {"relative_path": "frame\x00.jpg"}]
        for change in changes:
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.client.begin(replace(valid, **change), "begin-key")
            self.assertEqual(self.calls, [])
            self.assertEqual(self.signed, [])
        self.assertEqual(self.client.begin(valid, "begin-key")["id"], self.upload_id)

    def test_raw_begin_uses_frozen_policy_and_rejects_substitution_before_io(self):
        policy = {"schema_version": 1, "demosaic": "ahd", "wb_source": "camera",
                  "exposure_ev": 0, "highlight_policy": "blend",
                  "decoder_version": "rawpy-0.27.1/libraw-0.21.4"}
        frozen = canonical_raw_policy(policy)
        entry = Entry(self.ids[4], 1, "camera/frame.NEF", 3, 1, "hashed",
                      "a" * 64, None, 0, None, frozen)
        self.client.begin(entry, "same-begin-key")
        request = json.loads(self.calls[-1].data)
        self.assertEqual(request["raw_development"], policy)
        self.assertEqual(self.calls[-1].get_header("Idempotency-key"), "same-begin-key")
        before = len(self.calls)
        for invalid in (replace(entry, raw_policy_json=frozen + " "),
                        replace(entry, relative_path="frame.jpg")):
            with self.assertRaises(ValueError):
                self.client.begin(invalid, "same-begin-key")
            self.assertEqual(len(self.calls), before)

    def test_signature_wire_shape_and_storage_credential_separation(self):
        entry = Entry(self.ids[4], 1, "camera/frame.JPG", 3, 1, "hashed", "a" * 64, None, 0, None)
        self.client.begin(entry, "begin-key")
        self.assertEqual(json.loads(self.signed[0]), {"device_id": self.ids[4],
            "challenge_id": self.challenge_id, "nonce": self.nonce, "epoch": 2,
            "server_origin": "https://api.example.test"})
        self.assertEqual(self.signed[0], json.dumps(json.loads(self.signed[0]), sort_keys=True,
            separators=(",", ":")).encode())
        begin = self.calls[-1]
        self.assertEqual(begin.get_header("Authorization"), "Bearer capture-token")
        self.assertEqual(json.loads(begin.data)["format"], "jpg")
        self.assertEqual(self.client.upload_part(self.upload_id, 1, b"abc"), '"part-etag"')
        put = self.calls[-1]
        self.assertIsNone(put.get_header("Authorization"))
        self.assertIsNone(put.get_header("Idempotency-key"))
        self.assertEqual(put.data, b"abc")
        self.assertEqual(sum(call.full_url.endswith("/challenges") for call in self.calls), 1)

    def test_upload_responses_validate_scope_before_journal_admission(self):
        entry = Entry(self.ids[4], 1, "frame.jpg", 3, 1, "hashed", "a"*64, None, 0, None)
        for change in ({"project_id": str(uuid.uuid4())},
                       {"collection_id": None}, {"id": "bad"},
                       {"part_bytes": True}, {"parts_total": 1.0},
                       {"part_bytes": 0}, {"parts_total": 10001}):
            self.upload_override = change
            with self.subTest(change=change):
                with self.assertRaises(ValueError):
                    self.client.begin(entry, "same-begin-key")
                with self.assertRaises(ValueError):
                    self.client.get(self.upload_id)
        for change in ({"expected_size": 4}, {"expected_size": 3.0}, {"parts_total": 2}):
            self.upload_override = change
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.client.begin(entry, "same-begin-key")
        self.upload_override = {"id": str(uuid.uuid4())}
        with self.assertRaises(ValueError):
            self.client.get(self.upload_id)

    def test_receipt_validates_all_admission_bindings(self):
        entry = Entry(self.ids[4], 1, "frame.jpg", 3, 1, "receipt", "a"*64, self.upload_id, 0, None)
        self.assertEqual(self.client.receipt(entry, "receipt-key")["upload_id"], self.upload_id)
        for field in ("tenant_id", "project_id", "session_id", "device_id", "upload_id", "sequence", "content_sha", "resource_type", "id"):
            self.receipt_override = {field: "wrong"}
            with self.subTest(field=field), self.assertRaises(ValueError):
                self.client.receipt(entry, "receipt-key")

    def test_origin_rejects_parser_normalization_and_invalid_ports(self):
        for value in (None, b"https://api.example.test", " https://api.example.test",
                      "https://api.\nexample.test", "https://api.example.test\t",
                      "https://api.example.test:0", "https://api.example.test:65536",
                      "https://api.example.test:invalid", "https://api.example.test\x7f"):
            with self.subTest(value=value), self.assertRaises(ValueError):
                origin(value)
        self.assertEqual(origin("https://api.example.test:443/"), "https://api.example.test:443")

    def test_direct_client_refuses_unbounded_or_mistyped_transport_config(self):
        for timeout in (True, "30", None, float("nan"), float("inf"), 0, 121):
            with self.subTest(timeout=timeout), self.assertRaises(ValueError):
                HTTPClient("https://api.example.test", *self.ids, lambda payload: bytes(64),
                           ["https://storage.example.test"], timeout=timeout)
        for storage in (None, "https://storage.example.test", [],
                        ["https://storage.example.test"] * 33):
            with self.subTest(storage=storage), self.assertRaises(ValueError):
                HTTPClient("https://api.example.test", *self.ids, lambda payload: bytes(64), storage)

    def test_signed_url_control_characters_never_reach_storage_put(self):
        for url in ("https://storage.example.test/ob\nject?signature=x",
                    "https://storage.example.test/object?signature=x\t",
                    "https://storage.example.test/object with space?signature=x",
                    "https://storage.example.test/object?signature=\x7f"):
            with self.subTest(url=url):
                response = {"upload_id": self.upload_id, "items": [{"part_no": 1, "url": url, "expires_at": (datetime.now(timezone.utc)+timedelta(minutes=10)).isoformat()}]}
                with patch.object(self.client, "_json", return_value=response), \
                     patch.object(self.client, "_request") as request:
                    with self.assertRaises(ValueError):
                        self.client.upload_part(self.upload_id, 1, b"abc")
                    request.assert_not_called()

    def test_origin_configuration_rejects_insecure_and_credential_urls(self):
        for value in ["http://api.example.test", "https://user:pass@api.example.test", "https://api.example.test/path", "https://api.example.test?token=x"]:
            with self.assertRaises(ValueError):
                origin(value)
        self.assertEqual(origin("http://127.0.0.1:8080", True), "http://127.0.0.1:8080")


    def test_challenge_refuses_noncanonical_epoch_and_nonce_before_signing(self):
        valid = {"challenge_id": self.challenge_id, "nonce": self.nonce, "credential_epoch": "2"}
        changes = [{"credential_epoch": value} for value in (True, 2.5, 2, "02", " 2", "0", "9007199254740992")]
        changes += [{"nonce": None}, {"nonce": "a" * 44}, {"challenge_id": str(uuid.UUID(int=0))}]
        for change in changes:
            with self.subTest(change=change):
                self.client._json = lambda *args, **kwargs: {**valid, **change}
                with self.assertRaises(ValueError):
                    self.client._authenticate()
                self.assertEqual(self.signed, [])
                self.assertIsNone(self.client.token)

    def test_signed_parts_refuse_malformed_nested_shape_before_put(self):
        for items in (None, {}, [None], ["part"], [{"part_no": True, "url": "https://storage.example.test/object"}],
                      [{"part_no": 1, "url": None}], [{"part_no": 1, "url": "x" * 8193}]):
            with self.subTest(items=items):
                self.client._json = lambda *args, value=items, **kwargs: {"upload_id": self.upload_id, "items": value}
                with self.assertRaises(ValueError):
                    self.client.upload_part(self.upload_id, 1, b"abc")
                self.assertEqual(self.calls, [])


    def test_unauthorized_response_reauthenticates_once_with_rotated_epoch(self):
        original = self.client._request
        probes, challenges = [], []
        def request(method, url, body, headers):
            if url.endswith("/challenges"):
                epoch = str(2 + len(challenges))
                challenges.append(epoch)
                return json.dumps({"challenge_id": self.challenge_id, "nonce": self.nonce,
                                   "credential_epoch": epoch}).encode(), {}
            if url.endswith("/probe"):
                probes.append(dict(headers))
                if len(probes) == 1:
                    raise APIError(401, "INVALID_CREDENTIALS")
                return b'{"ok":true}', {}
            return original(method, url, body, headers)
        self.client._request = request
        self.assertEqual(self.client._json("POST", "/probe", {"capture": "same"},
                                            key="capture/retry"), {"ok": True})
        self.assertEqual(self.client.token, "capture-token")
        self.assertEqual(challenges, ["2", "3"])
        self.assertEqual([json.loads(payload)["epoch"] for payload in self.signed], [2, 3])
        self.assertEqual(len(probes), 2)
        self.assertEqual([item["Idempotency-Key"] for item in probes],
                         ["capture/retry", "capture/retry"])

    def test_late_unauthorized_parallel_request_preserves_refreshed_token(self):
        old_in_flight = threading.Event()
        new_accepted = threading.Event()
        authentication_count = 0

        def authenticate():
            nonlocal authentication_count
            if self.client.token == "new-token":
                return
            authentication_count += 1
            self.client.token = "old-token" if authentication_count == 1 else "new-token"
            self.client.token_until = 100 + authentication_count

        def request(method, url, body, headers):
            if headers["Authorization"] == "Bearer old-token":
                old_in_flight.set()
                if not new_accepted.wait(2):
                    raise AssertionError("refreshed request did not finish")
                raise APIError(401, "INVALID_CREDENTIALS")
            self.assertEqual(headers["Authorization"], "Bearer new-token")
            new_accepted.set()
            return b'{"ok":true}', {}

        with patch.object(self.client, "_authenticate", side_effect=authenticate), \
             patch.object(self.client, "_request", side_effect=request), \
             ThreadPoolExecutor(max_workers=2) as pool:
            old = pool.submit(self.client._json, "GET", "/old-probe")
            self.assertTrue(old_in_flight.wait(2))
            new = pool.submit(self.client._json, "GET", "/new-probe")
            self.assertEqual(new.result(timeout=2), {"ok": True})
            self.assertEqual(old.result(timeout=2), {"ok": True})
        self.assertEqual(self.client.token, "new-token")
        self.assertEqual(self.client.token_until, 102)

    def test_second_unauthorized_response_stops_after_one_refresh(self):
        requests = []
        original = self.client._request
        def reject(method, url, body, headers):
            if url.endswith('/probe'):
                requests.append(headers['Authorization'])
                raise APIError(401, 'INVALID_CREDENTIALS')
            return original(method, url, body, headers)
        with patch.object(self.client, '_request', side_effect=reject):
            with self.assertRaises(APIError) as failure:
                self.client._json('GET', '/probe')
        self.assertEqual(failure.exception.status, 401)
        self.assertEqual(requests, ['Bearer capture-token', 'Bearer capture-token'])
        self.assertIsNone(self.client.token)

    def test_truncated_response_is_retryable_and_response_is_closed(self):
        class TruncatedResponse(io.BytesIO):
            headers = {}

            def read(self, size=-1):
                raise http.client.IncompleteRead(b'{"accepted":', 10)

        response = TruncatedResponse()
        with patch.object(self, "open", return_value=response):
            with self.assertRaises(ConnectionError) as failure:
                self.client._request("POST", "https://api.example.test/test", b"{}", {})
        self.assertEqual(str(failure.exception), "capture transport unavailable")
        self.assertIsNone(failure.exception.__cause__)
        self.assertTrue(response.closed)

    def test_truncated_http_error_body_keeps_status_and_closes_stream(self):
        class TruncatedErrorBody(io.BytesIO):
            def read(self, size=-1):
                raise http.client.IncompleteRead(b"private-error", 9)

        for status in (401, 429, 503):
            with self.subTest(status=status):
                body = TruncatedErrorBody()
                error = urllib.error.HTTPError("https://api.example.test/test", status,
                                              "failure", {}, body)
                with patch.object(self, "open", side_effect=error):
                    with self.assertRaises(APIError) as failure:
                        self.client._request("POST", "https://api.example.test/test", b"{}", {})
                self.assertEqual(failure.exception.status, status)
                self.assertEqual(failure.exception.code, "HTTP_ERROR")
                self.assertNotIn("private-error", str(failure.exception))
                self.assertTrue(body.closed)

    def test_truncated_response_while_opening_is_retryable(self):
        with patch.object(self, "open", side_effect=http.client.IncompleteRead(b"private", 7)):
            with self.assertRaises(ConnectionError) as failure:
                self.client._request("POST", "https://api.example.test/test", b"{}", {})
        self.assertNotIn("private", str(failure.exception))
        self.assertEqual(self.client.token_until, 0.0)

    def test_invalid_bearer_bytes_are_rejected_before_cache_or_upload(self):
        for token in ("", "a" * 8193, "bad\x00token", "bad\x1ftoken",
                      "bad\x7ftoken", "bad token", "bad\ttoken", "токен"):
            with self.subTest(token=repr(token)):
                self.client.token, self.client.token_until = None, 0.0
                self.calls.clear()
                original_open = self.open

                def open_response(request, timeout):
                    if request.full_url.endswith("/token"):
                        self.calls.append(request)
                        return Response({"access_token": token, "expires_in": 900})
                    return original_open(request, timeout)

                with patch.object(self, "open", side_effect=open_response):
                    with self.assertRaises(ValueError):
                        self.client.get(self.upload_id)
                self.assertIsNone(self.client.token)
                self.assertEqual(self.client.token_until, 0.0)
                self.assertEqual(len(self.calls), 2)
                self.assertTrue(self.calls[0].full_url.endswith("/challenges"))
                self.assertTrue(self.calls[1].full_url.endswith("/token"))

    def test_token_lifetime_must_be_integer_and_no_invalid_token_is_cached(self):
        for expires in (True, 900.0, "900", 30, 901, None):
            with self.subTest(expires=expires):
                def response(method, path, *args, **kwargs):
                    if path.endswith("/challenges"):
                        return {"challenge_id": self.challenge_id, "nonce": self.nonce, "credential_epoch": "2"}
                    return {"access_token": "valid-looking-token", "expires_in": expires}
                self.client._json = response
                with self.assertRaises(ValueError):
                    self.client._authenticate()
                self.assertIsNone(self.client.token)


    def test_json_response_refuses_duplicate_fields_nonfinite_and_deep_nesting(self):
        invalid = [b'{"id":"first","id":"second"}',
                   b'{"items":[{"part_no":1,"part_no":2}]}',
                   b'{"value":NaN}', b'{"value":Infinity}', b'{"value":-Infinity}',
                   b'{"nested":' + b'[' * 2000 + b'0' + b']' * 2000 + b'}']
        for raw in invalid:
            with self.subTest(raw=raw[:80]):
                with patch.object(self.client, "_request", return_value=(raw, {})):
                    with self.assertRaises(ValueError):
                        self.client._json("GET", "/test", authenticated=False)
        self.assertIsNone(self.client.token)
        self.assertEqual(self.signed, [])

    def test_json_response_preserves_distinct_nested_objects(self):
        raw = b'{"items":[{"part_no":1},{"part_no":2}],"meta":{"count":2}}'
        with patch.object(self.client, "_request", return_value=(raw, {})):
            result = self.client._json("GET", "/test", authenticated=False)
        self.assertEqual(result, {"items": [{"part_no": 1}, {"part_no": 2}],
                                  "meta": {"count": 2}})


    def test_zero_configured_resource_ids_refuse_before_transport(self):
        zero = str(uuid.UUID(int=0))
        for index in range(5):
            ids = self.ids.copy()
            ids[index] = zero
            with self.subTest(index=index):
                with self.assertRaisesRegex(ValueError, "zero capture resource"):
                    HTTPClient("https://api.example.test", *ids,
                        lambda payload: self.fail("signed invalid scope"),
                        ["https://storage.example.test"])

    def test_zero_upload_id_refuses_before_authentication_or_network(self):
        zero = str(uuid.UUID(int=0))
        for action in (lambda: self.client.get(zero),
                       lambda: self.client.upload_part(zero, 1, b"abc"),
                       lambda: self.client.complete(zero, [], 3, "key")):
            with self.assertRaisesRegex(ValueError, "zero capture resource"):
                action()
        self.assertEqual(self.calls, [])
        self.assertEqual(self.signed, [])

    def test_zero_server_upload_and_receipt_ids_never_accept_admission(self):
        entry = Entry(self.ids[4], 1, "frame.jpg", 3, 1, "hashed", "a" * 64, None, 0, None)
        self.upload_override = {"id": str(uuid.UUID(int=0))}
        with self.assertRaises(ValueError):
            self.client.begin(entry, "begin-key")
        self.receipt_override = {"id": str(uuid.UUID(int=0))}
        receipt_entry = Entry(self.ids[4], 1, "frame.jpg", 3, 1, "receipt",
                              "a" * 64, self.upload_id, 0, None)
        with self.assertRaises(ValueError):
            self.client.receipt(receipt_entry, "receipt-key")


    def test_invalid_local_part_arguments_refuse_before_authentication(self):
        invalid = [(True, b"abc"), (1.0, b"abc"), (0, b"abc"), (10001, b"abc"),
                   (1, b""), (1, bytearray(b"abc")), (1, "abc"),
                   (1, b"x" * (16 * 1024**2 + 1))]
        for number, data in invalid:
            with self.subTest(number=number, kind=type(data).__name__, size=len(data)):
                with self.assertRaisesRegex(ValueError, "invalid capture multipart"):
                    self.client.upload_part(self.upload_id, number, data)
        self.assertEqual(self.calls, [])
        self.assertEqual(self.signed, [])
        self.assertIsNone(self.client.token)


    def test_invalid_completion_refuses_before_authentication(self):
        invalid = [(True, [{"part_no": 1, "etag": "a"}]), (0, []),
                   (3, []), (3, [None]), (3, [{"part_no": True, "etag": "a"}]),
                   (3, [{"part_no": 2, "etag": "a"}]),
                   (3, [{"part_no": 1, "etag": ""}]),
                   (3, [{"part_no": 1, "etag": "a\nb"}]),
                   (3, [{"part_no": 1, "etag": "a\x00b"}]),
                   (3, [{"part_no": 1, "etag": "a", "extra": 1}]),
                   (3, [{"part_no": 1, "etag": "a"}, {"part_no": 1, "etag": "b"}])]
        for size, parts in invalid:
            with self.subTest(size=size, parts=parts):
                with self.assertRaises(ValueError):
                    self.client.complete(self.upload_id, parts, size, "key")
        self.assertEqual(self.calls, [])
        self.assertEqual(self.signed, [])

    def test_completion_owns_parts_before_authentication(self):
        parts = [{"part_no": 1, "etag": "original"}]
        def authenticate():
            parts[0]["etag"] = "changed"
            parts.append({"part_no": 2, "etag": "late"})
            self.client.token = "fixture-token"
        with patch.object(self.client, "_authenticate", authenticate):
            self.client.complete(self.upload_id, parts, 3, "completion-key")
        request = self.calls[-1]
        self.assertEqual(json.loads(request.data), {
            "parts": [{"part_no": 1, "etag": "original"}], "expected_size": 3})
        self.assertEqual(request.get_header("Idempotency-key"), "completion-key")

    def test_completion_requires_exact_original_upload_queued_receipt(self):
        for changed in ({"job_id": str(uuid.uuid4())}, {"status": "verified"}, {"extra": True}):
            self.completion_override = changed
            with self.subTest(changed=changed), self.assertRaises(ValueError):
                self.client.complete(self.upload_id, [{"part_no": 1, "etag": "etag"}], 3, "stable-key")
        self.completion_override = {}
        self.client.complete(self.upload_id, [{"part_no": 1, "etag": "etag"}], 3, "stable-key")

    def test_begin_rejects_other_filename_and_unknown_remote_state(self):
        entry = Entry(self.ids[4], 1, "frame.jpg", 3, 1, "hashed", "a"*64, None, 0, None)
        for changed in ({"filename": "foreign.jpg"}, {"state": "success"}):
            self.upload_override = changed
            with self.subTest(changed=changed), self.assertRaises(ValueError):
                self.client.begin(entry, "stable-begin-key")


    def test_storage_auth_refusal_retries_without_invalidating_device_token(self):
        for status in (401, 403):
            with self.subTest(status=status):
                original = self.client._request
                def request(method, url, body, headers):
                    if method == "PUT":
                        raise APIError(status, "HTTP_ERROR")
                    return original(method, url, body, headers)
                with patch.object(self.client, "_request", request):
                    with self.assertRaisesRegex(ConnectionError, "storage authorization"):
                        self.client.upload_part(self.upload_id, 1, b"abc")
                self.assertEqual(self.client.token, "capture-token")

    def test_signed_url_api_revocation_remains_terminal_device_error(self):
        original = self.client._request
        def request(method, url, body, headers):
            if url.endswith("/parts"):
                raise APIError(403, "DEVICE_REVOKED")
            return original(method, url, body, headers)
        with patch.object(self.client, "_request", request):
            with self.assertRaises(APIError) as error:
                self.client.upload_part(self.upload_id, 1, b"abc")
        self.assertEqual(error.exception.status, 403)
        self.assertFalse(any(call.get_method() == "PUT" for call in self.calls))


    def test_signed_part_scope_and_expiry_refuse_before_storage_put(self):
        now = datetime.now(timezone.utc)
        for mode in ("foreign", "missing", "naive", "invalid", "far", "expired", "near"):
            with self.subTest(mode=mode):
                item = {"part_no": 1, "url": "https://storage.example.test/object?signature=opaque",
                        "expires_at": (now+timedelta(minutes=10)).isoformat()}
                response = {"upload_id": self.upload_id, "items": [item]}
                if mode == "foreign": response["upload_id"] = str(uuid.uuid4())
                if mode == "missing": item.pop("expires_at")
                if mode == "naive": item["expires_at"] = now.replace(tzinfo=None).isoformat()
                if mode == "invalid": item["expires_at"] = "not-a-time"
                if mode == "far": item["expires_at"] = (now+timedelta(hours=1)).isoformat()
                if mode == "expired": item["expires_at"] = (now-timedelta(seconds=1)).isoformat()
                if mode == "near": item["expires_at"] = (now+timedelta(seconds=3)).isoformat()
                error = ConnectionError if mode in ("expired", "near") else ValueError
                with patch.object(self.client, "_json", return_value=response), patch.object(self.client, "_request") as request:
                    with self.assertRaises(error): self.client.upload_part(self.upload_id, 1, b"abc")
                    request.assert_not_called()

    def test_signed_part_expiry_is_rechecked_before_storage_put(self):
        now = datetime.now(timezone.utc).timestamp()
        response = {"upload_id": self.upload_id, "items": [{"part_no": 1,
            "url": "https://storage.example.test/object?signature=opaque",
            "expires_at": datetime.fromtimestamp(now+60, timezone.utc).isoformat()}]}
        with patch.object(self.client, "_json", return_value=response), patch.object(self.client, "_request") as request, patch("studio_agent.http.time.time", side_effect=[now, now+58]):
            with self.assertRaises(ConnectionError): self.client.upload_part(self.upload_id, 1, b"abc")
            request.assert_not_called()


if __name__ == "__main__":
    unittest.main()
