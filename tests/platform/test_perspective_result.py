from uuid import uuid4
from types import SimpleNamespace
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_hash
from photo_worker.contracts.images import Canvas,ColorSpace,TransformChain,WorkingImage
from photo_worker.algorithms.perspective import manual_perspective
from photo_worker.operations.optical.perspective_result import perspective_center_matrix,perspective_working_image,stage_perspective_result
from photo_worker.operations.session.contracts import VersionBinding


def ref():return ArtifactRef(artifact_id=uuid4(),sha256='a'*64)

def image():
    return WorkingImage(pixels=ref(),alpha=None,dtype='float32',
        canvas=Canvas(width=20,height=10,orientation=1,space_id='input',source_sha256='b'*64),
        color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),transform_chain=TransformChain.create([]))


def test_matrix_conjugation_matches_half_pixel_edge_warp():
    source=image();geometry=manual_perspective([[.1,0],[.9,0],[.9,1],[.1,1]],20,10,max_crop_fraction=1,max_pixels=200)
    center=np.array(perspective_center_matrix(source,geometry)).reshape(3,3)
    point=np.array([7.,4.,1.]);edge=point+np.array([.5,.5,0])
    expected=geometry.source_to_output@edge;expected=expected/expected[2];expected[:2]-=.5
    actual=center@point;actual/=actual[2]
    np.testing.assert_allclose(actual,expected,atol=1e-10)


def test_result_preserves_lineage_color_and_chain():
    source=image();geometry=manual_perspective([[0,0],[1,0],[1,1],[0,1]],20,10,max_crop_fraction=0,max_pixels=200)
    output,transform=perspective_working_image(source,ref(),ref(),geometry)
    assert output.canvas.source_sha256==source.canvas.source_sha256
    assert output.color==source.color and len(output.transform_chain.steps)==1
    assert transform.from_canvas_hash==canonical_hash(source.canvas)
    assert transform.to_canvas_hash==canonical_hash(output.canvas)
    assert transform.matrix==output.transform_chain.steps[-1].forward_matrix


def test_foreign_binding_rejected_before_staging():
    source=image();source_ref=ref();binding=VersionBinding(asset_version_id=uuid4(),source=ref(),canvas_hash=canonical_hash(source.canvas))
    class RejectWrites:
        def stage(self,*args):raise AssertionError('must reject before IO')
        def stage_contract(self,*args):raise AssertionError('must reject before IO')
    with pytest.raises(ValueError,match='SOURCE_MISMATCH'):
        stage_perspective_result(RejectWrites(),source_ref,source,binding,SimpleNamespace(),mode='manual')
