from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.operations.semantic.exposure_protection import bind_local_exposure_protection
from test_v27_exposure_geometry import geometry_fixture


def fixture(change=None):
    values=geometry_fixture();mask=parse_json(values[4]);mask['protected']=True
    if change:change(mask)
    raw=canonical_bytes(mask)
    root=dict(artifact_id=str(uuid4()),sha256=sha256(raw).hexdigest())
    return values[0],values[3],root,raw


def test_protection_origin_receives_exact_independent_source_and_child_pins():
    edit,source,root,raw=fixture();calls=[]
    def origin(binding,ref,child):
        assert canonical_bytes(binding)==canonical_bytes(edit['source'])
        assert canonical_bytes(ref)==canonical_bytes(root)
        calls.append(child.artifact_id);child.artifact_id=uuid4();return True
    result=bind_local_exposure_protection(edit,source,root,raw,verify_origin=origin,check_active=lambda:None)
    assert result.protected and result.artifact.artifact_id==calls[0]


@pytest.mark.parametrize('answer',[False,None,1,'true'])
def test_missing_canonical_origin_never_accepts_protection(answer):
    with pytest.raises(ValueError,match='ORIGIN_STALE'):
        bind_local_exposure_protection(*fixture(),verify_origin=lambda *args:answer,check_active=lambda:None)


@pytest.mark.parametrize('change',[
    lambda mask:mask.update(protected=False),lambda mask:mask.update(model_lock_id=None,selector="person"),
    lambda mask:mask['canvas'].update(space_id='foreign'),
])
def test_protection_metadata_substitution_rejected_before_origin(change):
    def forbidden(*args):raise AssertionError('origin called for invalid protection')
    with pytest.raises(ValueError,match='PROTECTION_CHANGED'):
        bind_local_exposure_protection(*fixture(change),verify_origin=forbidden,check_active=lambda:None)


def test_original_bytes_and_installed_authority_are_required():
    args=list(fixture());args[3]+=b' '
    with pytest.raises(ValueError,match='PROTECTION_HASH'):
        bind_local_exposure_protection(*args,verify_origin=lambda *args:True,check_active=lambda:None)
    with pytest.raises(ValueError,match='INSTALLATION_REQUIRED'):
        bind_local_exposure_protection(*fixture(),verify_origin=None,check_active=lambda:None)


@pytest.mark.parametrize('accepted',[True,False])
def test_manual_protection_without_model_lock_still_requires_origin(accepted):
    args=fixture(lambda mask:mask.update(selector='manual',model_lock_id=None))
    if accepted:
        result=bind_local_exposure_protection(*args,verify_origin=lambda *args:True,check_active=lambda:None)
        assert result.selector=='manual' and result.model_lock_id is None
    else:
        with pytest.raises(ValueError,match='ORIGIN_STALE'):
            bind_local_exposure_protection(*args,verify_origin=lambda *args:False,check_active=lambda:None)
