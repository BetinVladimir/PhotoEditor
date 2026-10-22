"""Explicit test-only policy reuse; real import/admission, no measured qualification."""
from copy import deepcopy
from uuid import uuid4
import pytest
from semantic_qualification_fixture import frozen_qualification
from retouch_preview_bundle_fixture import retouch_bundle_fixture
from removal_transaction_state import tenant_transaction_state
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


@pytest.mark.parametrize('mutation',[None,'model','runtime','device','dataset','backend','checks',
    'evidence','calibration','fingerprint','issuer','revoked_issuer'])
def test_exact_current_transfer_policy_reuse_never_creates_or_changes_policy(database,mutation):
    first=retouch_bundle_fixture(database,admitted=True)
    tenant=first['args'][0];scope=first['scope'];pin=scope['qualification']
    descriptor={key:pin[key] for key in ('policy_id','issuer_id','policy_sha')}
    options=dict(calibration_ref=deepcopy(pin['calibration']),existing_policy=descriptor)
    changes={'model':'model_lock_sha','runtime':'runtime_lock_sha','device':'device_profile_sha',
        'dataset':'dataset_sha','evidence':'evidence_sha'}
    if mutation in changes:options[changes[mutation]]='0'*64
    elif mutation=='backend':options['backend']='cpu'
    elif mutation=='checks':options['required_checks']=['different_required_check']
    elif mutation=='calibration':options['calibration_ref']['sha256']='0'*64
    elif mutation=='fingerprint':descriptor['policy_sha']='0'*64
    elif mutation=='issuer':descriptor['issuer_id']=str(uuid4())
    elif mutation=='revoked_issuer':
        database.execute('UPDATE photo.v2_qualification_operators SET enabled=false WHERE operator_id=%s',
            (pin['issuer_id'],))
    before=tenant_transaction_state(database,tenant)
    policies=database.execute('SELECT to_jsonb(p) FROM photo.v2_qualification_policies p ORDER BY id').fetchall()
    operators=database.execute('SELECT operator_id,enabled FROM photo.v2_qualification_operators ORDER BY operator_id').fetchall()
    if mutation is not None:
        with pytest.raises(AssertionError):
            frozen_qualification(database,tenant,scope['project'],scope['actor'],**options)
        assert tenant_transaction_state(database,tenant)==before
    else:
        other=frozen_qualification(database,tenant,scope['project'],scope['actor'],**options)
        assert {key:other[key] for key in descriptor}==descriptor
        assert other['qualification_id']!=pin['qualification_id']
        assert other['report']['artifact_id']!=pin['report']['artifact_id']
        assert database.execute('SELECT tenant_id,project_id,trusted_import,status FROM photo.model_qualifications '
            'WHERE id=%s',(other['qualification_id'],)).fetchone()==(tenant,scope['project'],True,'passed')
        assert database.execute("SELECT count(*) FROM photo.audit_log WHERE tenant_id=%s AND action='v2_qualification.imported' "
            "AND resource_id=%s AND details->>'policy_id'=%s",
            (tenant,other['qualification_id'],pin['policy_id'])).fetchone()==(1,)
    assert database.execute('SELECT to_jsonb(p) FROM photo.v2_qualification_policies p ORDER BY id').fetchall()==policies
    assert database.execute('SELECT operator_id,enabled FROM photo.v2_qualification_operators ORDER BY operator_id').fetchall()==operators
