import hashlib
import io
from types import SimpleNamespace
from unittest.mock import Mock

import pytest

from photo_worker.operations.archive.build import _copy_entry
from photo_worker.runtime.executor import OperationFailure


def test_archive_copy_streams_exact_bytes_across_block_boundary():
    payload = b'a' * (1024 * 1024) + b'end'
    source, target = io.BytesIO(payload), io.BytesIO()
    active = Mock()
    digest = hashlib.sha256(payload).hexdigest()
    assert _copy_entry(SimpleNamespace(check_active=active), source, target, len(payload), digest) == digest
    assert target.getvalue() == payload
    assert source.tell() == len(payload)
    assert active.call_count >= 6


def test_cancel_during_source_read_prevents_target_write():
    lost = False
    failure = RuntimeError('lease lost during read')

    def active():
        if lost: raise failure

    def read(limit):
        nonlocal lost
        lost = True
        return b'abc'

    target = Mock()
    with pytest.raises(RuntimeError) as raised:
        _copy_entry(SimpleNamespace(check_active=active), SimpleNamespace(read=read), target,
                    3, hashlib.sha256(b'abc').hexdigest())
    assert raised.value is failure
    target.write.assert_not_called()


@pytest.mark.parametrize('written', [0, 2, None, True, 3.0, 4])
def test_archive_copy_refuses_short_or_invalid_write_result(written):
    target = Mock()
    target.write.return_value = written
    with pytest.raises(OperationFailure) as failure:
        _copy_entry(SimpleNamespace(check_active=lambda: None), io.BytesIO(b'abc'), target,
                    3, hashlib.sha256(b'abc').hexdigest())
    assert failure.value.error.code == 'INTERNAL_ERROR'
    target.write.assert_called_once_with(b'abc')


@pytest.mark.parametrize('payload,expected_bytes,digest', [
    (b'ab', 3, hashlib.sha256(b'abc').hexdigest()),
    (b'abcd', 3, hashlib.sha256(b'abc').hexdigest()),
    (b'abc', 3, 'a' * 64),
])
def test_archive_copy_refuses_truncated_growing_or_corrupt_source(payload, expected_bytes, digest):
    with pytest.raises(OperationFailure) as failure:
        _copy_entry(SimpleNamespace(check_active=lambda: None), io.BytesIO(payload), io.BytesIO(),
                    expected_bytes, digest)
    assert failure.value.error.code == 'ARTIFACT_HASH_MISMATCH'
