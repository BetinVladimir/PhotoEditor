import copy
import hashlib
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.operations.delivery.preparation import prepare_delivery


def fixture(fields):
    objects = {}

    def pin(value):
        raw = canonical_bytes(value)
        identity = uuid4()
        ref = {'artifact_id': str(identity), 'sha256': hashlib.sha256(raw).hexdigest()}
        objects[identity] = raw
        return ref

    snapshot = {'schema_version': 1, 'snapshot_id': str(uuid4()), 'items': []}
    for ordinal in range(2):
        snapshot['items'].append({'ordinal': ordinal,
            'binding': {'asset_version_id': str(uuid4()),
                        'source': {'artifact_id': str(uuid4()), 'sha256': 'a' * 64},
                        'canvas_hash': 'b' * 64},
            'working_image': {'artifact_id': str(uuid4()), 'sha256': 'c' * 64},
            'recipe': {'artifact_id': str(uuid4()), 'sha256': 'd' * 64},
            'captured_at': None, 'camera_hash': None})
    for item in snapshot['items']:
        item['working_image'] = copy.deepcopy(item['binding']['source'])
    snapshot['snapshot_digest'] = canonical_hash(snapshot)
    snapshot_ref = pin(snapshot)
    presets = []
    for required in (True, False):
        config = pin({'format': 'jpeg' if required else 'png', 'icc_id': 'srgb_v1',
                      'subsampling': '444', 'alpha': 'matte' if required else 'preserve',
                      'metadata_policy': 'approved_exif', 'naming_template': '{original}',
                      **({'quality': 92, 'matte_rgb': [1, 1, 1]} if required else {})})
        presets.append({'preset_id': str(uuid4()), 'preset_sha': config['sha256'],
                        'config': config, 'required': required})
    frozen = {'schema_version': 1, 'snapshot': snapshot_ref,
              'snapshot_digest': snapshot['snapshot_digest'], 'presets': presets,
              'policy': {'required_presets': [presets[0]['preset_id']],
                         'optional_presets': [presets[1]['preset_id']],
                         'require_review': True, 'metadata_policy': 'approved_exif',
                         'approved_fields': fields, 'provenance': 'off', 'allow_omitted': True}}
    root = pin(frozen)
    return ArtifactRef.model_validate_json(canonical_bytes(root)), objects, frozen


def test_complete_preparation_reads_frozen_snapshot_and_both_presets():
    ref, objects, frozen = fixture(['author', 'gps'])
    original = copy.deepcopy(objects)
    reads, validations = [], []

    def read(dependency):
        reads.append(dependency.artifact_id)
        return objects[dependency.artifact_id]

    result = prepare_delivery(ref, objects[ref.artifact_id], read, validations.append)
    assert result.item_count == 2
    assert result.slot_count == 4
    assert result.snapshot_digest == frozen['snapshot_digest']
    assert result.input == ref
    assert len(reads) == 3
    assert len(validations) == 2
    assert reads[0] == ArtifactRef.model_validate_json(canonical_bytes(frozen['snapshot'])).artifact_id
    assert objects == original


def test_required_provenance_preparation_preserves_the_signed_policy_choice():
    ref, objects, frozen = fixture(['author', 'gps'])
    frozen['policy'].update(provenance='required',signing_policy_id=str(uuid4()),signing_policy_sha='e'*64)
    raw=canonical_bytes(frozen)
    ref=ArtifactRef(artifact_id=ref.artifact_id,sha256=hashlib.sha256(raw).hexdigest())
    objects[ref.artifact_id]=raw
    reads=[]
    def read(dependency):
        reads.append(dependency.artifact_id)
        return objects[dependency.artifact_id]
    prepared=prepare_delivery(ref,raw,read,lambda _:None)
    assert prepared.slot_count==4
    assert prepared.input==ref
    assert len(reads)==3


@pytest.mark.parametrize('fields', [['gps', 'author'], ['gps', 'gps'], ['private_embedding']])
def test_malformed_frozen_approval_refuses_all_dependency_io(fields):
    ref, objects, _ = fixture(fields)
    reads, validations = [], []
    with pytest.raises(ValueError):
        prepare_delivery(ref, objects[ref.artifact_id], reads.append, validations.append)
    assert reads == []
    assert validations == []


