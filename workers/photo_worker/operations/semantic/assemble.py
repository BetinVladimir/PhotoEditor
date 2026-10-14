"""Content-level semantic plan assembly; scoped adoption is independent."""
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.semantic import V2SemanticAssembleParams,EditIntent,TransferCandidate,SemanticPlan
from photo_worker.operations.session.contracts import InputSnapshot,Calibration


def assemble_semantic_plan(params,intent,snapshot,calibration,candidates,*,check_active=lambda:None):
    """Verify pinned documents and exact coverage, preserving snapshot order.

    Caller must prove scoped source/target membership and candidate producer
    provenance, including intent/model/calibration agreement: TransferCandidate
    itself carries none of those producer pins. Original candidate refs remain
    in owned assembly params for lineage; SemanticPlan embeds typed candidates.
    """
    check_active()
    params=V2SemanticAssembleParams.model_validate_json(canonical_bytes(params))
    intent=EditIntent.model_validate_json(canonical_bytes(intent))
    snapshot=InputSnapshot.model_validate_json(canonical_bytes(snapshot))
    calibration=Calibration.model_validate_json(canonical_bytes(calibration))
    if canonical_hash(intent)!=params.intent.sha256:raise ValueError('SEMANTIC_INTENT_CHANGED')
    if canonical_hash(calibration)!=params.calibration.sha256:raise ValueError('SEMANTIC_CALIBRATION_CHANGED')
    if not calibration.qualified or calibration.model_lock_sha!=params.model_lock_sha:
        raise ValueError('SEMANTIC_CALIBRATION_INVALID')
    if (snapshot.snapshot_id!=intent.target_snapshot_id or snapshot.snapshot_digest!=canonical_hash(
            snapshot.model_dump(mode='json',exclude={'snapshot_digest'}))):
        raise ValueError('SEMANTIC_SNAPSHOT_CHANGED')
    if not isinstance(candidates,(list,tuple)) or len(candidates)!=len(params.candidates):
        raise ValueError('SEMANTIC_CANDIDATE_COVERAGE_INVALID')
    if len({ref.artifact_id for ref in params.candidates})!=len(params.candidates):
        raise ValueError('SEMANTIC_DUPLICATE_CANDIDATE')
    items={item.ordinal:item for item in snapshot.items}
    targets={item.binding.asset_version_id:item.binding for item in snapshot.items}
    if (len(items)!=len(snapshot.items) or len(targets)!=len(snapshot.items)
            or set(items)!=set(range(len(items)))
            or any(item.working_image!=item.binding.source for item in snapshot.items)):
        raise ValueError('SEMANTIC_SNAPSHOT_INVALID')
    by_target={}
    for ref,value in zip(params.candidates,candidates):
        check_active();candidate=TransferCandidate.model_validate_json(canonical_bytes(value))
        if canonical_hash(candidate)!=ref.sha256:raise ValueError('SEMANTIC_CANDIDATE_CHANGED')
        key=candidate.target.asset_version_id
        if key in by_target:raise ValueError('SEMANTIC_DUPLICATE_TARGET')
        if targets.get(key)!=candidate.target:raise ValueError('SEMANTIC_TARGET_CHANGED')
        if candidate.state=='proposed':
            if candidate.mask is None or candidate.transform is None or candidate.reason is not None:
                raise ValueError('SEMANTIC_CANDIDATE_STATE_INVALID')
        elif candidate.state=='unsupported':
            if candidate.mask is not None or candidate.transform is not None or candidate.reason is None:
                raise ValueError('SEMANTIC_CANDIDATE_STATE_INVALID')
        elif candidate.reason is None or (candidate.mask is None)!=(candidate.transform is None):
            raise ValueError('SEMANTIC_CANDIDATE_STATE_INVALID')
        by_target[key]=candidate
    if set(by_target)!=set(targets):raise ValueError('SEMANTIC_CANDIDATE_COVERAGE_INVALID')
    plan=SemanticPlan(schema_version=1,intent_sha=params.intent.sha256,snapshot_digest=snapshot.snapshot_digest,
        candidates=[by_target[items[n].binding.asset_version_id] for n in range(len(items))],
        model_lock_sha=params.model_lock_sha,calibration=params.calibration)
    if len(canonical_bytes(plan))>8*1024*1024:raise ValueError('SEMANTIC_PLAN_LIMIT')
    check_active();return plan
