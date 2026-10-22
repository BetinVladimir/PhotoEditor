import json
"""Physical-evidence manifest reconstruction; no SQL authority implied."""
from dataclasses import replace
from hashlib import sha256
from uuid import UUID
import pytest
from photo_worker.contracts.base import ArtifactRef as WireRef, canonical_bytes
from photo_worker.operations.delivery.assembly import assemble_delivery
from photo_worker.v2.adoption import ArtifactRef, AdoptionInvalid
from photo_worker.v2.delivery_assembly_adoption import verify_delivery_assembly
from test_delivery_assembly import fixture, ref, uid
from test_delivery_prepare_adoption import Reader


def adoption_fixture():
    assembly, shard = fixture()
    shard_raw = canonical_bytes(shard)
    assembly['shards'] = [ref(50, shard_raw)]
    assembly_raw = canonical_bytes(assembly)
    assembly_ref = WireRef.model_validate_json(json.dumps(ref(51, assembly_raw)))
    params_raw = canonical_bytes({'schema_version': 1, 'assembly': ref(51, assembly_raw)})
    expected = assemble_delivery(assembly_ref, assembly_raw, lambda _: shard_raw)
    result_raw = canonical_bytes(expected)
    root = ArtifactRef(UUID(uid(53)), sha256(result_raw).hexdigest())
    params = ArtifactRef(UUID(uid(52)), sha256(params_raw).hexdigest())
    return Reader({uid(50): shard_raw, uid(51): assembly_raw,
                   uid(52): params_raw, uid(53): result_raw}, root), params, root


def test_reconstructs_manifest_from_verified_shards():
    reader, params, root = adoption_fixture()
    verified = verify_delivery_assembly(reader, params, root)
    assert verified.params_jcs == reader.objects[params.artifact_id]
    assert verified.result_jcs == reader.objects[root.artifact_id]


@pytest.mark.parametrize('change', ['role', 'source', 'hash', 'extra_output', 'count', 'cancel'])
def test_refuses_untrusted_manifest(change):
    reader, params, root = adoption_fixture()
    if change == 'role':
        reader.metadata[root.artifact_id] = replace(reader.metadata[root.artifact_id], role='report')
    elif change == 'source':
        reader.metadata[root.artifact_id] = replace(reader.metadata[root.artifact_id], source='input')
    elif change == 'hash':
        reader.objects[root.artifact_id] = b'{}'
    elif change == 'extra_output':
        reader.outputs.add(params)
    elif change == 'count':
        raw = reader.objects[root.artifact_id].replace(b'"ready_count":1', b'"ready_count":2')
        root = ArtifactRef(root.artifact_id, sha256(raw).hexdigest())
        reader.objects[root.artifact_id] = raw
        reader.metadata[root.artifact_id] = replace(reader.metadata[root.artifact_id], ref=root, bytes=len(raw))
        reader.outputs = {root}
    def active():
        if change == 'cancel':
            raise AdoptionInvalid('CANCELLED')
    with pytest.raises(AdoptionInvalid):
        verify_delivery_assembly(reader, params, root, check_active=active)


def test_dispatcher_requires_assembly_params_before_generic_traversal():
    from photo_worker.v2.adoption import prepare_v2_adoption
    reader, _, root = adoption_fixture()
    with pytest.raises(AdoptionInvalid, match='DELIVERY_ASSEMBLY_PARAMS_REQUIRED'):
        prepare_v2_adoption(task_id=UUID(uid(70)), operation='v2.delivery.assemble',
                           version='1', root=root, reader=reader)


@pytest.mark.parametrize('mode', ['accepted', 'stale_params', 'refused_commit', 'corrupt_output'])
def test_service_uses_dedicated_assembly_commit_after_physical_reads(monkeypatch, mode):
    from contextlib import nullcontext
    from types import SimpleNamespace
    from photo_worker.v2.adoption_service import AdoptionCoordinator, AdoptionContext
    reader, params, root = adoption_fixture()
    tenant, task, result = UUID(uid(80)), UUID(uid(81)), UUID(uid(82))
    calls = []
    class Cursor:
        def __init__(self, row): self.row = row
        def fetchone(self): return self.row
    class Read:
        autocommit = True
        def execute(self, query, args):
            if 'v30_delivery_signed_adoption_scope' in query:
                assert args==(tenant,task,2,7,result,reader.objects[params.artifact_id].decode())
                calls.append('scope');return Cursor(None)
            calls.append('params')
            assert 'v30_delivery_assembly_adoption_params' in query
            assert args == (tenant, task, 2, 7, result)
            return Cursor(None if mode == 'stale_params' else (params.artifact_id, params.sha256))
    class Write:
        def transaction(self):
            calls.append('transaction')
            return nullcontext()
        def execute(self, query, args):
            calls.append('commit')
            assert 'adopt_v30_delivery_assembled' in query
            assert args[:5] == (tenant, task, 2, 7, result)
            assert args[-2:] == (reader.objects[params.artifact_id].decode(), reader.objects[root.artifact_id].decode())
            return Cursor((mode != 'refused_commit',))
    monkeypatch.setattr('photo_worker.v2.adoption_service.PostgresObjectEvidenceReader', lambda *a, **k: reader)
    def prepare(**kwargs):
        calls.append('prepare')
        assert kwargs['params_ref'] == params and kwargs['operation'] == 'v2.delivery.assemble'
        return SimpleNamespace(inventory=(), digest='a' * 64)
    monkeypatch.setattr('photo_worker.v2.adoption_service.prepare_v2_adoption', prepare)
    coordinator = AdoptionCoordinator(Read(), Write(), object(), 'photos')
    monkeypatch.setattr(coordinator, 'context', lambda *a: AdoptionContext(2, 7, result, 'v2.delivery.assemble', '1', root))
    if mode == 'corrupt_output': reader.objects[root.artifact_id] = b'{}'
    if mode in ('stale_params', 'corrupt_output'):
        with pytest.raises(AdoptionInvalid): coordinator.adopt(tenant, task)
        assert calls == (['params'] if mode=='stale_params' else ['params','scope'])
    else:
        assert coordinator.adopt(tenant, task) is (mode == 'accepted')
        assert calls == ['params', 'scope', 'prepare', 'transaction', 'commit']
