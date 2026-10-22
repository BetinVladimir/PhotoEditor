import numpy as np
import pytest
from photo_worker.operations.semantic.preview_overlay import prepare_preview_mask_overlay


def overlay(mask, **kwargs):
    return prepare_preview_mask_overlay(mask, width=2, height=2, protected=True,
        max_bytes=32*1024**2, check_active=lambda: None, **kwargs)


def test_downsampling_preserves_tiny_protected_region_and_source():
    mask=np.zeros((8,8),dtype=np.float32);mask[3,3]=.001
    before=mask.copy();result=overlay(mask)
    assert result.dtype==np.uint8 and result.shape==(2,2,4)
    assert result[0,0,3]==1 and np.count_nonzero(result[:,:,3])==1
    assert np.array_equal(mask,before)
    assert tuple(result[0,0,:3])==(0,220,255)


@pytest.mark.parametrize('value',[-.1,1.1,np.nan,np.inf])
def test_overlay_rejects_invalid_probabilities(value):
    mask=np.zeros((8,8),dtype=np.float32);mask[7,7]=value
    with pytest.raises(ValueError):overlay(mask)


def test_overlay_rejects_budget_and_propagates_cancellation():
    mask=np.zeros((8,8),dtype=np.float32)
    with pytest.raises(ValueError):prepare_preview_mask_overlay(mask,width=2,height=2,
        protected=False,max_bytes=1,check_active=lambda:None)
    def cancelled():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        prepare_preview_mask_overlay(mask,width=2,height=2,protected=False,
            max_bytes=32*1024**2,check_active=cancelled)


def test_overlay_png_preserves_exact_transparency_and_strips_metadata(tmp_path):
    from PIL import Image
    from photo_worker.operations.semantic.preview_overlay import write_preview_mask_overlay
    mask=np.zeros((8,8),dtype=np.float32);mask[3,3]=.001;mask[7,7]=1
    rgba=overlay(mask);before=rgba.copy();path=tmp_path/'protected.png'
    size=write_preview_mask_overlay(path,rgba,max_bytes=32*1024**2,
        max_output_bytes=1024**2,check_active=lambda:None)
    assert size==path.stat().st_size and np.array_equal(rgba,before)
    with Image.open(path) as image:
        assert image.mode=='RGBA' and not image.info
        assert np.array_equal(np.asarray(image),rgba)


def test_overlay_png_rejects_output_budget_before_writing(tmp_path):
    from photo_worker.operations.semantic.preview_overlay import write_preview_mask_overlay
    path=tmp_path/'protected.png'
    with pytest.raises(ValueError,match='BUDGET'):
        write_preview_mask_overlay(path,np.zeros((2,2,4),dtype=np.uint8),
            max_bytes=32*1024**2,max_output_bytes=1,check_active=lambda:None)
    assert not path.exists()
