from pathlib import Path
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_hash
from photo_worker.contracts.images import WorkingImage, DenseWarpTransform, TransformChain, Rect
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.operations.optical.multi_lineage import compile_multi_lineage


def ref():return ArtifactRef(artifact_id=uuid4(),sha256='a'*64)


def fixture():
    path=Path(__file__).resolve().parents[2]/'backend/internal/v2optical/testdata/negative-source.json'
    source=WorkingImage.model_validate_json(path.read_bytes())
    bindings=[VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(source)),canvas_hash=canonical_hash(source.canvas)) for _ in range(2)]
    return source,bindings


def test_planar_lineage_keeps_all_sources_and_reference_chain():
    source,bindings=fixture()
    image,registrations=compile_multi_lineage([source]*2,bindings,[np.eye(3)]*2,ref(),ref(),
        operation='optical.focus_stack',reference=1,output_shape=(100,100))
    assert [r.source for r in registrations]==bindings
    assert all(r.to_canvas_hash==canonical_hash(image.canvas) and r.inverse_field is None for r in registrations)
    assert image.canvas.source_sha256==source.canvas.source_sha256
    assert image.transform_chain.steps[-1].from_canvas==source.canvas


def test_cylindrical_fields_remain_ordered_and_reference_dense_step_is_explicit():
    source,bindings=fixture();fields=[ref(),ref()]
    image,registrations=compile_multi_lineage([source]*2,bindings,[np.eye(3)]*2,ref(),ref(),
        operation='optical.panorama',reference=1,output_shape=(100,100),inverse_fields=fields)
    assert [r.inverse_field for r in registrations]==fields
    dense=image.transform_chain.steps[-2]
    assert dense.inverse_field==fields[1] and dense.pixel_centers=='half_pixel'
    assert image.transform_chain.steps[-1].from_canvas==dense.to_canvas


def test_lineage_rejects_unbound_source_and_partial_projection_list():
    source,bindings=fixture();bindings[0].canvas_hash='b'*64
    with pytest.raises(ValueError,match='MULTI_SOURCE_MISMATCH'):
        compile_multi_lineage([source]*2,bindings,[np.eye(3)]*2,ref(),ref(),operation='optical.panorama',reference=0,output_shape=(100,100))
    source,bindings=fixture()
    with pytest.raises(ValueError,match='MULTI_LINEAGE_INVALID'):
        compile_multi_lineage([source]*2,bindings,[np.eye(3)]*2,ref(),ref(),operation='optical.panorama',reference=0,output_shape=(100,100),inverse_fields=[ref(),None])


@pytest.mark.parametrize('role',['pixels','alpha','projection'])
def test_new_output_cannot_reuse_a_source_dense_warp_field(role):
    source,_=fixture();field=ref()
    step=DenseWarpTransform(kind='dense_warp',from_canvas=source.canvas,to_canvas=source.canvas,
        invertibility='partial',valid_region=Rect(x=0,y=0,width=100,height=100),
        inverse_field=field,pixel_centers='half_pixel')
    source=source.model_copy(update={'transform_chain':TransformChain.create([step])})
    bindings=[VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(
        artifact_id=uuid4(),sha256=canonical_hash(source)),canvas_hash=canonical_hash(source.canvas))
        for _ in range(2)]
    pixels=field if role=='pixels' else ref()
    alpha=field if role=='alpha' else ref()
    fields=[field,ref()] if role=='projection' else [ref(),ref()]
    with pytest.raises(ValueError,match='MULTI_OUTPUT_ALIAS'):
        compile_multi_lineage([source]*2,bindings,[np.eye(3)]*2,pixels,alpha,
            operation='optical.panorama',reference=0,output_shape=(100,100),inverse_fields=fields)
