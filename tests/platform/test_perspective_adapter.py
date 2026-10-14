from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash,ArtifactRef
from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain
from photo_worker.contracts.optical_perspective import OpticalPerspectiveParams
from photo_worker.operations.optical.perspective_geometry import resolve_perspective_geometry
from photo_worker.operations.optical.perspective_adapter import execute
from photo_worker.runtime.executor import OperationFailure


def source():
    return WorkingImage(pixels=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),alpha=None,dtype='float32',
        canvas=Canvas(width=100,height=80,orientation=1,space_id='source',source_sha256='b'*64),
        color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),transform_chain=TransformChain.create([]))


def params(image,strength):
    return OpticalPerspectiveParams.model_validate_json(canonical_bytes(dict(schema_version=1,
        base=dict(asset_version_id=str(uuid4()),source=dict(artifact_id=str(uuid4()),sha256='d'*64),canvas_hash=canonical_hash(image.canvas)),
        mode='manual',strength=strength,corners=[dict(x=x,y=y) for x,y in [(.2,0),(.8,0),(.8,1),(.2,1)]],lines=None,max_crop_fraction=.5)))


def test_manual_strength_interpolates_crop_quad():
    image=source()
    zero=resolve_perspective_geometry(params(image,0),image,None,max_pixels=8000)
    half=resolve_perspective_geometry(params(image,.5),image,None,max_pixels=8000)
    full=resolve_perspective_geometry(params(image,1),image,None,max_pixels=8000)
    assert (zero.width,half.width,full.width)==(100,80,60)
    assert half.height==full.height==80
    np.testing.assert_array_equal(zero.source_to_output,np.eye(3))
    assert half.crop_fraction==pytest.approx(.2)


def test_adapter_requires_qualification_before_artifact_access():
    ctx=SimpleNamespace(lease=SimpleNamespace(execution_profile=dict(backend='cpu',renderer_version='v2.2')))
    class NoIO:
        def params(self,*args):raise AssertionError('must fail before IO')
    with pytest.raises(OperationFailure):execute(ctx,NoIO(),None)


def test_admission_failure_precedes_pixel_io():
    image=source();intent=params(image,1);intent.base.source.sha256=canonical_hash(image)
    ctx=SimpleNamespace(lease=SimpleNamespace(execution_profile=dict(backend='cpu',renderer_version='v2.2')),check_active=lambda:None)
    class Artifacts:
        def params(self,*args):return intent
        def contract(self,*args):return image
        def declared_contract_refs(self,contract,maximum):return (intent.base.source,) if contract=='WorkingImage' else (image.pixels,)
        def input(self,*args,**kwargs):raise AssertionError('must fail before pixel IO')
    class Refuse:
        def admit(self,*args):raise OperationFailure('PERSPECTIVE_QUALIFICATION_UNAVAILABLE')
    with pytest.raises(OperationFailure):execute(ctx,Artifacts(),Refuse())
