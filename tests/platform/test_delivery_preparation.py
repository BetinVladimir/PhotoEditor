"""Pure frozen-input tests; validator/readers are explicit doubles."""
import hashlib
import json
from uuid import UUID
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.operations.delivery.preparation import prepare_delivery


def uid(n): return str(UUID(int=n))
def ref(n, raw): return {'artifact_id': uid(n), 'sha256': hashlib.sha256(raw).hexdigest()}


def fixture():
    config = canonical_bytes({'format': 'jpeg', 'metadata_policy': 'strip'})
    reference = {'artifact_id': uid(10), 'sha256': 'a' * 64}
    snapshot = {'schema_version': 1, 'snapshot_id': uid(1), 'items': [
        {'ordinal': 0, 'binding': {'asset_version_id': uid(2), 'source': reference, 'canvas_hash': 'b' * 64},
         'working_image': reference, 'recipe': reference, 'captured_at': None, 'camera_hash': None}]}
    snapshot['snapshot_digest'] = canonical_hash(snapshot)
    snapshot_raw = canonical_bytes(snapshot)
    frozen = {'schema_version': 1, 'snapshot': ref(3, snapshot_raw), 'snapshot_digest': snapshot['snapshot_digest'],
              'policy': {'required_presets': [uid(4)], 'optional_presets': [], 'require_review': True,
                         'metadata_policy': 'strip', 'provenance': 'off', 'allow_omitted': False},
              'presets': [{'preset_id': uid(4), 'preset_sha': hashlib.sha256(config).hexdigest(), 'config': ref(5, config), 'required': True}]}
    return frozen, {uid(3): snapshot_raw, uid(5): config}


def execute(frozen, objects, validator=lambda raw: None):
    raw = canonical_bytes(frozen)
    return prepare_delivery(ArtifactRef.model_validate_json(json.dumps(ref(6, raw))), raw,
                            lambda r: objects[str(r.artifact_id)], validator)


def test_prepares_exact_counts_from_verified_snapshot_and_config():
    frozen, objects = fixture()
    seen = []
    result = execute(frozen, objects, seen.append)
    assert result.item_count == result.slot_count == 1
    assert seen == [objects[uid(5)]]
    assert result.snapshot_digest == frozen['snapshot_digest']


@pytest.mark.parametrize('change', ['config_hash', 'snapshot_bytes', 'required', 'duplicate', 'provenance'])
def test_refuses_frozen_input_substitution(change):
    frozen, objects = fixture()
    if change == 'config_hash': frozen['presets'][0]['preset_sha'] = 'f' * 64
    elif change == 'snapshot_bytes': objects[uid(3)] = b'{}'
    elif change == 'required': frozen['presets'][0]['required'] = False
    elif change == 'duplicate': frozen['presets'].append(frozen['presets'][0])
    elif change == 'provenance': frozen['policy']['provenance'] = 'required'
    with pytest.raises(ValueError): execute(frozen, objects)


def test_propagates_existing_preset_validator_refusal():
    def reject(raw): raise ValueError('UNSUPPORTED_PRESET')
    with pytest.raises(ValueError, match='UNSUPPORTED_PRESET'): execute(*fixture(), validator=reject)


@pytest.mark.parametrize('boundary', ['reader', 'validator', 'cancellation'])
def test_callbacks_cannot_rebind_preparation_input(boundary):
    frozen, objects = fixture()
    raw = canonical_bytes(frozen)
    root = ArtifactRef.model_validate_json(json.dumps(ref(6, raw)))
    expected = root.model_copy(deep=True)

    def mutate():
        root.artifact_id = UUID(int=99)
        root.sha256 = 'f' * 64

    def read(request):
        data = objects[str(request.artifact_id)]
        if boundary == 'reader':
            mutate()
            request.sha256 = '0' * 64
        return data

    def validate(data):
        if boundary == 'validator':
            mutate()

    def cancelled():
        if boundary == 'cancellation':
            mutate()
        return False

    result = prepare_delivery(root, raw, read, validate, cancelled)
    assert result.input == expected
    root.sha256 = '0' * 64
    assert result.input == expected
