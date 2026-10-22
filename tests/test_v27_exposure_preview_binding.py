from uuid import uuid4
import pytest
from photo_worker.contracts.base import parse_json
from photo_worker.operations.semantic.preview_binding import bind_exposure_preview_documents
from test_v27_approved_edit_binding import documents


def fixture():
    edit,candidate,intent=documents()
    params=dict(schema_version=1,tenant_id=str(uuid4()),intent_id=str(uuid4()),
        target_id=edit['target_id'],workflow_id=str(uuid4()),workflow_revision='9',
        target_revision='9007199254740993',snapshot_id=parse_json(intent)['target_snapshot_id'],
        plan=dict(artifact_id=str(uuid4()),sha256='a'*64),intent=edit['intent'],
        candidate=edit['candidate'],source=edit['source'],
        protection=dict(artifact_id=str(uuid4()),sha256='b'*64),
        model_lock_sha='c'*64,runtime_lock_sha='d'*64,device_profile_sha='e'*64)
    return params,candidate,intent


def test_preview_binds_original_proposal_without_approval():
    params,candidate,intent=fixture()
    owned,proposal,edit=bind_exposure_preview_documents(params,candidate,intent)
    params['source']['canvas_hash']='f'*64
    assert proposal.target == owned.source and edit.ev == .25
    assert owned.target_revision == '9007199254740993'


@pytest.mark.parametrize('failure',['hash','snapshot','revision','alias','approval'])
def test_preview_rejects_changed_scope_or_apply_input(failure):
    params,candidate,intent=fixture()
    if failure=='hash':candidate+=b' '
    if failure=='snapshot':params['snapshot_id']=str(uuid4())
    if failure=='revision':params['target_revision']='01'
    if failure=='alias':params['protection']=params['candidate']
    if failure=='approval':params['approval_id']=str(uuid4())
    with pytest.raises(ValueError):bind_exposure_preview_documents(params,candidate,intent)


def test_preview_source_binds_exact_working_pixels_without_approved_edit():
    from test_v27_exposure_source import source_fixture
    from photo_worker.operations.semantic.preview_binding import bind_exposure_preview_source
    params,_,_=fixture();edit,image,raw=source_fixture();params['source']=edit['source']
    result=bind_exposure_preview_source(params,bytearray(raw))
    assert result.canvas.width==2 and result.color.encoding=='gamma24'
    with pytest.raises(ValueError,match='SOURCE_HASH'):
        bind_exposure_preview_source(params,raw+b' ')
    params['source']['canvas_hash']='0'*64
    with pytest.raises(ValueError,match='SOURCE_CHANGED'):
        bind_exposure_preview_source(params,raw)


def test_preview_source_forbids_document_as_pixel_payload():
    from hashlib import sha256
    from test_v27_exposure_source import source_fixture
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.operations.semantic.preview_binding import bind_exposure_preview_source
    params,_,_=fixture();edit,image,_=source_fixture();params['source']=edit['source']
    image['pixels']['artifact_id']=params['candidate']['artifact_id']
    raw=canonical_bytes(image);params['source']['source']['sha256']=sha256(raw).hexdigest()
    with pytest.raises(ValueError,match='ARTIFACT_ALIAS'):
        bind_exposure_preview_source(params,raw)


def test_preview_geometry_uses_exact_candidate_mask_and_transform():
    from test_v27_exposure_geometry import geometry_fixture
    from photo_worker.operations.semantic.preview_binding import bind_exposure_preview_geometry
    edit,candidate,intent,source,mask,transform,reference=geometry_fixture()
    params,_,_=fixture();params.update(source=edit['source'],candidate=edit['candidate'],intent=edit['intent'],snapshot_id=parse_json(intent)['target_snapshot_id'])
    image,bound_mask,bound_transform=bind_exposure_preview_geometry(params,candidate,intent,source,mask,transform,reference)
    assert bound_mask.canvas==image.canvas and bound_transform.kind=='homography'
    with pytest.raises(ValueError,match='GEOMETRY_HASH'):
        bind_exposure_preview_geometry(params,candidate,intent,source,mask+b' ',transform,reference)


@pytest.mark.parametrize('answer',[True,False,None,1])
def test_preview_protection_requires_exact_canonical_origin(answer):
    from test_v27_exposure_protection import fixture as protection_fixture
    from photo_worker.operations.semantic.preview_binding import bind_exposure_preview_protection
    params,_,_=fixture();edit,source,root,raw=protection_fixture()
    params.update(source=edit['source'],protection=root)
    options=dict(verify_origin=lambda *args:answer,check_active=lambda:None)
    if answer is True:
        mask=bind_exposure_preview_protection(params,source,raw,**options)
        assert mask.protected
    else:
        with pytest.raises(ValueError,match='ORIGIN_STALE'):
            bind_exposure_preview_protection(params,source,raw,**options)


@pytest.mark.parametrize('schema_version',[True,1.0,'1'])
def test_preview_schema_version_requires_exact_integer(schema_version):
    params,candidate,intent=fixture();params['schema_version']=schema_version
    with pytest.raises(ValueError):bind_exposure_preview_documents(params,candidate,intent)
