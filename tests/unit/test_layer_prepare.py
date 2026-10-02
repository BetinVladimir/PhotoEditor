from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.images import ColorSpace
from photo_worker.operations.layers.prepare import prepare_layer
from photo_worker.runtime.scratch import ScratchSpace,DiskQuota
from photo_worker.runtime.arrays import read_array

COLOR=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='a'*64)

def scratch(tmp_path,budget=65536):return ScratchSpace(tmp_path,uuid4(),uuid4(),1,budget,min_free_bytes=0)

def test_layer_output_owned_partial_edges_protected_and_float_alpha(tmp_path):
    space=scratch(tmp_path)
    source=np.full((2,257,3),-2,np.float32);source[:,0,0]=-0.
    proposal=np.full_like(source,4);alpha=np.full((2,257),.5,np.float32)
    mask=np.ones((2,257),np.float32);mask[:,0]=0
    protected=np.zeros_like(mask);protected[:,128]=.001
    calls=[]
    def read(x,y,w,h):calls.append((x,y,w,h));return mask[y:y+h,x:x+w]
    try:
        with prepare_layer(source,proposal,read,space,color=COLOR,opacity=.5,blend='normal',
                source_alpha=alpha,proposal_alpha=alpha,read_protected=lambda x,y,w,h:protected[y:y+h,x:x+w],tile_size=256,max_tile_bytes=1<<20) as files:
            assert calls==[(0,0,256,2),(256,0,1,2)] and files.any_changed
            with read_array(files.rgb,(2,257,3),'float32',lambda:None) as result:
                np.testing.assert_array_equal(result[:,0].view(np.uint32),source[:,0].view(np.uint32))
                np.testing.assert_array_equal(result[:,128].view(np.uint32),source[:,128].view(np.uint32))
                np.testing.assert_array_equal(result[:,256],1)
            with read_array(files.alpha,(2,257),'float32',lambda:None,unit_interval=True) as result:np.testing.assert_array_equal(result,alpha)
            changed=np.load(files.changed_mask,allow_pickle=False)
            np.testing.assert_array_equal(changed[:,[0,128]],0);np.testing.assert_array_equal(changed[:,256],1)
            files.reservation.validate_file(files.rgb)
        assert not files.rgb.exists() and not files.alpha.exists() and not files.changed_mask.exists()
    finally:space.cleanup()


def test_zero_effect_no_change_and_absent_alpha(tmp_path):
    space=scratch(tmp_path);source=np.ones((1,1,3),np.float32)
    try:
        with prepare_layer(source,source,lambda *args:np.ones((1,1),np.float32),space,color=COLOR,opacity=0,blend='normal',max_tile_bytes=4096) as files:
            assert not files.any_changed and files.alpha is None
            np.testing.assert_array_equal(np.load(files.changed_mask,allow_pickle=False),0)
    finally:space.cleanup()


def test_mid_frame_failure_and_quota_discard_all_scratch_outputs(tmp_path):
    source=np.ones((2,257,3),np.float32);space=scratch(tmp_path)
    def interrupted(x,y,w,h):
        if x:raise RuntimeError('cancelled after first tile')
        return np.ones((h,w),np.float32)
    try:
        with pytest.raises(RuntimeError,match='cancelled'):
            with prepare_layer(source,source,interrupted,space,color=COLOR,opacity=1,blend='normal',tile_size=256,max_tile_bytes=1<<20):pytest.fail('partial output escaped')
        assert not list(space.directory.rglob('*.npy'))
    finally:space.cleanup()
    space=scratch(tmp_path,1)
    try:
        with pytest.raises(DiskQuota):
            with prepare_layer(source,source,interrupted,space,color=COLOR,opacity=1,blend='normal',tile_size=256,max_tile_bytes=1<<20):pytest.fail('quota bypass')
        assert not list(space.directory.rglob('*.npy'))
    finally:space.cleanup()


@pytest.mark.parametrize('blend,expected',[('normal',0),('multiply',1),('screen',1)])
def test_generated_coverage_partial_edges_and_protected(tmp_path,blend,expected):
    space=scratch(tmp_path);source=np.ones((2,257,3),np.float32)
    protected=np.zeros((2,257),np.float32);protected[:,0]=.001
    calls=[]
    def generated(x,y,w,h):calls.append((x,y,w,h));return np.ones((h,w),np.float32)
    try:
        with prepare_layer(source,source,lambda x,y,w,h:np.ones((h,w),np.float32),space,
                color=COLOR,opacity=1,blend=blend,read_source_generated=generated,
                read_protected=lambda x,y,w,h:protected[y:y+h,x:x+w],tile_size=256,max_tile_bytes=1<<20) as files:
            assert calls==[(0,0,256,2),(256,0,1,2)]
            result=np.load(files.generated_mask,allow_pickle=False)
            np.testing.assert_array_equal(result[:,0],1)
            np.testing.assert_array_equal(result[:,1:],expected)
            files.reservation.validate_file(files.generated_mask)
        assert not files.generated_mask.exists()
    finally:space.cleanup()


def test_identical_proposal_still_carries_generated_dependency(tmp_path):
    space=scratch(tmp_path);source=np.ones((1,1,3),np.float32)
    try:
        with prepare_layer(source,source,lambda *args:np.ones((1,1),np.float32),space,
                color=COLOR,opacity=.5,blend='normal',read_proposal_generated=lambda *args:np.ones((1,1),np.float32),max_tile_bytes=4096) as files:
            assert not files.any_changed
            np.testing.assert_array_equal(np.load(files.generated_mask,allow_pickle=False),1)
    finally:space.cleanup()


def test_invalid_generated_second_tile_discards_all_files(tmp_path):
    space=scratch(tmp_path);source=np.ones((2,257,3),np.float32)
    def generated(x,y,w,h):return np.full((h,w),.5 if x else 1,np.float32)
    try:
        with pytest.raises(ValueError,match='LINEAGE_MASK_NOT_BINARY'):
            with prepare_layer(source,source,lambda x,y,w,h:np.ones((h,w),np.float32),space,
                    color=COLOR,opacity=0,blend='normal',read_source_generated=generated,tile_size=256,max_tile_bytes=1<<20):pytest.fail('invalid lineage escaped')
        assert not list(space.directory.rglob('*.npy'))
    finally:space.cleanup()
