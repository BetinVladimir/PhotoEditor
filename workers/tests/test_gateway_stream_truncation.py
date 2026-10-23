"""A truncated HTTP body is unavailable transport, not proven object corruption."""
import io
import hashlib
from uuid import uuid4

import pytest

from photo_worker.runtime.gateway import ArtifactStream, GatewayError
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.materialize import materialize_gateway, ArtifactTransferError
from photo_worker.runtime.scratch import ScratchSpace


class Connection:
    def __init__(self):
        self.closed = False

    def close(self):
        self.closed = True


@pytest.mark.parametrize('payload', [b'', b'partial'])
def test_early_eof_is_retryable_without_reopening_or_accepting_partial_body(payload):
    connection = Connection()
    response = io.BytesIO(payload)
    with ArtifactStream(response, connection, 16, lambda: None) as stream:
        if payload:
            assert stream.read(16) == payload
        with pytest.raises(GatewayError) as caught:
            stream.read(16)
        assert caught.value.code == 'DEPENDENCY_UNAVAILABLE'
        assert caught.value.retryable is True
        assert stream.size == 16
    assert response.closed and connection.closed


def test_complete_body_and_zero_byte_reads_remain_valid():
    connection = Connection()
    with ArtifactStream(io.BytesIO(b'whole'), connection, 5, lambda: None) as stream:
        assert stream.read(0) == b''
        assert stream.read(3) == b'who'
        assert stream.read(3) == b'le'
        assert stream.read(3) == b''
    assert connection.closed


def test_cancellation_before_read_is_not_mapped_to_dependency_retry():
    class Cancelled(Exception):
        pass

    def cancelled():
        raise Cancelled()

    with ArtifactStream(io.BytesIO(b'whole'), Connection(), 5, cancelled) as stream:
        with pytest.raises(Cancelled):
            stream.read(5)


@pytest.mark.parametrize('truncated', [True, False])
def test_materialization_discards_truncated_or_corrupt_bytes(tmp_path, truncated):
    expected = b'complete'
    payload = b'part' if truncated else b'corrupt!'
    ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(expected).hexdigest())
    connection = Connection()
    source = ArtifactStream(io.BytesIO(payload), connection, len(expected), lambda: None)
    calls = []

    def open_once(actual):
        calls.append(actual)
        return source

    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1024, min_free_bytes=0) as scratch:
        if truncated:
            with pytest.raises(GatewayError) as caught:
                materialize_gateway(ref, scratch, open_once, lambda: None)
            assert (caught.value.code, caught.value.retryable) == ('DEPENDENCY_UNAVAILABLE', True)
        else:
            with pytest.raises(ArtifactTransferError, match='ARTIFACT_HASH_MISMATCH'):
                materialize_gateway(ref, scratch, open_once, lambda: None)
        assert list(scratch.directory.iterdir()) == []
    assert calls == [ref] and source.closed and connection.closed
