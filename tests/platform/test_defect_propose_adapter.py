from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.images import Face68,MaskRef
from photo_worker.contracts.face import FaceLandmarks
from photo_worker.contracts.defects import DefectReview,accepted_candidates
from photo_worker.algorithms.defect_labels import validate_labels
from photo_worker.operations.defects.propose import execute
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_mask_raster_adapter import MaskArtifacts
from test_image_color_adapter import context
from test_propose_prepare import fixture


def setup(root):
    a=MaskArtifacts(root);rgb,image,faces=fixture()
    a.image=image.model_copy(update={'pixels':a.array(root,'proposal-source',rgb)})
    a.image_ref=a.add(a.image);bindings=[]
    for i,entry in enumerate(faces):
        mask=MaskRef(artifact=a.array(root,'proposal-skin-'+str(i),entry.skin_probability),canvas=image.canvas,
            transform_chain=image.transform_chain,selector='skin',confidence=1.,model_lock_id=None,
            object_id=entry.face.object_id,protected=False)
        bindings.append(dict(landmarks=a.add(entry.face).model_dump(mode='json'),skin=mask.model_dump(mode='json')))
    a.raw=dict(image=a.image_ref.model_dump(mode='json'),faces=bindings)
    ctx=context(a);ctx.lease.command.inputs[:]=[r for r in ctx.lease.command.inputs if r is not None]
    ctx.lease.execution_profile['resource_estimate']['ram_bytes']=1073741824
    def require_declared(ref):
        if ref not in ctx.lease.command.inputs:
            raise ValueError('Artifact is not a declared task input')
    a.require_declared_input=require_declared
    return a,ctx


def test_proposals_publish_bound_candidates_and_explicit_review(tmp_path):
    a,ctx=setup(tmp_path);root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,12*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    proposal=a.staged[0][1];report=a.staged[1][1]
    assert len(result.outputs)==5 and result.review_required and proposal.candidates
    assert all(c.decision=='pending' for c in proposal.candidates)
    validate_labels(a.outputs['defect_labels'],proposal)
    assert np.array_equal(a.outputs['defect_labels_png'],a.outputs['defect_labels'])
    assert not np.any(a.outputs['defect_labels'][a.outputs['defect_protection']>0])
    assert report.protection.protected and report.proposal==result.outputs[3]
    assert 'overlapping_candidates' in report.warnings
    review=DefectReview(proposal=report.proposal,image=a.image_ref,label_map=proposal.label_map,revision=1,
        accepted_candidate_ids=[proposal.candidates[0].id],rejected_candidate_ids=[])
    assert accepted_candidates(report.proposal,proposal,review,a.image_ref,a.image)==(proposal.candidates[0],)
    assert ctx.snapshot_progress()==dict(done=3,total=3)


def test_wrong_mask_object_prevents_proposal_uploads(tmp_path):
    a,ctx=setup(tmp_path);a.raw['faces'][0]['skin']['object_id']=str(uuid4())
    with pytest.raises(OperationFailure) as error:execute(ctx,a)
    assert error.value.error.code=='INPUT_INVALID' and not a.uploads


def test_proposals_accept_bound_landmark_collection(tmp_path):
    a,ctx=setup(tmp_path)
    individual=[value for value in a.contracts.values() if isinstance(value,Face68)]
    collection=FaceLandmarks(image=a.image_ref,faces=a.image_ref,landmarks=individual,model_id='fan2d',
        model_lock_id='a'*64,backend='cpu',preprocessing_id='fan2d_crop256_v1',mean_confidence=.9,warnings=[])
    collection_ref=a.add(collection)
    ctx.lease.command.inputs.append(collection_ref)
    for entry,face in zip(a.raw['faces'],individual):
        entry['landmarks']=collection_ref.model_dump(mode='json');entry['object_id']=str(face.object_id)
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,12*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    assert result.review_required and a.staged[0][1].candidates


