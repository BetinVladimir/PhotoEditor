from hashlib import sha256
import pytest
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.operations.semantic.preview_binding import bind_removal_preview_documents
from test_v27_exposure_preview_binding import fixture


def removal_fixture():
    params, candidate, intent_raw = fixture()
    intent = parse_json(intent_raw); intent['kind'] = 'remove_object'; intent['ev'] = None
    raw = canonical_bytes(intent); params['intent']['sha256'] = sha256(raw).hexdigest()
    params.update(kind='remove_object',model_id='big_lama',context_pixels=128,feather_pixels=8)
    return params, candidate, raw


def test_removal_preview_has_no_confirmation_or_approval():
    params, candidate, raw = removal_fixture()
    owned, proposal, intent = bind_removal_preview_documents(params,candidate,raw)
    assert owned.kind == intent.kind == 'remove_object'
    assert proposal.target == owned.source
    assert 'confirmed' not in owned.model_dump() and 'approval_id' not in owned.model_dump()


@pytest.mark.parametrize('field,value', [('confirmed',True),('approval_id','fake'),('model_id','sd15_inpaint'),('context_pixels',True),('feather_pixels',0)])
def test_rejects_apply_fields_or_changed_model_policy(field,value):
    params, candidate, raw = removal_fixture();params[field]=value
    with pytest.raises(ValueError):
        bind_removal_preview_documents(params,candidate,raw)


def test_rejects_an_exposure_intent_or_mutated_candidate():
    params,candidate,raw=removal_fixture()
    with pytest.raises(ValueError):bind_removal_preview_documents(params,candidate+b' ',raw)
    _,_,exposure=fixture();params['intent']['sha256']=sha256(exposure).hexdigest()
    with pytest.raises(ValueError,match='INTENT_BINDING'):
        bind_removal_preview_documents(params,candidate,exposure)


def test_removal_geometry_uses_exact_source_mask_and_transform_bytes():
    from test_v27_exposure_geometry import geometry_fixture
    from photo_worker.operations.semantic.preview_binding import bind_removal_preview_geometry
    edit,candidate,intent,source,mask,transform,reference=geometry_fixture()
    params,_,_=removal_fixture()
    value=parse_json(intent);value['kind']='remove_object';value['ev']=None
    intent=canonical_bytes(value)
    params.update(source=edit['source'],candidate=edit['candidate'],intent={**edit['intent'],'sha256':sha256(intent).hexdigest()},snapshot_id=value['target_snapshot_id'])
    image,bound_mask,bound_transform=bind_removal_preview_geometry(params,candidate,intent,source,mask,transform,reference)
    assert bound_mask.canvas==image.canvas and bound_transform.kind=='homography'
    with pytest.raises(ValueError,match='GEOMETRY_HASH'):
        bind_removal_preview_geometry(params,candidate,intent,source,mask+b' ',transform,reference)


@pytest.mark.parametrize('origin',[True,False,1,None])
def test_removal_protection_requires_exact_canonical_origin(origin):
    from test_v27_exposure_protection import fixture as protection_fixture
    from photo_worker.operations.semantic.preview_binding import bind_removal_preview_protection
    params,_,_=removal_fixture();edit,source,root,raw=protection_fixture()
    params.update(source=edit['source'],protection=root)
    options=dict(verify_origin=lambda *args:origin,check_active=lambda:None)
    if origin is True:
        assert bind_removal_preview_protection(params,source,raw,**options).protected
    else:
        with pytest.raises(ValueError,match='ORIGIN_STALE'):
            bind_removal_preview_protection(params,source,raw,**options)
