"""Exact-version MinIO reads for isolated restore verification.

The caller supplies a read-only operator client with explicit endpoint/TLS and
bounded transport timeouts. No credentials, endpoint, bucket or executable is
selected by an HTTP/job payload. This adapter performs no write/delete/repair.
"""
from contextlib import contextmanager
from .restore_inventory import RestoreObject, _validate
from .restore_authority import require_restore_active


class _RestoreStream:
    def __init__(self, response, check_active):
        self.response, self.check_active = response, check_active

    def read(self, size):
        if type(size) is not int or not 1 <= size <= 1 << 20:
            raise ValueError('RESTORE_READ_LIMIT')
        require_restore_active(self.check_active)
        data = self.response.read(size, decode_content=False)
        require_restore_active(self.check_active)
        if not isinstance(data, bytes) or len(data) > size:
            raise ValueError('RESTORE_OBJECT_STREAM_INVALID')
        return data


class MinioRestoreObjects:
    def __init__(self, client, buckets, *, check_active):
        if (not callable(getattr(client, 'get_object', None))
                or not callable(check_active) or not isinstance(buckets, (list, tuple))
                or not 1 <= len(buckets) <= 256
                or any(not isinstance(b, str) or not 3 <= len(b) <= 63 for b in buckets)
                or len(set(buckets)) != len(buckets)):
            raise ValueError('RESTORE_OBJECT_INSTALLATION')
        self.client, self.buckets = client, frozenset(buckets)
        self.check_active = check_active

    @contextmanager
    def open_version(self, ref: RestoreObject):
        require_restore_active(self.check_active)
        _validate(ref)
        if ref.bucket not in self.buckets:
            raise ValueError('RESTORE_BUCKET_NOT_INSTALLED')
        response = self.client.get_object(ref.bucket, ref.key, version_id=ref.version)
        try:
            require_restore_active(self.check_active)
            # Require exact returned version, not merely a request parameter.
            # Reject transparent content transformation before SHA verification.
            if (response.status != 200
                    or response.headers.get('x-amz-version-id') != ref.version
                    or response.headers.get('content-length') != str(ref.size)
                    or response.headers.get('content-encoding') not in (None, 'identity')):
                raise ValueError('RESTORE_OBJECT_RESPONSE_MISMATCH')
            yield _RestoreStream(response, self.check_active)
        finally:
            try:
                response.close()
            finally:
                response.release_conn()
