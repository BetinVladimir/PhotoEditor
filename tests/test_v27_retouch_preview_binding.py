"""Own-kind, exact-byte proposal binding before any retouch adapter IO."""
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.operations.semantic.preview_binding import bind_retouch_preview_documents
from test_v27_exposure_preview_binding import fixture


def retouch_fixture():
    params,candidate,raw=fixture()
    intent=parse_json(raw)
    intent.update(kind='retouch_region',ev=None)
    raw=canonical_bytes(intent)
    params.update(kind='retouch_region')
    params['intent']['sha256']=sha256(raw).hexdigest()
    return params,candidate,raw


def test_retouch_proposal_binds_exact_mask_strength_and_source_without_approval():
    params,candidate,raw=retouch_fixture()
    owned,proposal,intent=bind_retouch_preview_documents(params,bytearray(candidate),bytearray(raw))
    assert owned.kind==intent.kind=='retouch_region'
    assert proposal.target==owned.source and proposal.mask is not None
    assert intent.strength==parse_json(raw)['strength'] and intent.ev is None
    assert 'confirmed' not in owned.model_dump() and 'approval_id' not in owned.model_dump()


@pytest.mark.parametrize('failure',['candidate_bytes','intent_bytes','wrong_kind','snapshot',
    'target','mask_missing','transform_missing','rejected','mask_alias','transform_alias','foreign_params'])
def test_retouch_proposal_refuses_changed_bytes_scope_or_mask(failure):
    params,candidate,raw=retouch_fixture()
    value=parse_json(candidate)
    intent=parse_json(raw)
    if failure=='wrong_kind':intent['kind']='background_cleanup'
    if failure=='snapshot':intent['target_snapshot_id']=str(uuid4())
    if failure=='target':value['target']['asset_version_id']=str(uuid4())
    if failure=='mask_missing':value['mask']=None
    if failure=='transform_missing':value['transform']=None
    if failure=='rejected':value.update(state='rejected',reason='user_rejected')
    if failure=='mask_alias':value['mask']=params['plan']
    if failure=='transform_alias':value['transform']=params['protection']
    if failure=='foreign_params':params['kind']='remove_object'
    candidate=canonical_bytes(value);raw=canonical_bytes(intent)
    params['candidate']['sha256']=sha256(candidate).hexdigest()
    params['intent']['sha256']=sha256(raw).hexdigest()
    if failure=='candidate_bytes':candidate+=b' '
    if failure=='intent_bytes':raw+=b' '
    with pytest.raises(ValueError):bind_retouch_preview_documents(params,candidate,raw)


def test_retouch_geometry_keeps_exact_transferred_mask_and_transform():
    from test_v27_exposure_geometry import geometry_fixture
    from photo_worker.operations.semantic.preview_binding import bind_retouch_preview_geometry
    edit,candidate,intent,source,mask,transform,reference=geometry_fixture()
    params,_,_=retouch_fixture()
    value=parse_json(intent);value.update(kind='retouch_region',ev=None)
    intent=canonical_bytes(value)
    params.update(source=edit['source'],candidate=edit['candidate'],
        intent={**edit['intent'],'sha256':sha256(intent).hexdigest()},snapshot_id=value['target_snapshot_id'])
    image,bound_mask,bound_transform=bind_retouch_preview_geometry(
        params,candidate,intent,source,mask,transform,reference)
    assert bound_mask.canvas==image.canvas and bound_transform.kind=='homography'
    with pytest.raises(ValueError,match='GEOMETRY_HASH'):
        bind_retouch_preview_geometry(params,candidate,intent,source,mask+b' ',transform,reference)


@pytest.mark.parametrize('origin',[True,False,1,None])
def test_retouch_protection_requires_exact_canonical_origin(origin):
    from test_v27_exposure_protection import fixture as protection_fixture
    from photo_worker.operations.semantic.preview_binding import bind_retouch_preview_protection
    params,_,_=retouch_fixture();edit,source,root,raw=protection_fixture()
    params.update(source=edit['source'],protection=root)
    options=dict(verify_origin=lambda *args:origin,check_active=lambda:None)
    if origin is True:
        assert bind_retouch_preview_protection(params,source,raw,**options).protected
    else:
        with pytest.raises(ValueError,match='ORIGIN_STALE'):
            bind_retouch_preview_protection(params,source,raw,**options)


@pytest.mark.parametrize('preserve',[False,0,1,1.0,'true',None])
def test_retouch_intent_requires_literal_json_true_for_person_protection(preserve):
    params,candidate,raw=retouch_fixture()
    value=parse_json(raw);value['preserve_persons']=preserve
    raw=canonical_bytes(value);params['intent']['sha256']=sha256(raw).hexdigest()
    with pytest.raises(ValueError,match='INTENT_PROTECTION_INVALID'):
        bind_retouch_preview_documents(params,candidate,raw)


@pytest.mark.parametrize('strength',[0.,.125,1.])
def test_retouch_intent_keeps_exact_strength_without_scaling(strength):
    params,candidate,raw=retouch_fixture()
    value=parse_json(raw);value['strength']=strength
    raw=canonical_bytes(value);params['intent']['sha256']=sha256(raw).hexdigest()
    _,_,intent=bind_retouch_preview_documents(params,candidate,raw)
    assert intent.strength==strength
