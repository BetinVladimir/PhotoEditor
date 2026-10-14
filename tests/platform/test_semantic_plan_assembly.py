from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_hash
from photo_worker.contracts.semantic import V2SemanticAssembleParams,TransferCandidate
from photo_worker.operations.session.contracts import InputSnapshot,SnapshotItem
from photo_worker.operations.semantic.assemble import assemble_semantic_plan
from test_semantic_transfer_contracts import transfer_documents


@pytest.mark.parametrize('variant',['valid','missing','duplicate','changed','digest','state'])
def test_semantic_plan_exact_target_coverage_and_pins(variant):
    transfer,intent,calibration=transfer_documents()
    def ref(value):return ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(value))
    bindings=[transfer.target.model_copy(update={'asset_version_id':uuid4()}) for _ in range(2)]
    items=[SnapshotItem(ordinal=n,binding=binding,working_image=binding.source,captured_at=None,camera_hash=None,
        recipe=ref(intent)) for n,binding in enumerate(bindings)]
    snapshot=InputSnapshot(schema_version=1,snapshot_id=intent.target_snapshot_id,snapshot_digest='0'*64,items=items)
    snapshot.snapshot_digest=canonical_hash(snapshot.model_dump(mode='json',exclude={'snapshot_digest'}))
    candidates=[TransferCandidate(schema_version=1,target=binding,mask=None,transform=None,confidence=0,
        state='unsupported',reason='NO_PLANAR_CORRESPONDENCE') for binding in reversed(bindings)]
    if variant=='missing':candidates.pop()
    if variant=='duplicate':candidates[1]=candidates[0].model_copy(deep=True)
    if variant=='changed':candidates[0].target.canvas_hash='f'*64
    if variant=='digest':snapshot.snapshot_digest='f'*64
    if variant=='state':candidates[0].state='proposed'
    params=V2SemanticAssembleParams(schema_version=1,intent=ref(intent),candidates=[ref(c) for c in candidates],
        model_lock_sha=transfer.model_lock_sha,calibration=ref(calibration))
    if variant=='valid':
        plan=assemble_semantic_plan(params,intent,snapshot,calibration,candidates)
        assert [c.target for c in plan.candidates]==bindings
        candidates[0].reason='changed';assert plan.candidates[1].reason=='NO_PLANAR_CORRESPONDENCE'
    else:
        with pytest.raises(ValueError,match='SEMANTIC_'):assemble_semantic_plan(params,intent,snapshot,calibration,candidates)


@pytest.mark.parametrize('cancel',[False,True])
def test_semantic_plan_five_thousand_mixed_candidates_and_cancellation(cancel):
    import tracemalloc
    from photo_worker.contracts.base import canonical_bytes
    transfer,intent,calibration=transfer_documents()
    def ref(value):return ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(value))
    bindings=[transfer.target.model_copy(update={'asset_version_id':uuid4()}) for _ in range(5000)]
    recipe=ref(intent)
    items=[SnapshotItem(ordinal=n,binding=binding,working_image=binding.source,captured_at=None,camera_hash=None,
        recipe=recipe) for n,binding in enumerate(bindings)]
    snapshot=InputSnapshot(schema_version=1,snapshot_id=intent.target_snapshot_id,snapshot_digest='0'*64,items=items)
    snapshot.snapshot_digest=canonical_hash(snapshot.model_dump(mode='json',exclude={'snapshot_digest'}))
    candidates=[]
    for n,binding in enumerate(reversed(bindings)):
        mask=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
        transform=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
        state=('proposed','rejected','unsupported')[n%3]
        candidates.append(TransferCandidate(schema_version=1,target=binding,
            mask=None if state=='unsupported' else mask,transform=None if state=='unsupported' else transform,
            confidence=.8,state=state,reason=None if state=='proposed' else 'MANUAL_REJECTED' if state=='rejected' else 'NO_PLANAR_CORRESPONDENCE'))
    params=V2SemanticAssembleParams(schema_version=1,intent=recipe,candidates=[ref(c) for c in candidates],
        model_lock_sha=transfer.model_lock_sha,calibration=ref(calibration))
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if cancel and calls==100:raise InterruptedError('cancelled')
    if cancel:
        with pytest.raises(InterruptedError,match='cancelled'):
            assemble_semantic_plan(params,intent,snapshot,calibration,candidates,check_active=active)
        assert calls==100
    else:
        tracemalloc.start()
        try:
            plan=assemble_semantic_plan(params,intent,snapshot,calibration,candidates,check_active=active)
            _,peak=tracemalloc.get_traced_memory()
        finally:tracemalloc.stop()
        assert len(plan.candidates)==5000 and [c.target for c in plan.candidates]==bindings
        assert {c.state for c in plan.candidates}=={'proposed','rejected','unsupported'}
        assert len(canonical_bytes(plan))<8*1024*1024
        # Python allocations during pure assembly only; not RSS qualification.
        assert peak<256*1024*1024
        assert calls>=5002