def test_snapshot_read_cancellation_prevents_config_reads_and_validation():
    ref, objects, _ = fixture(['author', 'gps'])
    cancelled = False
    reads, validations = [], []

    def read(dependency):
        nonlocal cancelled
        reads.append(dependency.artifact_id)
        cancelled = True
        return b'bad snapshot after cancellation'

    with pytest.raises(ValueError, match='DELIVERY_CANCELLED'):
        prepare_delivery(ref, objects[ref.artifact_id], read, validations.append, lambda: cancelled)
    assert len(reads) == 1
    assert validations == []


@pytest.mark.parametrize('kind', ['snapshot_zero', 'config_zero', 'preset_zero', 'required_bit', 'config_pin'])
def test_entire_frozen_preset_matrix_preflight_precedes_dependency_reads(kind):
    ref, _, frozen = fixture(['author', 'gps'])
    zero = '00000000-0000-0000-0000-000000000000'
    # Corrupt the last preset to prove earlier configs/snapshot are not read.
    if kind == 'snapshot_zero': frozen['snapshot']['artifact_id'] = zero
    elif kind == 'config_zero': frozen['presets'][1]['config']['artifact_id'] = zero
    elif kind == 'preset_zero':
        frozen['presets'][1]['preset_id'] = zero
        frozen['policy']['optional_presets'] = [zero]
    elif kind == 'required_bit': frozen['presets'][1]['required'] = True
    elif kind == 'config_pin': frozen['presets'][1]['preset_sha'] = 'f' * 64
    raw = canonical_bytes(frozen)
    ref = ArtifactRef(artifact_id=ref.artifact_id, sha256=hashlib.sha256(raw).hexdigest())
    reads, validations = [], []
    with pytest.raises(ValueError, match='DELIVERY_(SNAPSHOT|PRESET)_INVALID'):
        prepare_delivery(ref, raw, reads.append, validations.append)
    assert reads == []
    assert validations == []


@pytest.mark.parametrize('mode', ['success', 'validator_cancel', 'admission_mutation'])
def test_composed_preparation_adapter_lifecycle(mode):
    from types import SimpleNamespace
    from photo_worker.contracts.export_preset import validate_frozen_export_preset
    from photo_worker.contracts.delivery import V2DeliveryPrepareParams
    from photo_worker.operations.delivery.adapter import DeliveryInstallation, execute
    from photo_worker.runtime.executor import OperationFailure

    ref, objects, frozen = fixture(['author', 'gps'])
    params = V2DeliveryPrepareParams(schema_version=1, input=ref)
    original = params.model_copy(deep=True)
    events, staged = [], []
    cancelled = False

    def active():
        if cancelled:
            raise RuntimeError('fixture lease lost')

    ctx = SimpleNamespace(check_active=active, lease=SimpleNamespace(
        execution_profile={'renderer_version': 'v2.2', 'backend': 'cpu'},
        command=SimpleNamespace(resource_class='cpu', operation_version='1', operation='v2.delivery.prepare')))

    def admit(context, candidate):
        events.append('admit')
        if mode == 'admission_mutation':
            candidate.input.sha256 = 'f' * 64

    def validate(raw):
        nonlocal cancelled
        events.append('validate')
        validate_frozen_export_preset(raw)
        if mode == 'validator_cancel':
            cancelled = True

    class Artifacts:
        def params(self, model):
            events.append('params')
            return params

        def contract_bytes(self, candidate, model):
            events.append('read')
            raw = objects[candidate.artifact_id]
            assert hashlib.sha256(raw).hexdigest() == candidate.sha256
            model.model_validate_json(raw)
            return raw

        def stage_contract(self, result, role):
            events.append('stage')
            assert role == 'document'
            staged.append(result.model_copy(deep=True))
            return ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(result)).hexdigest())

    installation = DeliveryInstallation(admission=SimpleNamespace(admit=admit),
        validate_preset=validate)
    if mode == 'success':
        result = execute(ctx, Artifacts(), installation, operation='v2.delivery.prepare')
        assert events == ['params', 'admit', 'read', 'read', 'read', 'validate', 'read', 'validate', 'admit', 'stage']
        assert len(result.outputs) == 1 and result.review_required is False
        assert result.bytes_written == len(canonical_bytes(staged[0]))
        assert staged[0].item_count == 2 and staged[0].slot_count == 4
        assert staged[0].snapshot_digest == frozen['snapshot_digest']
        assert staged[0].input == original.input
    elif mode == 'validator_cancel':
        with pytest.raises(RuntimeError, match='fixture lease lost'):
            execute(ctx, Artifacts(), installation, operation='v2.delivery.prepare')
        assert events == ['params', 'admit', 'read', 'read', 'read', 'validate']
        assert staged == []
    else:
        with pytest.raises(OperationFailure):
            execute(ctx, Artifacts(), installation, operation='v2.delivery.prepare')
        assert events == ['params', 'admit']
        assert staged == []
    assert params == original


