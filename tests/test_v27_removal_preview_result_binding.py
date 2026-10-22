"""Exact removal document linkage; does not validate physical NPY/PNG bytes."""
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,parse_json
from photo_worker.operations.semantic.preview_removal_result_binding import bind_removal_preview_result
from test_v27_removal_preview_binding import removal_fixture
from test_v27_removal_preview_evidence import evidence

def ref():
    return dict(artifact_id=str(uuid4()), sha256="a"*64)

def presentation():
    return dict(schema_version=1, before=ref(), after=ref(), affected=ref(), protected=ref(),
        width=8, height=8, icc_lock_sha="b"*64, srgb_sha="c"*64,
        resize="area_linear_premult_v1", overlay="max_coverage_ceil_alpha_v1")
from test_v27_exposure_geometry import geometry_fixture


def fixture(failure=None):
    edit,_,_,source_raw,*_=geometry_fixture()
    params=removal_fixture()[0];params['source']=edit['source']
    source=parse_json(source_raw);image=dict(source,pixels=ref())
    def pinned(value):
        raw=canonical_bytes(value)
        return ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest()),raw
    if failure=='color':image['color']=dict(image['color'],icc_sha256='f'*64)
    if failure=='source_pixels':image['pixels']=source['pixels']
    image_ref,image_raw=pinned(image)
    proof=evidence();proof.update(params=params,working_image=image_ref.model_dump(mode='json'),pixels=image['pixels'])
    if failure=='params':proof['params']=dict(params,target_revision='8')
    if failure=='roi':proof['roi']=[0,0,100,100]
    if failure=='pixels':proof['pixels']=ref()
    proof_ref,proof_raw=pinned(proof)
    display=presentation();display.update(params=params,numerical_evidence=proof_ref.model_dump(mode='json'),
        working_image=image_ref.model_dump(mode='json'),pixels=image['pixels'])
    if failure=='evidence':display['numerical_evidence']=ref()
    if failure=='image':display['working_image']=ref()
    if failure=='input_alias':display['before']=params['plan']
    if failure=='width':display['width']=7
    if failure=='height':display['height']=7
    display_ref,display_raw=pinned(display)
    if failure=='bytes':proof_raw+=b' '
    return params,display_ref,display_raw,proof_ref,proof_raw,image_ref,image_raw,source_raw


def test_removal_result_binds_exact_preview_and_preserved_image_metadata():
    params,display,proof,image=bind_removal_preview_result(*fixture())
    assert params.kind=='remove_object' and display.params==proof.params==params
    assert display.pixels==proof.pixels==image.pixels


@pytest.mark.parametrize('failure',['color','source_pixels','params','roi','pixels','evidence','image','input_alias','bytes','width','height'])
def test_removal_result_refuses_changed_linkage(failure):
    with pytest.raises(ValueError):bind_removal_preview_result(*fixture(failure))
