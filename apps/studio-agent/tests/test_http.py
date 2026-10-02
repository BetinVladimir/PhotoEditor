import base64
import io
import json
import unittest
import uuid

from studio_agent.http import HTTPClient, origin
from studio_agent.journal import Entry


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
            return Response({"items": [{"part_no": 1, "url": "https://storage.example.test/object?signature=secret"}]})
        if request.get_method() == "PUT":
            return Response({}, {"ETag": '"part-etag"'})
        return Response({"id": self.upload_id, "state": "uploading", "part_bytes": 16777216, "parts_total": 1})

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

    def test_receipt_validates_all_admission_bindings(self):
        entry = Entry(self.ids[4], 1, "frame.jpg", 3, 1, "receipt", "a"*64, self.upload_id, 0, None)
        self.assertEqual(self.client.receipt(entry, "receipt-key")["upload_id"], self.upload_id)
        for field in ("tenant_id", "project_id", "session_id", "device_id", "upload_id", "sequence", "content_sha", "resource_type", "id"):
            self.receipt_override = {field: "wrong"}
            with self.subTest(field=field), self.assertRaises(ValueError):
                self.client.receipt(entry, "receipt-key")

    def test_origin_configuration_rejects_insecure_and_credential_urls(self):
        for value in ["http://api.example.test", "https://user:pass@api.example.test", "https://api.example.test/path", "https://api.example.test?token=x"]:
            with self.assertRaises(ValueError):
                origin(value)
        self.assertEqual(origin("http://127.0.0.1:8080", True), "http://127.0.0.1:8080")


if __name__ == "__main__":
    unittest.main()
