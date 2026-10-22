import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.semantic.exposure_quality import measure_exposure_quality
from photo_worker.operations.optical.color import to_working_linear
from photo_worker.operations.semantic.local_exposure import apply_local_exposure
from photo_worker.operations.semantic.exposure_encoding import restore_local_exposure_encoding
from test_v27_exposure_source import source_fixture


@pytest.mark.parametrize('failure',[None,'outside','inside','overlap','budget','icc'])
def test_measured_report_requires_exact_exposure(failure):
    edit,image,_=source_fixture();color=WorkingImage.model_validate_json(canonical_bytes(image)).color
    before=np.full((1,2,3),1.2,np.float32);mask=np.array([[0.,.5]],np.float32);protected=np.zeros((1,2),np.float32)
    linear=to_working_linear(before,color)
    result=apply_local_exposure(linear,mask,protected,ev=edit['ev'],strength=edit['strength'],max_bytes=1024)
    after=restore_local_exposure_encoding(before,result,mask,protected,color,max_bytes=1024,check_active=lambda:None)
    if failure=='outside':after[0,0,0]=1.
    if failure=='inside':after[0,1,0]=1.
    if failure=='overlap':protected[0,1]=.01
    options=dict(max_bytes=1 if failure=='budget' else 128*1024**2,
        verify_color=lambda color:failure!='icc',check_active=lambda:None)
    if failure:
        with pytest.raises(ValueError):measure_exposure_quality(edit,before,after,mask,protected,color,**options)
    else:
        report=measure_exposure_quality(edit,before,after,mask,protected,color,**options)
        assert report.status=='pass' and report.clipping_fraction==0 and report.outside_mask_max_error==0
        assert np.min(after)>1


@pytest.mark.parametrize('failure',[None,'protected','ev','strength','cancel'])
def test_shared_pixel_verifier_needs_no_approval_but_preserves_quality(failure):
    from photo_worker.operations.semantic.exposure_quality import verify_exposure_pixels
    edit,image,_=source_fixture()
    color=WorkingImage.model_validate_json(canonical_bytes(image)).color
    before=np.full((1,2,3),.5,np.float32)
    mask=np.array([[0.,.5]],np.float32);protected=np.array([[1.,0.]],np.float32)
    linear=to_working_linear(before,color)
    result=apply_local_exposure(linear,mask,protected,ev=edit['ev'],strength=edit['strength'],max_bytes=1024)
    after=restore_local_exposure_encoding(before,result,mask,protected,color,max_bytes=1024,check_active=lambda:None)
    if failure=='protected':after[0,0,0]=np.nextafter(after[0,0,0],np.float32(1))
    def check():
        if failure=='cancel':raise RuntimeError('cancelled')
    options=dict(ev=True if failure=='ev' else edit['ev'],strength=float('nan') if failure=='strength' else edit['strength'],max_bytes=128*1024**2,verify_color=lambda color:True,check_active=check)
    if failure:
        with pytest.raises((ValueError,RuntimeError)):
            verify_exposure_pixels(before,after,mask,protected,color,**options)
    else:
        assert verify_exposure_pixels(before,after,mask,protected,color,**options) is None
