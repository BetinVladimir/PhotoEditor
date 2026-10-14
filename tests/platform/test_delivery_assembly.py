"""Authored V30 matrix tests; not runtime/export/qualification evidence."""
import hashlib
import json
from uuid import UUID
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.operations.delivery.assembly import assemble_delivery


def uid(n):
    return str(UUID(int=n))


def ref(n, raw=None):
    return {'artifact_id': uid(n), 'sha256': hashlib.sha256(raw).hexdigest() if raw else 'a' * 64}


def fixture():
    binding = {'asset_version_id': uid(1), 'source': ref(2), 'canvas_hash': 'b' * 64}
    slots = [{'ordinal': 0, 'binding': binding, 'preset_id': uid(10), 'preset_sha': 'c' * 64,
              'required': True, 'state': 'ready', 'export_id': uid(30), 'export': ref(31), 'reason': None},
             {'ordinal': 0, 'binding': binding, 'preset_id': uid(11), 'preset_sha': 'd' * 64,
              'required': False, 'state': 'omitted', 'export_id': None, 'export': None, 'reason': 'child failed'}]
    shard = {'schema_version': 1, 'snapshot_digest': 'e' * 64, 'items': slots}
    assembly = {'schema_version': 1, 'snapshot_digest': 'e' * 64, 'item_count': 1, 'slot_count': 2,
                'policy': {'required_presets': [uid(10)], 'optional_presets': [uid(11)],
                           'require_review': True, 'metadata_policy': 'strip', 'provenance': 'off', 'allow_omitted': True},
                'presets': [{'preset_id': uid(10), 'preset_sha': 'c' * 64, 'config': {**ref(40), 'sha256': 'c' * 64}, 'required': True},
                            {'preset_id': uid(11), 'preset_sha': 'd' * 64, 'config': {**ref(41), 'sha256': 'd' * 64}, 'required': False}],
                'shards': []}
    return assembly, shard


def execute(assembly, shard, cancelled=lambda: False):
    shard_raw = canonical_bytes(shard)
    assembly['shards'] = [ref(50, shard_raw)]
    raw = canonical_bytes(assembly)
    return assemble_delivery(ArtifactRef.model_validate_json(json.dumps(ref(51, raw))), raw,
                             lambda _: shard_raw, cancelled)


def test_manifest_retains_exact_shards_and_optional_omission():
    assembly, shard = fixture()
    result = execute(assembly, shard)
    assert (result.ready_count, result.omitted_count, result.slot_count) == (1, 1, 2)
    assert result.shards[0].sha256 == assembly['shards'][0]['sha256']
    result.shards[0].sha256 = 'f' * 64
    assert assembly['shards'][0]['sha256'] != 'f' * 64


@pytest.mark.parametrize('change', ['duplicate', 'missing', 'required_omitted', 'binding', 'preset_sha', 'snapshot', 'failed', 'provenance'])
def test_invalid_matrix_cannot_produce_manifest(change):
    assembly, shard = fixture()
    if change == 'duplicate': shard['items'][1]['preset_id'] = uid(10)
    elif change == 'missing': shard['items'].pop()
    elif change == 'required_omitted': shard['items'][0].update(state='omitted', export=None, export_id=None, reason='failure')
    elif change == 'binding': shard['items'][1]['binding'] = {**shard['items'][1]['binding'], 'asset_version_id': uid(99)}
    elif change == 'preset_sha': shard['items'][1]['preset_sha'] = 'f' * 64
    elif change == 'snapshot': shard['snapshot_digest'] = 'f' * 64
    elif change == 'failed': shard['items'][1]['state'] = 'failed'
    elif change == 'provenance': assembly['policy']['provenance'] = 'required'
    with pytest.raises(ValueError): execute(assembly, shard)


def test_one_ready_export_job_cannot_fill_two_slots():
    assembly, shard = fixture()
    shard['items'][1].update(state='ready', export_id=shard['items'][0]['export_id'],
                             export=ref(32), reason=None)
    with pytest.raises(ValueError, match='DELIVERY_MATRIX_INVALID'):
        execute(assembly, shard)


def test_cancellation_prevents_assembly():
    with pytest.raises(ValueError, match='DELIVERY_CANCELLED'):
        execute(*fixture(), cancelled=lambda: True)


def test_rejects_wrong_content_digest_before_shard_read():
    calls = []
    with pytest.raises(ValueError, match='DELIVERY_CONTENT_MISMATCH'):
        assemble_delivery(ArtifactRef.model_validate_json(json.dumps(ref(51))), b'{}', lambda r: calls.append(r))
    assert not calls


@pytest.mark.parametrize('preset_index', [0, 1])
def test_rejects_preset_config_sha_substitution(preset_index):
    assembly, shard = fixture()
    assembly['presets'][preset_index]['config']['sha256'] = 'f' * 64
    with pytest.raises(ValueError, match='DELIVERY_MATRIX_INVALID'):
        execute(assembly, shard)


def test_reader_cannot_change_assembly_identity_in_manifest():
    assembly, shard = fixture()
    shard_raw = canonical_bytes(shard)
    assembly['shards'] = [ref(50, shard_raw)]
    raw = canonical_bytes(assembly)
    root = ArtifactRef.model_validate_json(json.dumps(ref(51, raw)))
    expected_sha = root.sha256

    def read(request):
        root.sha256 = 'f' * 64
        root.artifact_id = UUID(int=99)
        request.sha256 = '0' * 64
        return shard_raw

    result = assemble_delivery(root, raw, read)
    assert result.assembly_sha == expected_sha
    assert result.shards[0].sha256 == hashlib.sha256(shard_raw).hexdigest()


def test_nil_assembly_identity_rejected_before_storage_io():
    assembly, shard = fixture()
    shard_raw = canonical_bytes(shard)
    assembly['shards'] = [ref(50, shard_raw)]
    raw = canonical_bytes(assembly)
    root = ArtifactRef.model_validate_json(json.dumps(ref(0, raw)))
    calls = []
    with pytest.raises(ValueError, match='DELIVERY_MATRIX_INVALID'):
        assemble_delivery(root, raw, lambda request: calls.append(request))
    assert calls == []


@pytest.mark.parametrize('policy',['copyright','approved_exif'])
def test_metadata_policy_is_committed_by_assembly_sha(policy):
    assembly,shard=fixture()
    baseline=execute(assembly,shard)
    assembly['policy']['metadata_policy']=policy
    result=execute(assembly,shard)
    assert result.assembly_sha!=baseline.assembly_sha
    assert result.shards==baseline.shards
    assert (result.ready_count,result.omitted_count)==(1,1)


def test_assembly_unknown_metadata_policy_rejected():
    assembly,shard=fixture()
    assembly['policy']['metadata_policy']='unknown'
    with pytest.raises(ValueError):execute(assembly,shard)
