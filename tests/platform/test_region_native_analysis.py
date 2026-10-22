from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.face import FaceBoxes,FaceLandmarks
from photo_worker.operations.region.analysis import load_region_native_analysis
from test_region_preview_presentation import fixture


def analyses():
    params,raw=fixture(False)
    source=params.validate_source(raw)
    boxes_ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    landmarks_ref=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    q=SimpleNamespace(model_lock_sha=params.frozen_recipe.model_lock_sha,backend='mps')
    boxes=FaceBoxes(image=params.source.ref,faces=[],model_id='sfd',model_lock_id=q.model_lock_sha,
        backend='mps',preprocessing_id='sfd_long1024_bgr_v1',preview_width=400,preview_height=300,
        candidate_count=0,accepted_count=0,alpha_excluded_fraction=0.,warnings=['no_faces'])
    landmarks=FaceLandmarks(image=params.source.ref,faces=boxes_ref,landmarks=[],model_id='fan2d',
        model_lock_id=q.model_lock_sha,backend='mps',preprocessing_id='fan2d_crop256_v1',
        mean_confidence=0.,warnings=['no_faces'])
    documents={'FaceBoxes':[(boxes_ref,boxes)],'FaceLandmarks':[(landmarks_ref,landmarks)],'MaskRef':[]}
    class Artifacts:
        def declared_contract_refs(self,contract,maximum):return tuple(ref for ref,_ in documents.get(contract,[]))
        def contract(self,ref,model):
            return next(doc.model_copy(deep=True) for entries in documents.values() for candidate,doc in entries if candidate==ref)
    return params,source,q,documents,Artifacts()


def test_native_region_noface_inventory_is_explicit_and_source_bound():
    params,source,q,documents,artifacts=analyses()
    result=load_region_native_analysis(artifacts,params.source.ref,source,q,require_faces=True)
    assert result.faces==() and result.skin==()
    assert result.detection[0]==documents['FaceBoxes'][0][0]


@pytest.mark.parametrize('defect',['source','backend','model','detection','ambiguous','truncated'])
def test_native_region_analysis_refuses_unbound_or_ambiguous_evidence(defect):
    params,source,q,documents,artifacts=analyses()
    boxes=documents['FaceBoxes'][0][1]
    landmarks=documents['FaceLandmarks'][0][1]
    if defect=='source':landmarks.image=ArtifactRef(artifact_id=uuid4(),sha256='c'*64)
    if defect=='backend':landmarks.backend='cuda'
    if defect=='model':landmarks.model_lock_id='0'*64
    if defect=='detection':landmarks.faces=ArtifactRef(artifact_id=uuid4(),sha256='c'*64)
    if defect=='ambiguous':documents['FaceBoxes'].append((ArtifactRef(artifact_id=uuid4(),sha256='d'*64),boxes))
    if defect=='truncated':boxes.warnings=['no_faces','faces_truncated']
    with pytest.raises(ValueError,match='REGION_FACE_ANALYSIS_UNAVAILABLE'):
        load_region_native_analysis(artifacts,params.source.ref,source,q,require_faces=True)
