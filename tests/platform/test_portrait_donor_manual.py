from hashlib import sha256
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.contracts.manual_mask import ManualMask,MaskRasterParams,RasterizedMask,Polygon,MaskPoint
from photo_worker.v2.portrait_donor_manual import verify_donor_manual_masks,read_verified_donor_manual_masks
from test_portrait_donor_masks_material import case


def manual_case():
    params,raw,docs,reader=case();source=WorkingImage.model_validate_json(raw)
    target,guard=[MaskRef.model_validate_json(body) for body in docs]
    vectors=ManualMask(canvas=source.canvas,source_sha256=source.canvas.source_sha256,
        shapes=[Polygon(kind='polygon',sequence=0,mode='add',points=[MaskPoint(x=x,y=y) for x,y in [(0.,0.),(1.,0.),(1.,1.),(0.,1.)]])])
    producer=MaskRasterParams(image=params.base.source,inline_vectors=vectors)
    producer_raw=canonical_bytes(producer)
    ref=lambda body:ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
    producer_ref=ref(producer_raw)
    root=RasterizedMask(image=params.base.source,vectors=producer_ref,target=target,protected=guard,
        target_png=ref(b'target-png'),protected_png=ref(b'protected-png'))
    root_raw=canonical_bytes(root);root_ref=ref(root_raw)
    pixels=np.ones((100,100),np.float32);protected=np.zeros_like(pixels)
    pixels.setflags(write=False);protected.setflags(write=False)
    args=(params,raw,*docs,root_ref,root_raw,producer_ref,producer_raw,pixels,protected)
    return args,reader


def test_manual_root_and_vectors_replay_exact_donor_masks():
    args,_=manual_case();pixels,protected=args[-2:];root_raw=args[5]
    assert verify_donor_manual_masks(*args,check_active=lambda:None) is False
    wrong=pixels.copy();wrong[50,50]=0;wrong.setflags(write=False)
    with pytest.raises(ValueError,match='RASTER_VECTOR_MISMATCH'):
        verify_donor_manual_masks(*args[:-2],wrong,protected,check_active=lambda:None)
    with pytest.raises(ValueError,match='DONOR_MANUAL_ROOT_SHA'):
        verify_donor_manual_masks(*args[:5],root_raw+b' ',*args[6:],check_active=lambda:None)


def test_noncanonical_rehashed_producer_params_rejected():
    args,_=manual_case();args=list(args)
    args[7]+=b' '
    args[6]=args[6].model_copy(update={'sha256':sha256(args[7]).hexdigest()})
    with pytest.raises(ValueError,match='DONOR_MANUAL_PARAMS_CANONICAL'):
        verify_donor_manual_masks(*args,check_active=lambda:None)


@pytest.mark.parametrize('deny_at',[None,1,2])
def test_physical_manual_context_rechecks_authority_and_cleans_scratch(tmp_path,deny_at):
    args,reader=manual_case();calls=[]
    def authority(p,root,producer):
        calls.append(1)
        return len(calls)!=deny_at
    def execute():
        with read_verified_donor_manual_masks(*args[:-2],reader,scratch_root=tmp_path,
                max_bytes=1_000_000,check_active=lambda:None,producer_authority=authority) as material:
            np.testing.assert_array_equal(material[2],1)
    if deny_at is None:
        execute();assert len(calls)==3
    else:
        with pytest.raises(ValueError,match='DONOR_MANUAL_AUTHORITY'):execute()
    assert not list(tmp_path.iterdir())
