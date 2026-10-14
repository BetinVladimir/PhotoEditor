from copy import deepcopy
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef as ContractRef, canonical_bytes, canonical_hash
from photo_worker.contracts.semantic import V2SemanticAssembleParams, TransferCandidate
from photo_worker.operations.semantic.assemble import assemble_semantic_plan
from photo_worker.operations.session.contracts import InputSnapshot, SnapshotItem
from photo_worker.v2.adoption import AdoptionInvalid, ArtifactRef, prepare_v2_adoption
from photo_worker.v2.semantic_plan_adoption import verify_semantic_plan
from test_semantic_transfer_contracts import transfer_documents
from test_v2_adoption import Reader


def case():
    transfer, intent, calibration = transfer_documents()
    reader = Reader()
    def add(value, source='input', role=None):
        value_ref = ContractRef(artifact_id=uuid4(), sha256=canonical_hash(value))
        reader.add(ArtifactRef(value_ref.artifact_id, value_ref.sha256), body=value, source=source, role=role)
        return value_ref
    intent_ref, calibration_ref = add(intent), add(calibration)
    bindings = [transfer.target.model_copy(update={'asset_version_id':uuid4()}) for _ in range(2)]
    candidates = [TransferCandidate(schema_version=1, target=binding, mask=None, transform=None,
        confidence=0, state='unsupported', reason='NO_PLANAR_CORRESPONDENCE') for binding in reversed(bindings)]
    params = V2SemanticAssembleParams(schema_version=1, intent=intent_ref,
        calibration=calibration_ref, model_lock_sha=transfer.model_lock_sha,
        candidates=[add(candidate) for candidate in candidates])
    params_ref = add(params)
    snapshot = InputSnapshot(schema_version=1, snapshot_id=intent.target_snapshot_id,
        snapshot_digest='0'*64, items=[SnapshotItem(ordinal=n, binding=binding,
            working_image=binding.source, recipe=intent_ref, captured_at=None, camera_hash=None)
            for n,binding in enumerate(bindings)])
    snapshot.snapshot_digest = canonical_hash(snapshot.model_dump(mode='json',exclude={'snapshot_digest'}))
    plan = assemble_semantic_plan(params, intent, snapshot, calibration, candidates)
    root = add(plan, 'output', 'document')
    reader.add(ArtifactRef(transfer.target.source.artifact_id,transfer.target.source.sha256), source='input')
    return reader, ArtifactRef(params_ref.artifact_id,params_ref.sha256), ArtifactRef(root.artifact_id,root.sha256), snapshot, plan


@pytest.mark.parametrize('variant', ['valid','reason','confidence','order','model','snapshot','bytes',
                                     'input_scope','output_role','authority','mutate','cancel'])
def test_semantic_plan_reassembly_and_scoped_authority(variant):
    reader, params, root, snapshot, plan = case()
    changed = deepcopy(plan)
    if variant == 'reason': changed.candidates[0].reason = 'DIFFERENT_REASON'
    if variant == 'confidence': changed.candidates[0].confidence = .5
    if variant == 'order': changed.candidates.reverse()
    if variant == 'model': changed.model_lock_sha = 'e'*64
    if variant == 'snapshot': snapshot.snapshot_digest = 'f'*64
    if variant in ('reason','confidence','order','model'):
        reader.uploads.remove(root)
        root = ArtifactRef(uuid4(),canonical_hash(changed))
        reader.add(root, body=changed, role='document')
    if variant == 'bytes': reader.body[root] += b' '
    if variant == 'input_scope':
        from dataclasses import replace
        reader.rows[params] = replace(reader.rows[params], source='output')
    if variant == 'output_role':
        from dataclasses import replace
        reader.rows[root] = replace(reader.rows[root], role='report')
    calls = []
    def provenance(*documents):
        calls.append('provenance')
        if variant == 'authority': raise AdoptionInvalid('SEMANTIC_PRODUCER_STALE')
        if variant == 'mutate':
            documents[0].model_lock_sha = 'd'*64
            documents[-1][0].reason = 'MUTATED'
    def active():
        if variant == 'cancel': raise InterruptedError('cancelled')
    def verify():
        return verify_semantic_plan(reader, params, root, load_snapshot=lambda *args:snapshot,
            verify_provenance=provenance, check_active=active)
    if variant == 'cancel':
        with pytest.raises(InterruptedError): verify()
        assert calls == []
    elif variant in ('valid','mutate'):
        expected = verify()
        assert canonical_bytes(expected) == canonical_bytes(plan)
        assert calls == ['provenance']
    else:
        with pytest.raises(AdoptionInvalid, match='SEMANTIC_'): verify()
        assert calls == (['provenance'] if variant == 'authority' else [])


def test_semantic_plan_generic_adoption_requires_and_runs_reassembly():
    reader, params, root, snapshot, _ = case()
    kwargs = dict(task_id=uuid4(),operation='v2.semantic.assemble',version='1',root=root,reader=reader)
    with pytest.raises(AdoptionInvalid,match='SEMANTIC_PLAN_PARAMS_REQUIRED'):
        prepare_v2_adoption(**kwargs)
    with pytest.raises(AdoptionInvalid,match='SEMANTIC_PLAN_AUTHORITY_REQUIRED'):
        prepare_v2_adoption(**kwargs,params_ref=params)
    calls=[]
    prepared=prepare_v2_adoption(**kwargs,params_ref=params,semantic_plan_snapshot=lambda *args:snapshot,
        semantic_plan_provenance=lambda *args:calls.append('provenance'))
    assert calls==['provenance'] and prepared.inventory[0]['artifact_id']==str(root.artifact_id)


def test_semantic_plan_missing_authority_denies_before_document_reads():
    class NoReads:
        def evidence(self,*args):raise AssertionError('Unexpected evidence IO')
    with pytest.raises(AdoptionInvalid,match='SEMANTIC_PLAN_AUTHORITY_REQUIRED'):
        verify_semantic_plan(NoReads(),None,None,load_snapshot=None,verify_provenance=lambda *args:None)


def test_semantic_plan_cancel_after_provenance_prevents_prepared_inventory():
    reader,params,root,snapshot,_=case()
    cancelled=False
    def provenance(*args):
        nonlocal cancelled
        cancelled=True
    def active():
        if cancelled:raise InterruptedError('cancelled during authority verification')
    with pytest.raises(InterruptedError,match='authority verification'):
        prepare_v2_adoption(task_id=uuid4(),operation='v2.semantic.assemble',version='1',root=root,
            reader=reader,params_ref=params,semantic_plan_snapshot=lambda *args:snapshot,
            semantic_plan_provenance=provenance,check_active=active)
