"""Exact retouch document linkage; does not validate physical NPY/PNG bytes."""
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,parse_json
from photo_worker.operations.semantic.preview_retouch_result_binding import bind_retouch_preview_result
from test_v27_retouch_preview_contract import retouch_params
from test_v27_retouch_preview_evidence import evidence,presentation,ref
from test_v27_exposure_geometry import geometry_fixture


def fixture(failure=None):
    edit,_,_,source_raw,*_=geometry_fixture()
    params=retouch_params();params['source']=edit['source']
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
    display_ref,display_raw=pinned(display)
    if failure=='bytes':proof_raw+=b' '
    return params,display_ref,display_raw,proof_ref,proof_raw,image_ref,image_raw,source_raw


def test_retouch_result_binds_exact_preview_and_preserved_image_metadata():
    params,display,proof,image=bind_retouch_preview_result(*fixture())
    assert params.kind=='retouch_region' and display.params==proof.params==params
    assert display.pixels==proof.pixels==image.pixels


@pytest.mark.parametrize('failure',['color','source_pixels','params','roi','pixels','evidence','image','input_alias','bytes'])
def test_retouch_result_refuses_changed_linkage(failure):
    with pytest.raises(ValueError):bind_retouch_preview_result(*fixture(failure))
