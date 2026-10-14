from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.contracts.semantic import SemanticTransferParams,TransferCandidate


def binding():
    return VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),canvas_hash='b'*64)


def test_semantic_transfer_pinned_documents_and_exact_target_roundtrip():
    reference,target=binding(),binding()
    ref=ArtifactRef(artifact_id=uuid4(),sha256='c'*64)
    params=SemanticTransferParams(schema_version=1,intent=ref,reference=reference,target=target,
        model_lock_sha='d'*64,calibration=ref)
    assert SemanticTransferParams.model_validate_json(canonical_bytes(params))==params
    candidate=TransferCandidate(schema_version=1,target=target,mask=None,transform=None,
        confidence=0,state='unsupported',reason='NO_PLANAR_CORRESPONDENCE')
    restored=TransferCandidate.model_validate_json(canonical_bytes(candidate))
    assert restored==candidate and restored.target==params.target


@pytest.mark.parametrize('field,value',[('schema_version',2),('state','approved'),
    ('confidence',float('nan')),('confidence',1.01),('reason',''),('reason','x'*81)])
def test_transfer_candidate_rejects_contract_drift(field,value):
    candidate=dict(schema_version=1,target=binding(),mask=None,transform=None,
        confidence=0,state='unsupported',reason='NO_PLANAR_CORRESPONDENCE')
    candidate[field]=value
    with pytest.raises(ValueError):TransferCandidate.model_validate(candidate)


@pytest.mark.parametrize('kind,ev,valid',[
    ('local_exposure',0,True),('local_exposure',None,False),('remove_object',None,True),
    ('background_cleanup',.5,False),('retouch_region',None,True)])
def test_edit_intent_exposure_is_kind_bound(kind,ev,valid):
    from photo_worker.contracts.semantic import EditIntent
    values=dict(schema_version=1,kind=kind,reference=binding(),
        reference_mask=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),target_snapshot_id=uuid4(),
        strength=.5,ev=ev,preserve_persons=True,prompt=None)
    if valid:
        intent=EditIntent(**values)
        assert EditIntent.model_validate_json(canonical_bytes(intent))==intent
    else:
        with pytest.raises(ValueError,match='EV_INVALID'):EditIntent(**values)


def test_edit_intent_requires_person_preservation():
    from photo_worker.contracts.semantic import EditIntent
    with pytest.raises(ValueError):EditIntent(schema_version=1,kind='remove_object',reference=binding(),
        reference_mask=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),target_snapshot_id=uuid4(),
        strength=.5,ev=None,preserve_persons=False,prompt=None)


@pytest.mark.parametrize('variant',['valid','reference','model','unqualified','zero_error'])
def test_transfer_documents_are_owned_and_calibration_bound(variant):
    from photo_worker.contracts.semantic import bind_semantic_transfer_documents
    params,intent,calibration=transfer_documents()
    target=params.target
    if variant=='reference':intent=intent.model_copy(update={'reference':target})
    elif variant=='model':calibration=calibration.model_copy(update={'model_lock_sha':'f'*64})
    elif variant=='unqualified':calibration=calibration.model_copy(update={'qualified':False})
    elif variant=='zero_error':calibration=calibration.model_copy(update={'match_error_px_max':0})
    if variant!='valid':
        with pytest.raises(ValueError):bind_semantic_transfer_documents(params,intent,calibration)
    else:
        owned=bind_semantic_transfer_documents(params,intent,calibration)
        assert owned==(params,intent,calibration)
        assert all(copy is not original for copy,original in zip(owned,(params,intent,calibration)))


def transfer_documents():
    from photo_worker.contracts.semantic import EditIntent
    from photo_worker.operations.session.contracts import Calibration
    reference=binding();target=binding();ref=ArtifactRef(artifact_id=uuid4(),sha256='c'*64)
    params=SemanticTransferParams(schema_version=1,intent=ref,reference=reference,target=target,
        model_lock_sha='d'*64,calibration=ref)
    intent=EditIntent(schema_version=1,kind='local_exposure',reference=reference,reference_mask=ref,
        target_snapshot_id=uuid4(),strength=.5,ev=.3,preserve_persons=True,prompt=None)
    calibration=Calibration(schema_version=1,model_lock_sha='d'*64,dataset_sha='e'*64,qualified=True,
        scene_cosine_min=.7,scene_log_luma_max=1,cluster_diameter_max=.3,sharpness_scale=1,
        clipping_penalty=1,match_inlier_min=.8,match_error_px_max=3,identity_similarity_min=.7,
        landmark_drift_max=.1,normalization_scales=[1.]*6,loss_weights=[1/6]*6,
        focus_warning=.5,focus_critical=.2,eye_open_min=.5,glare_warning_fraction=.2,
        clipping_warning_fraction=.1,clipping_critical_fraction=.3,min_face_edge_px=32,
        depth_sigma=.1,registration_error_px=2,motion_threshold=.1)
    return params,intent,calibration
