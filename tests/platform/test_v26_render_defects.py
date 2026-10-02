from types import SimpleNamespace
from uuid import uuid4
import numpy as np
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_defects import prepare_defect_stage
from test_repair_prepare import fixture as legacy_fixture


def test_review_bound_real_label_repair_and_evidence(tmp_path):
    a,ref,proposal,review,labels,rgb,protected=fixture(tmp_path)
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with prepare_defect_stage(SimpleNamespace(scratch=scratch,input=a.input,contract=a.contract),a.image_ref,a.image,AdjustmentPixels(rgb,None),
                ref,proposal,review,labels,protected,strength=1,protection_ref=a.protection_ref,review_ref=a.review_ref,execution_profile={'resource_estimate':dict(pixel_count=labels.size,ram_bytes=8*1024**3,scratch_bytes=8*1024**3,vram_bytes=0,output_bytes=8*1024**3)}) as result:
            np.testing.assert_array_equal(result.pixels.rgb[labels!=2],rgb[labels!=2])
            assert np.mean(result.pixels.rgb[labels==2])>.3
            assert result.candidates[0].applied_pixels==9
        assert not list(scratch.directory.iterdir())


def test_stale_review_cannot_reach_repair_allocation(tmp_path):
    import pytest
    from photo_worker.contracts.base import ArtifactRef
    a,ref,proposal,review,labels,rgb,protected=fixture(tmp_path)
    review.label_map=ArtifactRef(artifact_id=uuid4(),sha256='d'*64)
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(ValueError,match='STALE_DEFECT_REVIEW'):
            with prepare_defect_stage(SimpleNamespace(scratch=scratch,input=a.input,contract=a.contract),a.image_ref,a.image,AdjustmentPixels(rgb,None),
                    ref,proposal,review,labels,protected,strength=1,protection_ref=a.protection_ref,review_ref=a.review_ref,execution_profile={'resource_estimate':dict(pixel_count=labels.size,ram_bytes=8*1024**3,scratch_bytes=8*1024**3,vram_bytes=0,output_bytes=8*1024**3)}):pass
        assert not list(scratch.directory.iterdir())


def test_repair_bridge_consumer_failure_cleans_mapped_output(tmp_path):
    import pytest
    a,ref,proposal,review,labels,rgb,protected=fixture(tmp_path)
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(RuntimeError,match='consumer'):
            with prepare_defect_stage(SimpleNamespace(scratch=scratch,input=a.input,contract=a.contract),a.image_ref,a.image,AdjustmentPixels(rgb,None),
                    ref,proposal,review,labels,protected,strength=1,protection_ref=a.protection_ref,review_ref=a.review_ref,execution_profile={'resource_estimate':dict(pixel_count=labels.size,ram_bytes=8*1024**3,scratch_bytes=8*1024**3,vram_bytes=0,output_bytes=8*1024**3)}):raise RuntimeError('consumer')
        assert not list(scratch.directory.iterdir())


def test_defect_strength_derived_once_without_changing_policy_or_preservation():
    from photo_worker.contracts.style import Defects
    from photo_worker.v2.render_defects import render_defect_params
    requested=Defects(enabled=True,algorithm='defectrepair_telea_v1',policy='review',strength=.7,
        diameter_face_fraction=[.002,.012],preserve_permanent_marks=True)
    result=render_defect_params(SimpleNamespace(effective=SimpleNamespace(defects=requested,strength=.4)))
    assert result.strength==.7*.4 and requested.strength==.7
    assert result.policy=='review' and result.preserve_permanent_marks
    assert result.diameter_face_fraction==requested.diameter_face_fraction


def test_repair_resources_follow_only_accepted_support_windows(tmp_path):
    from photo_worker.v2.render_defects import defect_provider_peak
    import pytest
    a,ref,proposal,review,labels,rgb,protected=fixture(tmp_path)
    peak=defect_provider_peak(a.image_ref,a.image,ref,proposal,review)
    assert peak['scratch_bytes']==30*labels.size+32768 and peak['ram_bytes']>peak['scratch_bytes']
    with pytest.raises(ValueError,match='ROI_BUDGET_EXCEEDED'):
        defect_provider_peak(a.image_ref,a.image,ref,proposal,review,max_roi_pixels=1)


