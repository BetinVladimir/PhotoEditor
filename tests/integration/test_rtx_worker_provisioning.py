"""Execute the operator SQL in an isolated migrated database; never run on production."""
from pathlib import Path
from uuid import uuid4
import re
import pytest
from psycopg import sql, errors
from test_database import tenant
from photo_worker.runtime.capacity_policy import load_capacity_policy
from photo_worker.runtime.capacity_policy_installation import install_capacity_policy_timing

ROOT = Path(__file__).resolve().parents[2]


def registration_input(db):
    tenant_id, _ = tenant(db, str(uuid4()) + '@example.test')
    principal = 'rtx_' + uuid4().hex
    db.execute(sql.SQL('CREATE ROLE {} LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS').format(sql.Identifier(principal)))
    db.execute(sql.SQL('GRANT photo_worker TO {}').format(sql.Identifier(principal)))
    policy = load_capacity_policy(ROOT / 'configs/capacity-rtx3060.json')
    pin = install_capacity_policy_timing(db, policy)
    return dict(tenant_id=str(tenant_id), worker_id=str(uuid4()), node_id='rtx-' + uuid4().hex,
        db_principal=principal, model_lock_sha='a'*64, certificate_sha='b'*64,
        capacity_policy_sha=pin, ram_budget_bytes=str(policy.photo_ram_budget_bytes),
        scratch_budget_bytes='12884901888')


def provision(db, values):
    # Retain all SQL validation/mutations. The fixture owns transaction boundaries;
    # psql literal variables are rendered using the driver's escaping rules.
    source = (ROOT / 'deploy/provision-rtx-worker.sql').read_text()
    source = '\n'.join(line for line in source.splitlines()
        if not line.startswith('\\') and line.strip() not in ('BEGIN;', 'COMMIT;'))
    source = re.sub(r":'([a-z_]+)'", lambda match: sql.Literal(values[match[1]]).as_string(db), source)
    db.execute(source)


def test_initial_rtx_registration_is_pinned_but_not_dispatchable(db):
    values = registration_input(db)
    provision(db, values)
    assert db.execute('SELECT enabled,qualified_operations,capacity_policy_sha,lock_id,certificate_sha256 '
        'FROM photo.worker_registrations WHERE worker_id=%s', (values['worker_id'],)).fetchone() == (
            False, {}, values['capacity_policy_sha'], values['model_lock_sha'], values['certificate_sha'])
    assert db.execute('SELECT enabled,vram_budget_bytes,doctor_report_sha256,reported_at '
        'FROM photo.resource_nodes WHERE node_id=%s', (values['node_id'],)).fetchone() == (False,10000000000,None,None)
    assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE node_id=%s', (values['node_id'],)).fetchone() == (0,)


@pytest.mark.parametrize('change', [
    {'capacity_policy_sha':'f'*64}, {'ram_budget_bytes':'1'}, {'model_lock_sha':'invalid'}])
def test_invalid_registration_rolls_back_node_and_worker(db, change):
    values = {**registration_input(db), **change}
    with pytest.raises(errors.RaiseException):
        with db.transaction():
            provision(db, values)
    assert db.execute('SELECT count(*) FROM photo.resource_nodes WHERE node_id=%s', (values['node_id'],)).fetchone() == (0,)
    assert db.execute('SELECT count(*) FROM photo.worker_registrations WHERE worker_id=%s', (values['worker_id'],)).fetchone() == (0,)


def test_duplicate_worker_identity_does_not_create_second_node(db):
    values = registration_input(db)
    provision(db, values)
    # The temporary input table normally disappears at operator commit; release it
    # explicitly inside the outer rollback fixture to model a second invocation.
    db.execute('DROP TABLE rtx_provision_input')
    second = {**values, 'node_id':'rtx-' + uuid4().hex}
    with pytest.raises(errors.RaiseException, match='IDENTITY_ALREADY_REGISTERED'):
        with db.transaction():
            provision(db, second)
    assert db.execute('SELECT count(*) FROM photo.resource_nodes WHERE node_id=%s', (second['node_id'],)).fetchone() == (0,)
    assert db.execute('SELECT node_id FROM photo.worker_registrations WHERE worker_id=%s', (values['worker_id'],)).fetchone() == (values['node_id'],)


@pytest.mark.parametrize('role', ['photo_api','photo_scheduler','photo_gateway','photo_fn_owner','pg_read_all_data'])
def test_privileged_worker_login_cannot_be_registered(db, role):
    values = registration_input(db)
    db.execute(sql.SQL('GRANT {} TO {}').format(sql.Identifier(role),sql.Identifier(values['db_principal'])))
    with pytest.raises(errors.RaiseException, match='LOGIN_NOT_PROVISIONED'):
        with db.transaction():
            provision(db, values)
    assert db.execute('SELECT count(*) FROM photo.resource_nodes WHERE node_id=%s', (values['node_id'],)).fetchone() == (0,)
    assert db.execute('SELECT count(*) FROM photo.worker_registrations WHERE worker_id=%s', (values['worker_id'],)).fetchone() == (0,)


@pytest.mark.parametrize('kind', ['replication', 'inherited_replication', 'inherited_bypass', 'schema_create'])
def test_runtime_guard_incompatible_identity_refused_before_registration(db, kind):
    values = registration_input(db)
    principal = sql.Identifier(values['db_principal'])
    if kind == 'replication':
        db.execute(sql.SQL('ALTER ROLE {} REPLICATION').format(principal))
    elif kind == 'schema_create':
        db.execute(sql.SQL('GRANT CREATE ON SCHEMA photo TO {}').format(principal))
    else:
        privileged = sql.Identifier('rtx_priv_' + uuid4().hex)
        attribute = sql.SQL('REPLICATION' if kind == 'inherited_replication' else 'BYPASSRLS')
        db.execute(sql.SQL('CREATE ROLE {} NOLOGIN {}').format(privileged, attribute))
        db.execute(sql.SQL('GRANT {} TO {}').format(privileged, principal))
    with pytest.raises(errors.RaiseException, match='LOGIN_NOT_PROVISIONED'):
        with db.transaction():
            provision(db, values)
    assert db.execute('SELECT count(*) FROM photo.resource_nodes WHERE node_id=%s', (values['node_id'],)).fetchone() == (0,)
