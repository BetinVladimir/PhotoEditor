from dataclasses import replace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.algorithms.portrait_donor_patch import prepare_donor_patch
from photo_worker.operations.portrait.donor_files import write_donor_patch_files
from photo_worker.runtime.scratch import ScratchSpace
from test_portrait_donor_patch import inputs


def test_patch_files_preserve_outside_canvas_and_exact_changed_mask(tmp_path):
    args=inputs();patch=prepare_donor_patch(*args,80,1,(0,0),lambda:None)
    source=np.ones((70,80,3),np.float32);source[0,0,0]=-0.
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,1_000_000,min_free_bytes=0) as scratch:
        with write_donor_patch_files(source,patch,(10,15),scratch,target=args[4],protected=args[5],check_active=lambda:None) as files:
            pixels=np.load(files.rgb,allow_pickle=False)
            mask=np.load(files.changed_mask,allow_pickle=False)
            expected=source.copy();expected[15:55,10:50]=patch.pixels
            np.testing.assert_array_equal(pixels.view(np.uint32),expected.view(np.uint32))
            expected_mask=np.zeros((70,80),np.float32);expected_mask[15:55,10:50]=patch.changed_mask
            np.testing.assert_array_equal(mask,expected_mask)
            assert files.review_required


def test_forged_changed_mask_rejected_before_reservation(tmp_path):
    args=inputs();patch=prepare_donor_patch(*args,80,1,(0,0),lambda:None)
    forged=replace(patch,changed_mask=np.zeros((40,40),np.float32))
    with pytest.raises(ValueError,match='DONOR_CHANGED_MASK'):
        with write_donor_patch_files(args[0],forged,(0,0),None,target=args[4],protected=args[5],check_active=lambda:None):
            pytest.fail('Forged mask produced files')


@pytest.mark.parametrize('violation',['outside','protected'])
def test_changed_pixels_outside_effective_mask_rejected_before_io(violation):
    args=inputs();patch=prepare_donor_patch(*args,80,1,(0,0),lambda:None)
    target=args[4].copy();guard=args[5].copy()
    if violation=='outside':target[15,15]=0
    else:guard[15,15]=np.float32(1e-8)
    with pytest.raises(ValueError,match='DONOR_OUTSIDE_MASK'):
        with write_donor_patch_files(args[0],patch,(0,0),None,target=target,protected=guard,check_active=lambda:None):
            pytest.fail('Unauthorized pixel change produced files')
