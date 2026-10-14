from uuid import uuid4
import numpy as np
import pytest
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure
from photo_worker.operations.semantic.prepare_transfer import prepare_planar_mask_transfer,planar_mask_resources


@pytest.mark.parametrize('variant',['success','overlap','cancel','consumer','ram','scratch','source_empty','target_empty'])
def test_planar_mask_owner_cleanup_and_admission(tmp_path,variant):
    source=np.full((16,32),.4,np.float32);zero=np.zeros_like(source)
    protected=zero.copy()
    if variant=='overlap':protected[8,8]=.001
    if variant=='source_empty':source[:]=0
    matrix=np.eye(3)
    if variant=='target_empty':matrix[0,2]=1000
    costs=planar_mask_resources(source.shape,source.shape)
    estimate=dict(costs)
    if variant in ('ram','scratch'):estimate[variant+'_bytes']-=1
    cancelled=False
    def active():
        if cancelled:raise InterruptedError('cancelled')
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:
        def consume():
            nonlocal cancelled
            with prepare_planar_mask_transfer(source,matrix,source.shape,
                    source_protected=zero,target_protected=protected,scratch=scratch,
                    execution_profile={'resource_estimate':estimate},check_active=active) as result:
                assert result.path.exists() and not result.pixels.flags.writeable
                np.testing.assert_array_equal(result.pixels,source)
                assert result.source_pixels==result.target_pixels==512
                if variant=='consumer':raise RuntimeError('consumer')
                cancelled=variant=='cancel'
        if variant in ('source_empty','target_empty'):
            with pytest.raises(ValueError,match='SEMANTIC_MASK_EMPTY'):consume()
        elif variant=='overlap':
            with pytest.raises(ValueError,match='PROTECTED_OVERLAP'):consume()
        elif variant=='cancel':
            with pytest.raises(InterruptedError):consume()
        elif variant=='consumer':
            with pytest.raises(RuntimeError,match='consumer'):consume()
        elif variant in ('ram','scratch'):
            with pytest.raises(OperationFailure,match='ADMISSION_EXCEEDED'):consume()
        else:consume()
        assert not list(scratch.directory.iterdir())
    np.testing.assert_array_equal(source,np.full((16,32),0 if variant=='source_empty' else .4,np.float32))


def test_mask_stage_retains_measured_partial_target_support(tmp_path):
    source=np.ones((16,32),np.float32);zero=np.zeros_like(source)
    matrix=np.eye(3);matrix[0,2]=16
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:
        with prepare_planar_mask_transfer(source,matrix,source.shape,source_protected=zero,
                target_protected=zero,scratch=scratch,
                execution_profile={'resource_estimate':planar_mask_resources(source.shape,source.shape)}) as result:
            assert result.source_pixels==512 and result.target_pixels==256
            assert np.count_nonzero(result.pixels)==256
        assert not list(scratch.directory.iterdir())


@pytest.mark.parametrize('variant',['success','blank','scene','budget','cancel'])
def test_complete_planar_prepare_geometry_mask_and_scene_gate(tmp_path,variant):
    import cv2
    from photo_worker.operations.semantic.prepare_transfer import prepare_planar_transfer,planar_transfer_resources
    rng=np.random.default_rng(106)
    luma=rng.random((256,256),dtype=np.float32)
    rgb=np.repeat(luma[:,:,None],3,axis=2)
    if variant=='blank':rgb[:]=.5
    target=cv2.warpAffine(rgb,np.array([[1,0,4],[0,1,6]],float),(256,256))
    mask=np.zeros((256,256),np.float32);mask[50:100,60:110]=1
    protected=np.zeros_like(mask);calls=[];cancelled=False
    costs=planar_transfer_resources(mask.shape,mask.shape)
    if variant=='budget':costs['ram_bytes']-=1
    def scene():
        calls.append('scene')
        if variant=='scene':raise ValueError('OCCLUSION_UNSUPPORTED')
    def active():
        if cancelled:raise InterruptedError('cancelled')
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:
        def consume():
            nonlocal cancelled
            with prepare_planar_transfer(rgb,target,mask,source_protected=protected,target_protected=protected,
                    scratch=scratch,execution_profile={'resource_estimate':costs},match_error_px_max=.5,
                    match_inlier_min=.8,verify_scene=scene,check_active=active) as result:
                if variant=='blank':
                    assert result.mask is None and result.match.reason=='NO_PLANAR_CORRESPONDENCE'
                else:
                    assert result.match.reason is None and result.mask.target_pixels>0
                    assert not result.mask.pixels.flags.writeable
                cancelled=variant=='cancel'
        if variant=='scene':
            with pytest.raises(ValueError,match='OCCLUSION_UNSUPPORTED'):consume()
        elif variant=='budget':
            with pytest.raises(OperationFailure,match='RAM_ADMISSION_EXCEEDED'):consume()
            assert calls==[]
        elif variant=='cancel':
            with pytest.raises(InterruptedError):consume()
        else:consume()
        assert not list(scratch.directory.iterdir())


def test_calibrated_context_owns_documents_before_scene_callback(tmp_path):
    from test_semantic_transfer_contracts import transfer_documents
    from photo_worker.operations.semantic.prepare_transfer import prepare_calibrated_planar_transfer,planar_transfer_resources
    params,intent,calibration=transfer_documents()
    rgb=np.full((64,64,3),.5,np.float32);mask=np.ones((64,64),np.float32);zero=np.zeros_like(mask)
    profile={'resource_estimate':planar_transfer_resources(mask.shape,mask.shape)}
    original_target=params.target
    calls=[]
    def verify(p,i,c):
        calls.append((p.target,i.reference,c.match_error_px_max))
        # Neither callback copies nor mutation of outer originals change processing.
        c.match_error_px_max=0;calibration.match_error_px_max=0
        profile['resource_estimate']['ram_bytes']=0
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:
        with prepare_calibrated_planar_transfer(params,intent,calibration,rgb,rgb,mask,
                source_protected=zero,target_protected=zero,scratch=scratch,execution_profile=profile,
                verify_scene=verify) as result:
            assert result.mask is None and result.match.reason=='NO_PLANAR_CORRESPONDENCE'
        assert not list(scratch.directory.iterdir())
    assert calls[0][0]==original_target and calls[0][2]==3