@pytest.mark.parametrize('change', ['jpeg_no_quality', 'png_quality', 'missing_subsampling', 'null_size', 'metadata_mismatch'])
def test_preparation_real_export_validator_rejects_frozen_bad_config(change):
    from photo_worker.contracts.export_preset import validate_frozen_export_preset
    from photo_worker.contracts.base import parse_json

    ref, objects, frozen = fixture(['author', 'gps'])
    index = 1 if change == 'png_quality' else 0
    preset = frozen['presets'][index]
    config_id = ArtifactRef.model_validate_json(canonical_bytes(preset['config'])).artifact_id
    config = parse_json(objects[config_id])
    if change == 'jpeg_no_quality': config.pop('quality')
    elif change == 'png_quality': config['quality'] = 92
    elif change == 'missing_subsampling': config.pop('subsampling')
    elif change == 'null_size': config['size'] = None
    else: config['metadata_policy'] = 'strip'
    raw = canonical_bytes(config)
    objects[config_id] = raw
    preset['preset_sha'] = preset['config']['sha256'] = hashlib.sha256(raw).hexdigest()
    root_raw = canonical_bytes(frozen)
    ref = ArtifactRef(artifact_id=ref.artifact_id, sha256=hashlib.sha256(root_raw).hexdigest())
    validated = []

    def validate(raw):
        validate_frozen_export_preset(raw)
        validated.append(raw)

    with pytest.raises(ValueError):
        prepare_delivery(ref, root_raw, lambda dependency: objects[dependency.artifact_id], validate)
    assert len(validated) == index


@pytest.mark.parametrize('mode', ['foreign_working_image', 'duplicate_asset_version'])
def test_rehashed_but_semantically_invalid_snapshot_refuses_preset_reads(mode):
    import json
    from uuid import UUID
    ref,objects,frozen = fixture(['author'])
    snapshot_id = UUID(frozen['snapshot']['artifact_id'])
    snapshot = json.loads(objects[snapshot_id])
    if mode == 'foreign_working_image':
        snapshot['items'][0]['working_image']['artifact_id'] = str(uuid4())
    else:
        snapshot['items'][1]['binding']['asset_version_id'] = snapshot['items'][0]['binding']['asset_version_id']
    snapshot.pop('snapshot_digest')
    snapshot['snapshot_digest'] = canonical_hash(snapshot)
    objects[snapshot_id] = canonical_bytes(snapshot)
    frozen['snapshot']['sha256'] = hashlib.sha256(objects[snapshot_id]).hexdigest()
    frozen['snapshot_digest'] = snapshot['snapshot_digest']
    raw = canonical_bytes(frozen)
    root = ref.model_copy(update={'sha256':hashlib.sha256(raw).hexdigest()})
    reads,validations = [],[]
    def read(dependency):
        reads.append(dependency.artifact_id)
        return objects[dependency.artifact_id]
    with pytest.raises(ValueError,match='DELIVERY_SNAPSHOT_INVALID'):
        prepare_delivery(root,raw,read,validations.append)
    assert reads == [snapshot_id]
    assert validations == []
