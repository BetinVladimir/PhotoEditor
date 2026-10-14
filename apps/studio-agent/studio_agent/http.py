"""Capture HTTP transport. Device credentials never accompany storage PUTs."""

from __future__ import annotations

import base64
import http.client
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
from datetime import datetime

from .journal import Entry
from .raw_policy import frozen_raw_policy


class APIError(Exception):
    def __init__(self, status: int, code: str):
        super().__init__(f"capture API {status}: {code}")
        self.status, self.code = status, code


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def origin(value: str, development: bool = False) -> str:
    if (not isinstance(value, str) or not value or not value.isascii()
            or any(ord(c) <= 32 or ord(c) == 127 for c in value)
            or type(development) is not bool):
        raise ValueError("invalid origin")
    parsed = urllib.parse.urlsplit(value)
    # Accessing port validates its spelling and range; urlsplit alone does not.
    if parsed.port is not None and parsed.port == 0:
        raise ValueError("invalid origin port")
    if parsed.username or parsed.password or parsed.query or parsed.fragment or parsed.path not in ("", "/"):
        raise ValueError("expected an origin")
    if not parsed.hostname or not value.isascii() or parsed.scheme not in ("https", "http"):
        raise ValueError("invalid origin")
    if parsed.scheme != "https" and not (development and parsed.hostname in ("localhost", "127.0.0.1", "::1")):
        raise ValueError("capture transport requires HTTPS")
    return urllib.parse.urlunsplit((parsed.scheme, parsed.netloc, "", "", ""))


def _resource_id(value: str) -> str:
    if not isinstance(value, str):
        raise ValueError("invalid capture resource identity")
    identity = uuid.UUID(value)
    if identity.int == 0:
        raise ValueError("zero capture resource identity")
    return str(identity)


