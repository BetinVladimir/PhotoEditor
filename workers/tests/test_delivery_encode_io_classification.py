"""Local IO failures must remain retryable during V30 physical verification."""
from uuid import uuid4

import pytest

from photo_worker.v2.adoption import AdoptionInvalid, ArtifactRef
from photo_worker.v2.delivery_encoded_prepare import prepare_delivery_encode


class Reader:
    def __init__(self, error):
        self.error = error

    def evidence(self, _ref):
        raise self.error


@pytest.mark.parametrize('failure', [OSError('disk unavailable'), OSError('object read interrupted')])
def test_encode_io_failure_reaches_retry_path(tmp_path, failure):
    params = ArtifactRef(uuid4(), 'a' * 64)
    root = ArtifactRef(uuid4(), 'b' * 64)
    with pytest.raises(OSError) as caught:
        prepare_delivery_encode(Reader(failure), params, root, scratch_root=tmp_path,
            max_bytes=1024, max_pixels=100)
    assert caught.value is failure


def test_encode_invalid_contract_remains_semantic_refusal(tmp_path):
    params = ArtifactRef(uuid4(), 'a' * 64)
    root = ArtifactRef(uuid4(), 'b' * 64)
    with pytest.raises(AdoptionInvalid, match='DELIVERY_ENCODE_PHYSICAL_INVALID'):
        prepare_delivery_encode(Reader(ValueError('invalid contract')), params, root,
            scratch_root=tmp_path, max_bytes=1024, max_pixels=100)
