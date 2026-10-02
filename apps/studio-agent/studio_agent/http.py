"""Capture HTTP transport. Device credentials never accompany storage PUTs."""

from __future__ import annotations

import base64
import json
import time
import ssl
import threading
import urllib.error
import urllib.parse
import urllib.request
import uuid
from collections.abc import Callable
from pathlib import Path

from .journal import Entry


class APIError(Exception):
    def __init__(self, status: int, code: str):
        super().__init__(f"capture API {status}: {code}")
        self.status, self.code = status, code


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def origin(value: str, development: bool = False) -> str:
    parsed = urllib.parse.urlsplit(value)
    if parsed.username or parsed.password or parsed.query or parsed.fragment or parsed.path not in ("", "/"):
        raise ValueError("expected an origin")
    if not parsed.hostname or not value.isascii() or parsed.scheme not in ("https", "http"):
        raise ValueError("invalid origin")
    if parsed.scheme != "https" and not (development and parsed.hostname in ("localhost", "127.0.0.1", "::1")):
        raise ValueError("capture transport requires HTTPS")
    return urllib.parse.urlunsplit((parsed.scheme, parsed.netloc, "", "", ""))


class HTTPClient:
    def __init__(self, api_origin: str, tenant_id: str, project_id: str,
                 session_id: str, collection_id: str, device_id: str,
                 sign: Callable[[bytes], bytes], storage_origins: list[str],
                 *, server_origin: str | None = None, development: bool = False,
                 timeout: float = 30, ca_bundle: str | None = None):
        self.origin = origin(api_origin, development)
        self.server_origin = origin(server_origin or api_origin, development)
        self.storage_origins = {origin(item, development) for item in storage_origins}
        if not self.storage_origins or not 0 < timeout <= 120:
            raise ValueError("storage origins and bounded timeout are required")
        self.tenant_id, self.project_id, self.session_id, self.collection_id, self.device_id = (
            str(uuid.UUID(item)) for item in (tenant_id, project_id, session_id, collection_id, device_id))
        self.sign, self.timeout = sign, timeout
        context = ssl.create_default_context(cafile=ca_bundle)
        self.opener = urllib.request.build_opener(NoRedirect(), urllib.request.HTTPSHandler(context=context))
        self.token, self.token_until = None, 0.0
        self.authentication_lock = threading.RLock()

    def _request(self, method: str, url: str, body: bytes | None, headers: dict) -> tuple[bytes, object]:
        request = urllib.request.Request(url, data=body, headers=headers, method=method)
        try:
            with self.opener.open(request, timeout=self.timeout) as response:
                content = response.read(1048577)
                if len(content) > 1048576:
                    raise ValueError("capture response exceeds limit")
                return content, response.headers
        except urllib.error.HTTPError as error:
            code = "HTTP_ERROR"
            try:
                payload = json.loads(error.read(4096))
                candidate = payload.get("code")
                if isinstance(candidate, str) and len(candidate) <= 100 and candidate.replace("_", "").isalnum():
                    code = candidate
            except (ValueError, AttributeError):
                pass
            finally:
                error.close()
            raise APIError(error.code, code) from None
        except urllib.error.URLError:
            raise ConnectionError("capture transport unavailable") from None

    def _json(self, method: str, path: str, payload: dict | None = None,
              *, key: str | None = None, authenticated: bool = True) -> dict:
        headers = {"Content-Type": "application/json"}
        if key:
            headers["Idempotency-Key"] = key
        if authenticated:
            with self.authentication_lock:
                self._authenticate()
                headers["Authorization"] = "Bearer " + self.token
        body = None if payload is None else json.dumps(payload, separators=(",", ":"), allow_nan=False).encode()
        try:
            content, _ = self._request(method, self.origin + "/api/v1/tenants/" + self.tenant_id + path, body, headers)
        except APIError as error:
            if authenticated and error.status == 401:
                with self.authentication_lock:
                    self.token, self.token_until = None, 0.0
            raise
        result = json.loads(content)
        if not isinstance(result, dict):
            raise ValueError("invalid capture response")
        return result

    def _authenticate(self) -> None:
        with self.authentication_lock:
            self._authenticate_locked()

    def _authenticate_locked(self) -> None:
        if self.token and time.monotonic() < self.token_until:
            return
        path = "/capture-devices/" + self.device_id
        challenge = self._json("POST", path + "/challenges", {"device_id": self.device_id}, authenticated=False)
        challenge_id = str(uuid.UUID(challenge["challenge_id"]))
        nonce = challenge["nonce"]
        decoded = base64.urlsafe_b64decode(nonce + "=")
        if len(decoded) != 32 or base64.urlsafe_b64encode(decoded).rstrip(b"=").decode() != nonce:
            raise ValueError("invalid challenge nonce")
        epoch = int(challenge["credential_epoch"])
        if not 1 <= epoch <= 9007199254740991:
            raise ValueError("invalid credential epoch")
        payload = json.dumps({"device_id": self.device_id, "challenge_id": challenge_id,
                              "nonce": nonce, "epoch": epoch, "server_origin": self.server_origin},
                             sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()
        signature = self.sign(payload)
        if len(signature) != 64:
            raise ValueError("invalid Ed25519 signature length")
        result = self._json("POST", path + "/token", {"challenge_id": challenge_id,
            "signature": base64.urlsafe_b64encode(signature).rstrip(b"=").decode()}, authenticated=False)
        if not isinstance(result.get("access_token"), str) or not 30 < result.get("expires_in", 0) <= 900:
            raise ValueError("invalid capture token response")
        if not result["access_token"] or len(result["access_token"]) > 8192 or any(c.isspace() for c in result["access_token"]):
            raise ValueError("invalid capture access token")
        self.token = result["access_token"]
        self.token_until = time.monotonic() + result["expires_in"] - 30

    def begin(self, entry: Entry, key: str) -> dict:
        if entry.device_id != self.device_id:
            raise ValueError("capture device mismatch")
        return self._json("POST", "/capture-uploads", {"session_id": self.session_id,
            "project_id": self.project_id, "collection_id": self.collection_id,
            "filename": Path(entry.relative_path).name, "bytes": entry.size,
            "format": Path(entry.relative_path).suffix.lower().lstrip("."), "sha256": entry.sha256}, key=key)

    def get(self, upload_id: str) -> dict:
        return self._json("GET", "/capture-uploads/" + str(uuid.UUID(upload_id)))

    def upload_part(self, upload_id: str, part_number: int, data: bytes) -> str:
        upload_id = str(uuid.UUID(upload_id))
        response = self._json("POST", "/capture-uploads/" + upload_id + "/parts", {"part_numbers": [part_number]})
        items = response.get("items", [])
        if len(items) != 1 or items[0].get("part_no") != part_number:
            raise ValueError("invalid signed part response")
        url = items[0]["url"]
        parsed = urllib.parse.urlsplit(url)
        storage = origin(urllib.parse.urlunsplit((parsed.scheme, parsed.netloc, "", "", "")),
                         self.origin.startswith("http://"))
        if storage not in self.storage_origins or parsed.username or parsed.password or parsed.fragment:
            raise ValueError("untrusted storage URL")
        _, headers = self._request("PUT", url, data, {"Content-Type": "application/octet-stream"})
        etag = headers.get("ETag")
        if not etag or len(etag) > 256 or any(c in etag for c in "\r\n"):
            raise ValueError("storage omitted valid ETag")
        return etag

    def complete(self, upload_id: str, parts: list[dict], size: int, key: str) -> None:
        self._json("POST", "/capture-uploads/" + str(uuid.UUID(upload_id)) + "/complete",
                   {"parts": parts, "expected_size": size}, key=key)

    def receipt(self, entry: Entry, key: str) -> dict:
        response = self._json("POST", "/shoot-sessions/" + self.session_id + "/capture-receipts",
            {"device_id": self.device_id, "sequence": str(entry.sequence), "content_sha": entry.sha256,
             "upload_id": entry.upload_id, "captured_at": None}, key=key)
        expected = {"resource_type": "capture_receipts", "tenant_id": self.tenant_id,
                    "project_id": self.project_id, "session_id": self.session_id,
                    "device_id": self.device_id, "sequence": str(entry.sequence),
                    "content_sha": entry.sha256, "upload_id": entry.upload_id}
        if any(response.get(field) != value for field, value in expected.items()):
            raise ValueError("capture receipt scope does not match admitted transfer")
        if not isinstance(response.get("id"), str) or str(uuid.UUID(response["id"])) != response["id"]:
            raise ValueError("invalid capture receipt identity")
        return response
