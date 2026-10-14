import copy
import hashlib
from uuid import UUID, uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.operations.delivery.assembly import assemble_delivery


def matrix_fixture(items=251, omission=False):
    required = str(UUID(int=1))
    optional = str(UUID(int=2))
    digest = 'a' * 64
    slots = []
    for ordinal in range(items):
        binding = {'asset_version_id': str(uuid4()),
                   'source': {'artifact_id': str(uuid4()), 'sha256': 'b' * 64},
                   'canvas_hash': 'c' * 64}
        for preset, mandatory in ((required, True), (optional, False)):
            omitted = omission and not mandatory and ordinal % 2 == 0
            slots.append({'ordinal': ordinal, 'binding': copy.deepcopy(binding),
                          'preset_id': preset, 'preset_sha': 'd' * 64, 'required': mandatory,
                          'state': 'omitted' if omitted else 'ready',
                          'export_id': None if omitted else str(uuid4()),
                          'export': None if omitted else {'artifact_id': str(uuid4()), 'sha256': 'e' * 64},
                          'reason': 'OPTIONAL_EXPORT_FAILED' if omitted else None})
    value = {'schema_version': 1, 'snapshot_digest': digest, 'item_count': items,
             'slot_count': len(slots),
             'policy': {'required_presets': [required], 'optional_presets': [optional],
                        'require_review': True, 'metadata_policy': 'strip',
                        'provenance': 'off', 'allow_omitted': omission},
             'presets': [{'preset_id': preset, 'preset_sha': 'd' * 64,
                          'config': {'artifact_id': str(uuid4()), 'sha256': 'd' * 64},
                          'required': mandatory} for preset, mandatory in ((required, True), (optional, False))]}
    return value, slots


def pin_matrix(value, slots):
    value = copy.deepcopy(value)
    bodies = {}
    value['shards'] = []
    for start in range(0, len(slots), 500):
        body = canonical_bytes({'schema_version': 1, 'snapshot_digest': value['snapshot_digest'],
                                'items': slots[start:start + 500]})
        identity = uuid4()
        value['shards'].append({'artifact_id': str(identity), 'sha256': hashlib.sha256(body).hexdigest()})
        bodies[identity] = body
    raw = canonical_bytes(value)
    ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(raw).hexdigest())
    return ref, raw, bodies


@pytest.mark.parametrize('omission', [False, True])
def test_complete_two_preset_matrix_across_shard_boundary(omission):
    value, slots = matrix_fixture(omission=omission)
    before = copy.deepcopy(slots)
    ref, raw, bodies = pin_matrix(value, slots)
    reads = []

    def read(shard):
        reads.append(shard.artifact_id)
        return bodies[shard.artifact_id]

    result = assemble_delivery(ref, raw, read)
    omitted = 126 if omission else 0
    assert result.slot_count == 502
    assert result.omitted_count == omitted
    assert result.ready_count == 502 - omitted
    assert result.assembly_sha == ref.sha256
    assert result.snapshot_digest == value['snapshot_digest']
    assert reads == list(bodies)
    assert len(result.shards) == 2
    assert slots == before


@pytest.mark.parametrize('kind', ['ordinal', 'binding', 'preset_sha', 'required',
                                  'ready_without_export', 'ready_with_reason',
                                  'required_omitted', 'optional_omission_forbidden'])
def test_exact_matrix_semantics_refuse_byte_valid_malformed_slots(kind):
    value, slots = matrix_fixture()
    # Mutate the first slot of the second shard; pins are then recomputed so
    # failures prove semantic validation rather than a content-hash mismatch.
    slot = slots[500]
    if kind == 'ordinal': slot['ordinal'] = 249
    elif kind == 'binding': slots[501]['binding']['canvas_hash'] = 'f' * 64
    elif kind == 'preset_sha': slot['preset_sha'] = 'f' * 64
    elif kind == 'required': slot['required'] = False
    elif kind == 'ready_without_export': slot['export'] = None
    elif kind == 'ready_with_reason': slot['reason'] = 'FAILED'
    elif kind == 'required_omitted':
        slot.update(state='omitted', export=None, export_id=None, reason='FAILED')
        value['policy']['allow_omitted'] = True
    elif kind == 'optional_omission_forbidden':
        slots[501].update(state='omitted', export=None, export_id=None, reason='FAILED')
    ref, raw, bodies = pin_matrix(value, slots)
    reads = []

    def read(shard):
        reads.append(shard.artifact_id)
        return bodies[shard.artifact_id]

    with pytest.raises(ValueError, match='DELIVERY_MATRIX_INVALID'):
        assemble_delivery(ref, raw, read)
    assert reads == list(bodies)


@pytest.mark.parametrize('last_ordinal', [1,250])
def test_duplicate_snapshot_version_refuses_within_and_across_shards(last_ordinal):
    value,slots = matrix_fixture(items=251)
    # Both preset slots agree, and all hashes are freshly valid. Only the
    # forbidden duplicate source version distinguishes this matrix.
    for index in (last_ordinal*2,last_ordinal*2+1):
        slots[index]['binding'] = copy.deepcopy(slots[0]['binding'])
    ref,raw,bodies = pin_matrix(value,slots)
    reads = []
    def read(shard):
        reads.append(shard.artifact_id)
        return bodies[shard.artifact_id]
    with pytest.raises(ValueError,match='DELIVERY_MATRIX_INVALID'):
        assemble_delivery(ref,raw,read)
    assert len(reads) == (1 if last_ordinal == 1 else 2)