def fixture(tmp_path,alpha=None):
    from photo_worker.contracts.defects import candidate_id
    a,ref,proposal,review,labels,rgb,protected=legacy_fixture(tmp_path)
    accepted={c.label for c in proposal.candidates if c.id in review.accepted_candidate_ids}
    a.image.pixels=a.array(tmp_path,'review-current',rgb)
    a.image.alpha=a.array(tmp_path,'review-alpha',alpha) if alpha is not None else None
    a.image_ref=a.add(a.image)
    values=proposal.model_dump(mode='python')
    values['image']=a.image_ref;values['source_pixels']=a.image.pixels
    values['label_map']=a.array(tmp_path,'review-labels',labels)
    for candidate in values['candidates']:
        from photo_worker.contracts.images import Rect
        candidate['id']=candidate_id(a.image.pixels,values['label_map'],candidate['label'],Rect.model_validate(candidate['bbox']),candidate['diameter'])
    proposal=type(proposal).model_validate(values)
    ref=a.add(proposal)
    review.image=a.image_ref;review.proposal=ref;review.label_map=proposal.label_map
    review.accepted_candidate_ids=[c.id for c in proposal.candidates if c.label in accepted]
    from photo_worker.contracts.images import MaskRef
    mask=MaskRef(artifact=a.array(tmp_path,'review-protection',protected),canvas=a.image.canvas,
        transform_chain=a.image.transform_chain,selector='manual',confidence=1,model_lock_id=None,protected=True)
    a.protection_ref=a.add(mask)
    a.review_ref=a.add(review)
    return a,ref,proposal,review,labels,rgb,protected


def test_modified_current_pixels_cannot_reuse_approved_review(tmp_path):
    import pytest
    a,ref,proposal,review,labels,rgb,protected=fixture(tmp_path)
    rgb=rgb.copy();rgb[0,0,0]=.25
    root=tmp_path/'scratch';root.mkdir()
    profile={'resource_estimate':dict(pixel_count=labels.size,ram_bytes=8*1024**3,scratch_bytes=8*1024**3,vram_bytes=0,output_bytes=8*1024**3)}
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(ValueError,match='CURRENT_PIXELS_CHANGED'):
            with prepare_defect_stage(SimpleNamespace(scratch=scratch,input=a.input,contract=a.contract),a.image_ref,a.image,
                    AdjustmentPixels(rgb,None),ref,proposal,review,labels,protected,strength=1,protection_ref=a.protection_ref,review_ref=a.review_ref,
                    execution_profile=profile):pass
        assert not list(scratch.directory.iterdir())


def test_changed_label_pixels_cannot_reuse_review(tmp_path):
    import pytest
    a,ref,proposal,review,labels,rgb,protected=fixture(tmp_path)
    labels=labels.copy();labels[0,0]=2
    root=tmp_path/'scratch';root.mkdir()
    profile={'resource_estimate':dict(pixel_count=labels.size,ram_bytes=8*1024**3,scratch_bytes=8*1024**3,vram_bytes=0,output_bytes=8*1024**3)}
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(ValueError,match='LABEL_MAP_CHANGED'):
            with prepare_defect_stage(SimpleNamespace(scratch=scratch,input=a.input,contract=a.contract),a.image_ref,a.image,
                    AdjustmentPixels(rgb,None),ref,proposal,review,labels,protected,strength=1,protection_ref=a.protection_ref,review_ref=a.review_ref,
                    execution_profile=profile):pass
        assert not list(scratch.directory.iterdir())


def test_protection_pixels_cannot_be_substituted(tmp_path):
    import pytest
    a,ref,proposal,review,labels,rgb,protected=fixture(tmp_path)
    protected=protected.copy();protected[0,0]=.001
    root=tmp_path/'scratch';root.mkdir()
    profile={'resource_estimate':dict(pixel_count=labels.size,ram_bytes=8*1024**3,scratch_bytes=8*1024**3,vram_bytes=0,output_bytes=8*1024**3)}
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(ValueError,match='PROTECTION_CHANGED'):
            with prepare_defect_stage(SimpleNamespace(scratch=scratch,input=a.input,contract=a.contract),
                    a.image_ref,a.image,AdjustmentPixels(rgb,None),ref,proposal,review,labels,protected,
                    strength=1,protection_ref=a.protection_ref,review_ref=a.review_ref,execution_profile=profile):pass
        assert not list(scratch.directory.iterdir())


