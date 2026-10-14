from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.semantic.exposure_protection import sql_local_exposure_protection_origin
from test_v27_exposure_admission import setup


def fixture(answer=True):
    edit,lease,_,ctx,_=setup()
    calls=[]
    db=SimpleNamespace(local_exposure_protection_origin=lambda *args:calls.append(args) or answer)
    root=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    binary=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    origin=sql_local_exposure_protection_origin(db,lease,bytearray(canonical_bytes(edit)),check_active=ctx.check_active)
    return edit,lease,calls,root,binary,origin


def test_sql_origin_owns_lease_and_passes_original_params():
    edit,lease,calls,root,binary,origin=fixture()
    lease.command.operation='substituted'
    assert origin(edit.source,root,binary) is True
    assert calls[0][0].command.operation=='semantic.local_exposure'
    assert calls[0][1]==canonical_bytes(edit)
    calls[0][0].command.operation='callback-mutation'
    assert origin(edit.source,root,binary) is True
    assert calls[1][0].command.operation=='semantic.local_exposure'


def test_changed_source_is_rejected_without_sql():
    edit,_,calls,root,binary,origin=fixture()
    changed=edit.source.model_copy(deep=True);changed.asset_version_id=uuid4()
    assert origin(changed,root,binary) is False and calls==[]


@pytest.mark.parametrize('answer',[False,None,1,'true'])
def test_only_literal_true_accepts_sql_authority(answer):
    edit,_,_,root,binary,origin=fixture(answer)
    assert origin(edit.source,root,binary) is False


def test_params_mismatch_and_missing_installation_fail():
    edit,lease,_,ctx,_=setup()
    db=SimpleNamespace(local_exposure_protection_origin=lambda *args:True)
    with pytest.raises(ValueError,match='PARAMS_CHANGED'):
        sql_local_exposure_protection_origin(db,lease,canonical_bytes(edit)+b' ',check_active=ctx.check_active)
    with pytest.raises(ValueError,match='INSTALLATION_REQUIRED'):
        sql_local_exposure_protection_origin(object(),lease,canonical_bytes(edit),check_active=ctx.check_active)


def test_cancellation_after_origin_sql_returns_no_authority():
    edit,lease,_,_,_=setup();active_calls=[]
    def active():
        active_calls.append(True)
        if len(active_calls)==2:raise RuntimeError('cancelled')
    db=SimpleNamespace(local_exposure_protection_origin=lambda *args:True)
    origin=sql_local_exposure_protection_origin(db,lease,canonical_bytes(edit),check_active=active)
    with pytest.raises(RuntimeError,match='cancelled'):
        origin(edit.source,ArtifactRef(artifact_id=uuid4(),sha256='a'*64),ArtifactRef(artifact_id=uuid4(),sha256='b'*64))