@pytest.mark.parametrize('corruption', ['feature', 'candidate', 'partial_permanent', 'additional_protection'])
def test_materialized_protection_rejected_before_upload(tmp_path, monkeypatch, corruption):
    from contextlib import contextmanager
    import photo_worker.operations.defects.propose as operation
    a, ctx = setup(tmp_path)
    if corruption in ('partial_permanent','additional_protection'):
        permanent = np.full((512,512), .25, np.float32)
        mask = MaskRef(artifact=a.array(tmp_path,'permanent',permanent),canvas=a.image.canvas,
            transform_chain=a.image.transform_chain,selector='manual',confidence=1.,model_lock_id=None,protected=True)
        if corruption=='additional_protection':a.raw['protections']=[mask.model_dump(mode='json')]
        else:a.raw['permanent'] = mask.model_dump(mode='json')
        ctx.lease.command.inputs.append(mask.artifact)
    original = operation.prepare_proposals
    @contextmanager
    def tampered(*args, **kwargs):
        with original(*args, **kwargs) as files:
            protection = np.load(files.protection, mmap_mode='r+', allow_pickle=False)
            try:
                if corruption in ('partial_permanent','additional_protection'):
                    protection[0,0] = .125
                else:
                    ys, xs = np.where(protection == 1)
                    assert len(xs)
                    y, x = int(ys[0]), int(xs[0])
                    if corruption == 'feature':
                        protection[y,x] = 0
                    else:
                        labels = np.load(files.labels, mmap_mode='r+', allow_pickle=False)
                        try:
                            labels[y,x] = 1
                            labels.flush()
                        finally:
                            labels._mmap.close()
                protection.flush()
            finally:
                protection._mmap.close()
            yield files
    monkeypatch.setattr(operation, 'prepare_proposals', tampered)
    root = tmp_path/'scratch'; root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,12*1024**2,min_free_bytes=0) as scratch:
        a.scratch = scratch
        with pytest.raises(OperationFailure) as failure:
            execute(ctx,a)
        assert failure.value.error.code == 'INPUT_INVALID'
        assert not a.uploads and not a.staged
        assert not list(scratch.directory.iterdir())


def test_declared_protections_are_max_unioned_before_publish(tmp_path):
    a,ctx=setup(tmp_path)
    arrays=[];masks=[]
    for i,value in enumerate((.25,.5)):
        array=np.full((512,512),value,np.float32)
        if i==0:array[0,0]=.75
        mask=MaskRef(artifact=a.array(tmp_path,'protection-'+str(i),array),canvas=a.image.canvas,
            transform_chain=a.image.transform_chain,selector='manual',confidence=1.,model_lock_id=None,protected=True)
        arrays.append(array);masks.append(mask)
        ctx.lease.command.inputs.append(mask.artifact)
    a.raw['protections']=[m.model_dump(mode='json') for m in masks]
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,12*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    protected=a.outputs['defect_protection']
    assert protected[0,0]==.75 and protected[-1,-1]==.5
    assert np.all(protected>=np.maximum(*arrays))
    assert not np.any(a.outputs['defect_labels'])
    assert len(result.outputs)==5


def test_undeclared_protection_prevents_upload(tmp_path):
    a,ctx=setup(tmp_path)
    mask=MaskRef(artifact=a.array(tmp_path,'undeclared-protection',np.zeros((512,512),np.float32)),
        canvas=a.image.canvas,transform_chain=a.image.transform_chain,selector='manual',
        confidence=1.,model_lock_id=None,protected=True)
    a.raw['protections']=[mask.model_dump(mode='json')]
    with pytest.raises(OperationFailure) as failure:execute(ctx,a)
    assert failure.value.error.code=='INPUT_INVALID' and not a.uploads


def test_original_protection_field_presence_survives_param_serialization():
    from pathlib import Path
    import json
    from photo_worker.contracts.defects import DefectProposeParams
    from photo_worker.contracts.base import canonical_bytes
    root=Path(__file__).resolve().parents[2]
    original=json.loads((root/'backend/internal/review/testdata/proposal-report-python.json').read_text())['params']
    parsed=DefectProposeParams.model_validate_json(json.dumps(original))
    assert parsed.protections==[]
    assert canonical_bytes(parsed)==canonical_bytes(original)
    explicit=dict(original,protections=[])
    parsed=DefectProposeParams.model_validate_json(json.dumps(explicit))
    assert canonical_bytes(parsed)==canonical_bytes(explicit)
