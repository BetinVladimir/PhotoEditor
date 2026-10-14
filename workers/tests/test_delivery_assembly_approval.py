import hashlib
from uuid import uuid4
from unittest.mock import patch

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.operations.delivery.assembly import assemble_delivery


def assembly_input(fields):
    preset = str(uuid4())
    value = {
        'schema_version': 1, 'snapshot_digest': 'a' * 64, 'item_count': 1, 'slot_count': 1,
        'policy': {'required_presets': [preset], 'optional_presets': [],
                   'require_review': False, 'metadata_policy': 'approved_exif',
                   'provenance': 'off', 'allow_omitted': False, 'approved_fields': fields},
        'presets': [{'preset_id': preset, 'preset_sha': 'b' * 64,
                     'config': {'artifact_id': str(uuid4()), 'sha256': 'b' * 64}, 'required': True}],
        'shards': [{'artifact_id': str(uuid4()), 'sha256': 'c' * 64}],
    }
    raw = canonical_bytes(value)
    return ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(raw).hexdigest()), raw


def test_frozen_approval_order_refused_before_shard_io():
    ref, raw = assembly_input(['gps', 'author'])
    calls = []
    with pytest.raises(ValueError, match='DELIVERY_NONCANONICAL'):
        assemble_delivery(ref, raw, lambda shard: calls.append(shard))
    assert calls == []


@pytest.mark.parametrize('fields', [['gps', 'gps'], ['private_embedding'], ['GPS']])
def test_invalid_frozen_approval_refused_before_shard_io(fields):
    ref, raw = assembly_input(fields)
    calls = []
    with pytest.raises(ValueError):
        assemble_delivery(ref, raw, lambda shard: calls.append(shard))
    assert calls == []


@pytest.mark.parametrize('fields', [[], ['author', 'gps']])
def test_canonical_approval_reaches_scoped_shard_reader(fields):
    ref, raw = assembly_input(fields)
    calls = []
    sentinel = RuntimeError('scoped reader boundary')

    def read(shard):
        calls.append(shard)
        raise sentinel

    with pytest.raises(RuntimeError) as failure:
        assemble_delivery(ref, raw, read)
    assert failure.value is sentinel
    assert len(calls) == 1

@pytest.mark.parametrize('raw', [b'', b'{', b' ' * 1_048_577])
def test_assembly_document_size_rejected_before_hash_or_reader(raw):
    ref = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    calls = []
    with patch('photo_worker.operations.delivery.assembly.hashlib.sha256') as digest:
        with pytest.raises(ValueError, match='DELIVERY_CONTENT_MISMATCH'):
            assemble_delivery(ref, raw, lambda shard: calls.append(shard))
        digest.assert_not_called()
    assert calls == []


def test_cancellation_during_shard_read_precedes_returned_content_validation():
    ref, raw = assembly_input(['author', 'gps'])
    cancelled = False
    calls = []

    def read(shard):
        nonlocal cancelled
        calls.append(shard)
        cancelled = True
        return b'not a valid shard'

    with pytest.raises(ValueError, match='DELIVERY_CANCELLED'):
        assemble_delivery(ref, raw, read, lambda: cancelled)
    assert len(calls) == 1
