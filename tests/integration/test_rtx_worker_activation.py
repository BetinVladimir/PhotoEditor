"""Initial operator activation is atomic and does not self-report live capacity."""
from hashlib import sha256
import json
import re
import pytest
from psycopg import errors, sql
from photo_worker.contracts.base import canonical_bytes
from test_rtx_worker_provisioning import ROOT, registration_input, provision


def activation_input(db):
    values = registration_input(db)
    provision(db, values)
    report = dict(schema_version=1, backend='cuda', lock_id=values['model_lock_sha'],
        models=[dict(model_id='dinov2_vits14', status='passed', lock_backend_status='passed')])
    return with_report({**values, 'qualified_operations_json':json.dumps({'features.scene':['cuda']})}, report)


def with_report(values, report):
    raw = canonical_bytes(report).decode()
    return {**values, 'doctor_report_jcs':raw, 'doctor_report_file_sha':sha256(raw.encode()).hexdigest()}


def activate(db, values):
    source = (ROOT / 'deploy/activate-rtx-worker.sql').read_text()
    source = '\n'.join(line for line in source.splitlines()
        if not line.startswith('\\') and line.strip() not in ('BEGIN;', 'COMMIT;'))
    source = re.sub(r":'([a-z_]+)'", lambda match: sql.Literal(values[match[1]]).as_string(db), source)
    db.execute(source)


def assert_disabled_without_slots(db, values):
    assert db.execute('SELECT enabled,doctor_report_sha256 FROM photo.resource_nodes WHERE node_id=%s',
        (values['node_id'],)).fetchone() == (False,None)
    assert db.execute('SELECT enabled,qualified_operations FROM photo.worker_registrations WHERE worker_id=%s',
        (values['worker_id'],)).fetchone() == (False,{})
    assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE node_id=%s', (values['node_id'],)).fetchone() == (0,)


def test_activation_waits_for_authenticated_capacity_report(db):
    values = activation_input(db)
    activate(db, values)
    assert db.execute('SELECT enabled,doctor_report_sha256,ram_available_bytes,vram_available_bytes,'
        'scratch_available_bytes,reported_at FROM photo.resource_nodes WHERE node_id=%s',
        (values['node_id'],)).fetchone() == (True,values['doctor_report_file_sha'],0,0,0,None)
    assert db.execute('SELECT enabled,qualified_operations,reported_capacity_policy_sha,capacity_policy_reported_at '
        'FROM photo.worker_registrations WHERE worker_id=%s', (values['worker_id'],)).fetchone() == (
            True,{'features.scene':['cuda']},None,None)
    assert db.execute('SELECT resource_class,tenant_id,task_id FROM photo.resource_slots WHERE node_id=%s',
        (values['node_id'],)).fetchall() == [('gpu',None,None)]
    assert db.execute('SELECT photo.capacity_policy_worker_fresh(w) FROM photo.worker_registrations w '
        'WHERE worker_id=%s', (values['worker_id'],)).fetchone() == (False,)


@pytest.mark.parametrize('change', [
    {'certificate_sha':'c'*64}, {'doctor_report_file_sha':'f'*64},
    {'qualified_operations_json':'{}'}, {'qualified_operations_json':'{"features.scene":["cpu"]}'},
    {'qualified_operations_json':'{"asset.verify":["cuda"]}'},
    {'qualified_operations_json':'{"unknown.operation":["cuda"]}'}])
def test_activation_refusal_leaves_registration_disabled(db, change):
    values = {**activation_input(db), **change}
    with pytest.raises(errors.RaiseException):
        with db.transaction():
            activate(db, values)
    assert_disabled_without_slots(db, values)


@pytest.mark.parametrize('kind', ['wrong_backend','failed_model','untested_model','duplicate_model','missing_model'])
def test_doctor_report_cannot_activate_unqualified_model(db, kind):
    values = activation_input(db)
    report = json.loads(values['doctor_report_jcs'])
    if kind == 'wrong_backend': report['backend'] = 'cpu'
    if kind == 'failed_model': report['models'][0]['status'] = 'failed'
    if kind == 'untested_model': report['models'][0]['lock_backend_status'] = 'untested'
    if kind == 'duplicate_model': report['models'].append(dict(report['models'][0]))
    if kind == 'missing_model': report['models'] = []
    values = with_report(values, report)
    with pytest.raises(errors.RaiseException):
        with db.transaction():
            activate(db, values)
    assert_disabled_without_slots(db, values)


