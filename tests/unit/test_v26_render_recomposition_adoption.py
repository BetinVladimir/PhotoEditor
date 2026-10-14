"""Independent pixel oracle; submitted documents/NPY have actual valid hashes."""
from hashlib import sha256
from io import BytesIO
from uuid import UUID, uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import parse_json
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_recomposition_adoption import verify_render_recomposition
from test_v26_render_adoption import fixture


def expected_pixels(*, legacy=False):
    # The fixture's full-opacity normal proposal has this independently known
    # HDR value and inherited generated coverage, without reading staged bytes.
    rgb=np.full((2,2,3),[-.2,4.,.8],np.float32)
    alpha=(np.array([[0,1],[32768,65535]],np.uint16).astype(np.float32)/np.float32(65535)
        if legacy else None)
    generated=np.ones((2,2),np.float32)
    for array in (rgb,alpha,generated):
        if array is not None:array.flags.writeable=False
    return AdjustmentPixels(rgb,alpha,generated)


def verify(case, expected, path, **kwargs):
    reader,frozen,root,binding,parents,*_=case
    verify_render_recomposition(reader,frozen,root,binding,parents,expected,
        scratch_root=path,max_bytes=kwargs.get('max_bytes',100000),
        max_tile_bytes=kwargs.get('max_tile_bytes',4096),check_active=kwargs.get('check_active',lambda:None))


@pytest.mark.parametrize('legacy',[False,True])
def test_known_hdr_composition_and_generated_lineage_pass_and_cleanup(tmp_path,legacy):
    case=fixture(legacy_alpha=legacy)
    verify(case,expected_pixels(legacy=legacy),tmp_path)
    assert case[0].opened==case[0].closed
    assert not list(tmp_path.iterdir())


@pytest.mark.parametrize('field',['rgb','alpha','generated'])
def test_independent_expected_pixels_cannot_be_substituted(tmp_path,field):
    case=fixture(legacy_alpha=True);expected=expected_pixels(legacy=True)
    arrays={key:getattr(expected,key).copy() for key in ('rgb','alpha','generated')}
    arrays[field].flat[0]=np.float32(.2 if field!='generated' else 0)
    for array in arrays.values():array.flags.writeable=False
    with pytest.raises(AdoptionInvalid,match=field.upper()+'_MISMATCH'):
        verify(case,AdjustmentPixels(**arrays),tmp_path)
    assert case[0].opened==case[0].closed and not list(tmp_path.iterdir())


def test_valid_hash_wrong_staged_pixels_rejected(tmp_path):
    case=list(fixture());reader,_,_,_,_,put,result=case
    image=parse_json(reader.objects[UUID(result['working_image']['artifact_id'])][0])
    wrong=np.full((2,2,3),[-.2,4.,.8],np.float32);wrong[1,1,0]=7.
    stream=BytesIO();np.save(stream,wrong,allow_pickle=False);raw=stream.getvalue()
    identifier=uuid4();digest=sha256(raw).hexdigest()
    reader.objects[identifier]=(raw,'application/x-npy','output','pixels')
    image['pixels']={'artifact_id':str(identifier),'sha256':digest}
    image_ref=put(image,'output','working_image')
    result['working_image']={'artifact_id':str(image_ref.artifact_id),'sha256':image_ref.sha256}
    case[2]=put(result,'output','document')
    with pytest.raises(AdoptionInvalid,match='RGB_MISMATCH'):
        verify(case,expected_pixels(),tmp_path)
    assert reader.opened==reader.closed and not list(tmp_path.iterdir())


def test_omitted_generated_lineage_rejected(tmp_path):
    case=list(fixture());result=case[-1];result['generated_mask']=None
    case[2]=case[-2](result,'output','document')
    with pytest.raises(AdoptionInvalid,match='GENERATED_MISMATCH'):
        verify(case,expected_pixels(),tmp_path)
    assert case[0].opened==case[0].closed and not list(tmp_path.iterdir())


@pytest.mark.parametrize('failure',['writeable','nan','fractional','shape','tile','scratch','cancel'])
def test_expected_validation_budgets_and_cancellation_cleanup(tmp_path,failure):
    case=fixture();expected=expected_pixels();options={}
    arrays={key:getattr(expected,key) for key in ('rgb','alpha','generated')}
    if failure in ('writeable','nan','shape'):
        arrays['rgb']=expected.rgb.copy()
        if failure=='nan':arrays['rgb'].flat[0]=np.nan
        if failure=='shape':arrays['rgb']=arrays['rgb'][:1]
        if failure!='writeable':arrays['rgb'].flags.writeable=False
    if failure=='fractional':
        arrays['generated']=np.full((2,2),.5,np.float32);arrays['generated'].flags.writeable=False
    if failure=='tile':options['max_tile_bytes']=255
    if failure=='scratch':options['max_bytes']=1
    cancelled_mapping=False
    if failure=='cancel':
        def cancel():
            nonlocal cancelled_mapping
            if any(tmp_path.rglob('*.npy')):
                cancelled_mapping=True
                raise RuntimeError('cancelled')
        options['check_active']=cancel
    with pytest.raises((AdoptionInvalid,RuntimeError)):
        verify(case,AdjustmentPixels(**arrays),tmp_path,**options)
    assert case[0].opened==case[0].closed and not list(tmp_path.iterdir())
    if failure=='cancel':assert cancelled_mapping
