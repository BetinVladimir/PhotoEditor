"""Metadata verification keeps transient IO separate from invalid output."""
from uuid import uuid4

import pytest

from photo_worker.v2.adoption import AdoptionInvalid, ArtifactRef
from photo_worker.v2.delivery_metadata_prepare_result import prepare_delivery_metadata_result


class Reader:
    def __init__(self, failure):
        self.failure = failure

    def evidence(self, _ref):
        raise self.failure


def prepare(reader, tmp_path):
    params = ArtifactRef(uuid4(), 'a' * 64)
    root = ArtifactRef(uuid4(), 'b' * 64)
    report = ArtifactRef(uuid4(), 'c' * 64)
    return prepare_delivery_metadata_result(reader, params, root, report, uuid4(),
        None, None, None, None, None, None, policy='copyright',
        scratch_root=tmp_path, max_bytes=1024, max_pixels=100)


def test_metadata_io_failure_remains_retryable(tmp_path):
    failure = OSError('scratch volume unavailable')
    with pytest.raises(OSError) as caught:
        prepare(Reader(failure), tmp_path)
    assert caught.value is failure


def test_metadata_invalid_value_remains_semantic_refusal(tmp_path):
    with pytest.raises(AdoptionInvalid, match='DELIVERY_METADATA_PHYSICAL_INVALID'):
        prepare(Reader(ValueError('invalid immutable document')), tmp_path)