def test_drained_node_cannot_be_reactivated_by_initial_template(db):
    values = activation_input(db)
    db.execute('UPDATE photo.resource_nodes SET drain_generation=1,drain_started_at=clock_timestamp() '
        'WHERE node_id=%s', (values['node_id'],))
    with pytest.raises(errors.RaiseException, match='IDENTITY_CONFLICT'):
        with db.transaction():
            activate(db, values)
    assert_disabled_without_slots(db, values)



def capacity_heartbeat(db, values, *, authenticated=True, backend='cuda', policy_sha=None, operations=None):
    # SET ROLE alone does not change session_user, which is the worker identity
    # checked by SECURITY DEFINER functions. Emulate the dedicated login explicitly.
    if authenticated:
        db.execute(sql.SQL('SET SESSION AUTHORIZATION {}').format(sql.Identifier(values['db_principal'])))
    try:
        db.execute('SET LOCAL ROLE photo_worker')
        return db.execute('SELECT photo.heartbeat_worker_capacity_v2(%s,%s,%s,%s,%s,%s,%s,%s)',
            (values['worker_id'],'gpu',backend,
             ['features.scene'] if operations is None else operations,
             32_000_000_000,12_000_000_000,12_000_000_000,
             values['capacity_policy_sha'] if policy_sha is None else policy_sha)).fetchone()[0]
    finally:
        db.execute('RESET ROLE')
        if authenticated:
            db.execute('RESET SESSION AUTHORIZATION')


def test_first_registered_cuda_heartbeat_refreshes_pinned_capacity(db):
    values = activation_input(db)
    activate(db, values)
    assert capacity_heartbeat(db, values) is True
    worker = db.execute('SELECT reported_capacity_policy_sha,photo.capacity_policy_worker_fresh(w) '
        'FROM photo.worker_registrations w WHERE worker_id=%s', (values['worker_id'],)).fetchone()
    assert worker == (values['capacity_policy_sha'],True)
    node = db.execute('SELECT capacity_report_worker_id,capacity_report_policy_sha,ram_available_bytes,'
        'vram_available_bytes,scratch_available_bytes,photo.capacity_policy_node_fresh(n) '
        'FROM photo.resource_nodes n WHERE node_id=%s', (values['node_id'],)).fetchone()
    assert str(node[0]) == values['worker_id']
    assert node[1] == values['capacity_policy_sha']
    assert node[2] == int(values['ram_budget_bytes'])
    assert node[3] == 10_000_000_000
    assert 0 < node[4] <= int(values['scratch_budget_bytes'])
    assert node[5] is True


@pytest.mark.parametrize('change', [
    {'authenticated':False}, {'policy_sha':'f'*64}, {'backend':'mps'}, {'operations':['unknown.operation']}])
def test_mismatched_heartbeat_does_not_make_activated_worker_available(db, change):
    values = activation_input(db)
    activate(db, values)
    assert capacity_heartbeat(db, values, **change) is False
    assert db.execute('SELECT reported_at,ram_available_bytes,vram_available_bytes '
        'FROM photo.resource_nodes WHERE node_id=%s', (values['node_id'],)).fetchone() == (None,0,0)
    assert db.execute('SELECT reported_capacity_policy_sha,capacity_policy_reported_at '
        'FROM photo.worker_registrations WHERE worker_id=%s', (values['worker_id'],)).fetchone() == (None,None)



def test_privilege_escalation_after_registration_blocks_activation(db):
    values = activation_input(db)
    db.execute(sql.SQL('GRANT photo_scheduler TO {}').format(sql.Identifier(values['db_principal'])))
    with pytest.raises(errors.RaiseException, match='LOGIN_PRIVILEGES_INVALID'):
        with db.transaction():
            activate(db, values)
    assert_disabled_without_slots(db, values)


@pytest.mark.parametrize('kind', ['replication', 'schema_create'])
def test_privilege_change_after_provision_refuses_activation(db, kind):
    values = activation_input(db)
    principal = sql.Identifier(values['db_principal'])
    if kind == 'replication':
        db.execute(sql.SQL('ALTER ROLE {} REPLICATION').format(principal))
    else:
        db.execute(sql.SQL('GRANT CREATE ON SCHEMA photo TO {}').format(principal))
    with pytest.raises(errors.RaiseException, match='LOGIN_PRIVILEGES_INVALID'):
        with db.transaction():
            activate(db, values)
    assert_disabled_without_slots(db, values)