def _response_object(raw: bytes) -> dict:
    def unique_fields(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("duplicate capture response field")
            result[key] = value
        return result

    def invalid_constant(value):
        raise ValueError("nonfinite capture response number")

    try:
        result = json.loads(raw, object_pairs_hook=unique_fields,
                            parse_constant=invalid_constant)
    except RecursionError:
        raise ValueError("capture response nesting exceeds limit") from None
    if not isinstance(result, dict):
        raise ValueError("invalid capture response")
    return result


class HTTPClient:
    def __init__(self, api_origin: str, tenant_id: str, project_id: str,
                 session_id: str, collection_id: str, device_id: str,
                 sign: Callable[[bytes], bytes], storage_origins: list[str],
                 *, server_origin: str | None = None, development: bool = False,
                 timeout: float = 30, ca_bundle: str | None = None):
        if (not isinstance(storage_origins, list) or not 1 <= len(storage_origins) <= 32
                or type(timeout) not in (int, float) or not 0 < timeout <= 120):
            raise ValueError("storage origins and bounded timeout are required")
        self.origin = origin(api_origin, development)
        self.server_origin = origin(server_origin or api_origin, development)
        self.storage_origins = {origin(item, development) for item in storage_origins}
        if not self.storage_origins or not 0 < timeout <= 120:
            raise ValueError("storage origins and bounded timeout are required")
        self.tenant_id, self.project_id, self.session_id, self.collection_id, self.device_id = (
            _resource_id(item) for item in (tenant_id, project_id, session_id, collection_id, device_id))
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
            except (ValueError, AttributeError, http.client.IncompleteRead):
                pass
            finally:
                error.close()
            raise APIError(error.code, code) from None
        except (urllib.error.URLError, http.client.IncompleteRead):
            # A truncated response has an unknown server outcome. Preserve the
            # journal and replay the existing idempotency key on the next tick.
            raise ConnectionError("capture transport unavailable") from None

    def _json(self, method: str, path: str, payload: dict | None = None,
              *, key: str | None = None, authenticated: bool = True) -> dict:
        headers = {"Content-Type": "application/json"}
        if key:
            headers["Idempotency-Key"] = key
        if authenticated:
            with self.authentication_lock:
                self._authenticate()
                request_token = self.token
                headers["Authorization"] = "Bearer " + request_token
        body = None if payload is None else json.dumps(payload, separators=(",", ":"), allow_nan=False).encode()
        try:
            content, _ = self._request(method, self.origin + "/api/v1/tenants/" + self.tenant_id + path, body, headers)
        except APIError as error:
            if authenticated and error.status == 401:
                with self.authentication_lock:
                    # A sibling part may have refreshed credentials while this
                    # request was in flight. Invalidate only its own token.
                    if self.token == request_token:
                        self.token, self.token_until = None, 0.0
                    self._authenticate()
                    headers["Authorization"] = "Bearer " + self.token
                    retry_token = self.token
                try:
                    content, _ = self._request(method, self.origin + "/api/v1/tenants/" + self.tenant_id + path, body, headers)
                except APIError as retry_error:
                    if retry_error.status == 401:
                        with self.authentication_lock:
                            if self.token == retry_token:
                                self.token, self.token_until = None, 0.0
                    raise
            else:
                raise
        return _response_object(content)

    def _authenticate(self) -> None:
        with self.authentication_lock:
            self._authenticate_locked()

    def _authenticate_locked(self) -> None:
        if self.token and time.monotonic() < self.token_until:
            return
        path = "/capture-devices/" + self.device_id
        challenge = self._json("POST", path + "/challenges", {"device_id": self.device_id}, authenticated=False)
        identity = challenge.get("challenge_id")
        nonce = challenge.get("nonce")
        epoch_text = challenge.get("credential_epoch")
        if (not isinstance(identity, str) or len(identity) != 36
                or str(uuid.UUID(identity)) != identity or uuid.UUID(identity).int == 0
                or not isinstance(nonce, str) or len(nonce) != 43
                or not isinstance(epoch_text, str) or not 1 <= len(epoch_text) <= 16
                or not epoch_text.isascii() or not epoch_text.isdigit()
                or epoch_text.startswith("0")):
            raise ValueError("invalid capture challenge response")
        challenge_id = identity
        decoded = base64.urlsafe_b64decode(nonce + "=")
        if len(decoded) != 32 or base64.urlsafe_b64encode(decoded).rstrip(b"=").decode() != nonce:
            raise ValueError("invalid challenge nonce")
        epoch = int(epoch_text)
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
        if (not isinstance(result.get("access_token"), str)
                or type(result.get("expires_in")) is not int
                or not 30 < result["expires_in"] <= 900):
            raise ValueError("invalid capture token response")
        if (not result["access_token"] or len(result["access_token"]) > 8192
                or not result["access_token"].isascii()
                or any(ord(c) <= 32 or ord(c) == 127 for c in result["access_token"])):
            raise ValueError("invalid capture access token")
        self.token = result["access_token"]
        self.token_until = time.monotonic() + result["expires_in"] - 30

    def _validate_source(self, entry: Entry) -> None:
        if (entry.device_id != self.device_id
                or type(entry.sequence) is not int or not 1 <= entry.sequence <= 9223372036854775807
                or type(entry.size) is not int or not 1 <= entry.size <= 9007199254740991
                or not isinstance(entry.sha256, str) or len(entry.sha256) != 64
                or any(c not in "0123456789abcdef" for c in entry.sha256)):
            raise ValueError("invalid capture source")

    def begin(self, entry: Entry, key: str) -> dict:
        self._validate_source(entry)
        if (not isinstance(entry.relative_path, str) or not entry.relative_path
                or "\x00" in entry.relative_path
                or not Path(entry.relative_path).name
                or Path(entry.relative_path).is_absolute()
                or ".." in Path(entry.relative_path).parts):
            raise ValueError("invalid capture source path")
        frozen_policy = frozen_raw_policy(entry.raw_policy_json, entry.relative_path)
        body = {"session_id": self.session_id,
            "project_id": self.project_id, "collection_id": self.collection_id,
            "filename": Path(entry.relative_path).name, "bytes": entry.size,
            "format": Path(entry.relative_path).suffix.lower().lstrip("."), "sha256": entry.sha256}
        if frozen_policy is not None:
            body["raw_development"] = frozen_policy
        response = self._json("POST", "/capture-uploads", body, key=key)
        self._upload_response(response)
        if response.get("expected_size") != entry.size or type(response.get("expected_size")) is not int:
            raise ValueError("capture upload size mismatch")
        if response["parts_total"] != (entry.size + response["part_bytes"] - 1) // response["part_bytes"]:
            raise ValueError("capture upload layout mismatch")
        return response

    def _upload_response(self, response: dict, upload_id: str | None = None) -> None:
        identity = response.get("id")
        if (not isinstance(identity, str) or _resource_id(identity) != identity
                or upload_id is not None and identity != upload_id
                or response.get("project_id") != self.project_id
                or response.get("collection_id") != self.collection_id):
            raise ValueError("capture upload scope mismatch")
        if (type(response.get("part_bytes")) is not int
                or type(response.get("parts_total")) is not int
                or not 1 <= response["part_bytes"] <= 16 * 1024**2
                or not 1 <= response["parts_total"] <= 10000):
            raise ValueError("invalid capture upload layout")

    def get(self, upload_id: str) -> dict:
        upload_id = _resource_id(upload_id)
        response = self._json("GET", "/capture-uploads/" + upload_id)
        self._upload_response(response, upload_id)
        return response

    def upload_part(self, upload_id: str, part_number: int, data: bytes) -> str:
        upload_id = _resource_id(upload_id)
        if (type(part_number) is not int or not 1 <= part_number <= 10000
                or not isinstance(data, bytes) or not 1 <= len(data) <= 16 * 1024**2):
            raise ValueError("invalid capture multipart request")
        response = self._json("POST", "/capture-uploads/" + upload_id + "/parts", {"part_numbers": [part_number]})
        if response.get("upload_id") != upload_id:
            raise ValueError("signed part response upload mismatch")
        items = response.get("items", [])
        if (not isinstance(items, list) or len(items) != 1
                or not isinstance(items[0], dict)
                or type(items[0].get("part_no")) is not int
                or items[0]["part_no"] != part_number
                or not isinstance(items[0].get("url"), str)
                or not 1 <= len(items[0]["url"]) <= 8192):
            raise ValueError("invalid signed part response")
        expires = items[0].get("expires_at")
        if (not isinstance(expires, str) or not 1 <= len(expires) <= 64
                or "T" not in expires or any(ord(c) <= 32 for c in expires)):
            raise ValueError("invalid signed part expiry")
        try:
            deadline = datetime.fromisoformat(expires.replace("Z", "+00:00"))
            if deadline.tzinfo is None:
                raise ValueError("timezone required")
            expires_at = deadline.timestamp()
        except (ValueError, OverflowError):
            raise ValueError("invalid signed part expiry") from None
        def live_capability():
            now = time.time()
            if expires_at > now + 15 * 60:
                raise ValueError("signed part expiry exceeds horizon")
            if expires_at <= now + 5:
                raise ConnectionError("capture signed part expired; retry required")
        live_capability()
        url = items[0]["url"]
        if not url.isascii() or any(ord(c) <= 32 or ord(c) == 127 for c in url):
            raise ValueError("invalid signed storage URL")
        parsed = urllib.parse.urlsplit(url)
        storage = origin(urllib.parse.urlunsplit((parsed.scheme, parsed.netloc, "", "", "")),
                         self.origin.startswith("http://"))
        if storage not in self.storage_origins or parsed.username or parsed.password or parsed.fragment:
            raise ValueError("untrusted storage URL")
        live_capability()
        try:
            _, headers = self._request("PUT", url, data, {"Content-Type": "application/octet-stream"})
        except APIError as error:
            if error.status in (401, 403):
                # This is storage authority, not device-token revocation. A
                # retry requests a fresh signed URL for the same durable part.
                raise ConnectionError("capture storage authorization unavailable") from None
            raise
        etag = headers.get("ETag")
        if not etag or len(etag) > 256 or any(ord(c) < 32 or ord(c) == 127 for c in etag):
            raise ValueError("storage omitted valid ETag")
        return etag

    def complete(self, upload_id: str, parts: list[dict], size: int, key: str) -> None:
        upload_id = _resource_id(upload_id)
        if (type(size) is not int or not 1 <= size <= 9007199254740991
                or not isinstance(parts, list) or not 1 <= len(parts) <= 10000):
            raise ValueError("invalid capture completion request")
        owned = []
        for number, part in enumerate(parts, 1):
            if (not isinstance(part, dict) or set(part) != {"part_no", "etag"}
                    or type(part["part_no"]) is not int or part["part_no"] != number
                    or not isinstance(part["etag"], str) or not 1 <= len(part["etag"]) <= 256
                    or any(ord(c) < 32 or ord(c) == 127 for c in part["etag"])):
                raise ValueError("invalid capture completion part")
            owned.append({"part_no": number, "etag": part["etag"]})
        self._json("POST", "/capture-uploads/" + upload_id + "/complete",
                   {"parts": owned, "expected_size": size}, key=key)

    def receipt(self, entry: Entry, key: str) -> dict:
        self._validate_source(entry)
        if _resource_id(entry.upload_id) != entry.upload_id:
            raise ValueError("invalid capture receipt upload")
        response = self._json("POST", "/shoot-sessions/" + self.session_id + "/capture-receipts",
            {"device_id": self.device_id, "sequence": str(entry.sequence), "content_sha": entry.sha256,
             "upload_id": entry.upload_id, "captured_at": None}, key=key)
        expected = {"resource_type": "capture_receipts", "tenant_id": self.tenant_id,
                    "project_id": self.project_id, "session_id": self.session_id,
                    "device_id": self.device_id, "sequence": str(entry.sequence),
                    "content_sha": entry.sha256, "upload_id": entry.upload_id}
        if any(response.get(field) != value for field, value in expected.items()):
            raise ValueError("capture receipt scope does not match admitted transfer")
        if not isinstance(response.get("id"), str) or _resource_id(response["id"]) != response["id"]:
            raise ValueError("invalid capture receipt identity")
        return response
