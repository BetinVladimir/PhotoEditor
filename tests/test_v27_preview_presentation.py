from photo_worker.contracts.base import canonical_bytes
import json
from pathlib import Path
import numpy as np
import pytest
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.semantic.preview_presentation import prepare_exposure_display


def source():
    path=Path(__file__).resolve().parents[1]/'backend/internal/review/testdata/working-images-python.json'
    image=WorkingImage.model_validate_json(canonical_bytes(json.loads(path.read_text())[0]))
    rgb=np.zeros((image.canvas.height,image.canvas.width,3),dtype=np.float32)
    alpha=np.ones(rgb.shape[:2],dtype=np.float32) if image.alpha is not None else None
    return image,rgb,alpha


@pytest.mark.parametrize('accepted',[False,None,1])
def test_display_requires_explicit_qualified_runtime_before_profile_io(tmp_path,accepted):
    image,rgb,alpha=source()
    with pytest.raises(ValueError,match='UNQUALIFIED'):
        prepare_exposure_display(image,rgb,alpha,width=1,height=1,icc_root=tmp_path,
            icc_lock_sha='a'*64,srgb_sha='b'*64,verify_runtime=lambda:accepted,
            check_active=lambda:None,max_bytes=512*rgb.shape[0]*rgb.shape[1]+64*1024**2)


def test_display_rejects_missing_budget_before_runtime_or_io(tmp_path):
    image,rgb,alpha=source()
    def forbidden():raise AssertionError('runtime reached before budget admission')
    with pytest.raises(ValueError,match='INVALID'):
        prepare_exposure_display(image,rgb,alpha,width=1,height=1,icc_root=tmp_path,
            icc_lock_sha='a'*64,srgb_sha='b'*64,verify_runtime=forbidden,
            check_active=lambda:None,max_bytes=1)


@pytest.mark.parametrize('has_alpha',[False,True])
def test_display_png_preserves_pixels_alpha_and_exact_icc(tmp_path,has_alpha):
    from hashlib import sha256
    from PIL import Image
    from photo_worker.operations.semantic.preview_presentation import ExposureDisplay,write_exposure_display
    rgb=np.array([[[0.,.5,1.],[1.,0.,.25]]],dtype=np.float32)
    alpha=np.array([[0.,.5]],dtype=np.float32) if has_alpha else None
    profile=b'unit-test-pinned-profile';digest=sha256(profile).hexdigest()
    display=ExposureDisplay(rgb,alpha,profile,digest);path=tmp_path/'display.png'
    size=write_exposure_display(path,display,srgb_sha=digest,max_bytes=32*1024**2,
        max_output_bytes=1024**2,verify_runtime=lambda:True,check_active=lambda:None)
    assert size==path.stat().st_size
    expected=np.round(rgb*255).astype(np.uint8)
    if has_alpha:expected=np.dstack((expected,np.round(alpha*255).astype(np.uint8)))
    with Image.open(path) as image:
        assert image.info=={'icc_profile':profile}
        assert np.array_equal(np.asarray(image),expected)
