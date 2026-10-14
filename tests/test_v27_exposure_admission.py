from hashlib import sha256
from types import SimpleNamespace
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.operations.semantic.exposure_admission import SQLLocalExposureAdmission
from test_v27_approved_edit_binding import documents


def setup(authority_result=True):
    edit=ApprovedSemanticEdit.model_validate_json(canonical_bytes(documents()[0]))
    raw=canonical_bytes(edit)
    command=SimpleNamespace(operation='semantic.local_exposure',operation_version='1',resource_class='cpu',params=SimpleNamespace(sha256=sha256(raw).hexdigest()))
    lease=SimpleNamespace(command=command,execution_profile={'backend':'cpu','renderer_version':'v2.2'})
    calls=[]
    ctx=SimpleNamespace(lease=lease,check_active=lambda:calls.append('active'))
    database=SimpleNamespace(admit_semantic_local_exposure=lambda passed,body:calls.append((passed,body)) or authority_result)
    return edit,lease,calls,ctx,database


def test_exact_bytes_and_lease_are_forwarded_with_cancellation_checks():
    edit,lease,calls,ctx,database=setup()
    assert SQLLocalExposureAdmission(database,ctx)(edit) is True
    assert calls==['active',(lease,canonical_bytes(edit)),'active']


@pytest.mark.parametrize('field,value',[('operation','apply'),('operation_version','2'),('resource_class','gpu')])
def test_wrong_task_is_rejected_without_database_call(field,value):
    edit,lease,calls,ctx,database=setup();setattr(lease.command,field,value)
    assert SQLLocalExposureAdmission(database,ctx)(edit) is False
    assert calls==['active']


@pytest.mark.parametrize('field,value',[('backend','cuda'),('renderer_version','v2.1')])
def test_wrong_execution_profile_is_rejected(field,value):
    edit,lease,calls,ctx,database=setup();lease.execution_profile[field]=value
    assert SQLLocalExposureAdmission(database,ctx)(edit) is False
    assert calls==['active']


def test_changed_parameters_are_rejected():
    edit,lease,calls,ctx,database=setup();edit.ev=-.25
    assert SQLLocalExposureAdmission(database,ctx)(edit) is False
    assert calls==['active']


@pytest.mark.parametrize('result',[False,None,1,'true'])
def test_database_must_return_literal_true(result):
    edit,lease,calls,ctx,database=setup(result)
    assert SQLLocalExposureAdmission(database,ctx)(edit) is False


def test_missing_authority_is_not_permissive():
    with pytest.raises(ValueError,match='INSTALLATION_REQUIRED'):
        SQLLocalExposureAdmission(object(),object())


def test_cancellation_after_sql_propagates():
    edit,lease,calls,ctx,database=setup()
    def active():
        if calls:raise RuntimeError('cancelled')
        calls.append('active')
    ctx.check_active=active
    with pytest.raises(RuntimeError,match='cancelled'):
        SQLLocalExposureAdmission(database,ctx)(edit)