def test_valid_in_memory_review_cannot_replace_pinned_review_document(tmp_path):
    import pytest
    a,ref,proposal,review,labels,rgb,protected=fixture(tmp_path)
    changed=review.model_copy(deep=True);changed.accepted_candidate_ids=[]
    root=tmp_path/'scratch';root.mkdir()
    profile={'resource_estimate':dict(pixel_count=labels.size,ram_bytes=8*1024**3,scratch_bytes=8*1024**3,vram_bytes=0,output_bytes=8*1024**3)}
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(ValueError,match='DOCUMENT_CHANGED'):
            with prepare_defect_stage(SimpleNamespace(scratch=scratch,input=a.input,contract=a.contract),
                    a.image_ref,a.image,AdjustmentPixels(rgb,None),ref,proposal,changed,labels,protected,
                    strength=1,protection_ref=a.protection_ref,review_ref=a.review_ref,execution_profile=profile):pass
        assert not list(scratch.directory.iterdir())


def test_insufficient_resources_reject_before_any_declared_artifact_io(tmp_path):
    import pytest
    from photo_worker.runtime.executor import OperationFailure
    a,ref,proposal,review,labels,rgb,protected=fixture(tmp_path)
    calls=[]
    def forbidden(*args,**kwargs):calls.append('io');raise AssertionError('IO before admission')
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
            with prepare_defect_stage(SimpleNamespace(scratch=scratch,input=forbidden,contract=forbidden),
                    a.image_ref,a.image,AdjustmentPixels(rgb,None),ref,proposal,review,labels,protected,
                    strength=1,protection_ref=a.protection_ref,review_ref=a.review_ref,
                    execution_profile={'resource_estimate':{}}):pass
        assert calls==[] and not list(scratch.directory.iterdir())


def test_approved_alpha_is_preserved_and_substitution_rejected(tmp_path):
    import pytest
    from photo_worker.v2.render_defects import defect_provider_peak
    source=tmp_path/'source';source.mkdir()
    _,_,_,_,labels,_,_=legacy_fixture(source)
    alpha=np.full(labels.shape,.75,np.float32);alpha[0,0]=-0.0
    inputs=tmp_path/'inputs';inputs.mkdir()
    a,ref,proposal,review,labels,rgb,protected=fixture(inputs,alpha=alpha)
    peak=defect_provider_peak(a.image_ref,a.image,ref,proposal,review)
    assert peak['ram_bytes']>=40*labels.size+128*1024**2
    root=tmp_path/'scratch';root.mkdir()
    profile={'resource_estimate':dict(pixel_count=labels.size,ram_bytes=8*1024**3,scratch_bytes=8*1024**3,vram_bytes=0,output_bytes=8*1024**3)}
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        for supplied in (alpha,None,np.ones(labels.shape,np.float32),alpha.astype(np.float64)):
            def prepare():
                return prepare_defect_stage(SimpleNamespace(scratch=scratch,input=a.input,contract=a.contract),
                    a.image_ref,a.image,AdjustmentPixels(rgb,supplied),ref,proposal,review,labels,protected,
                    strength=1,protection_ref=a.protection_ref,review_ref=a.review_ref,execution_profile=profile)
            if supplied is alpha:
                with prepare() as result:
                    assert result.pixels.alpha is alpha
                    np.testing.assert_array_equal(result.pixels.alpha.view(np.uint32),alpha.view(np.uint32))
            else:
                with pytest.raises(ValueError,match='CURRENT_ALPHA_CHANGED'):
                    with prepare():pass
            assert not list(scratch.directory.iterdir())


def test_alpha_cannot_be_added_to_reviewed_opaque_image(tmp_path):
    import pytest
    a,ref,proposal,review,labels,rgb,protected=fixture(tmp_path)
    root=tmp_path/'scratch';root.mkdir()
    profile={'resource_estimate':dict(pixel_count=labels.size,ram_bytes=8*1024**3,scratch_bytes=8*1024**3,vram_bytes=0,output_bytes=8*1024**3)}
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(ValueError,match='CURRENT_ALPHA_CHANGED'):
            with prepare_defect_stage(SimpleNamespace(scratch=scratch,input=a.input,contract=a.contract),
                    a.image_ref,a.image,AdjustmentPixels(rgb,np.ones(labels.shape,np.float32)),ref,proposal,review,labels,protected,
                    strength=1,protection_ref=a.protection_ref,review_ref=a.review_ref,execution_profile=profile):pass
        assert not list(scratch.directory.iterdir())
