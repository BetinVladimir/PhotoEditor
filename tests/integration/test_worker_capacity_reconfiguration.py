"""Actual operator SQL guard tests; run only against an isolated migrated DB.

Reports/host attestations here are relational fixtures, never deployment evidence.
Each caller-owned fixture transaction must be rolled back after the case.
"""
import copy
import hashlib
import json
import re
from datetime import datetime, timezone, timedelta
from pathlib import Path
from uuid import uuid4
import pytest
from psycopg import errors, sql
from photo_worker.runtime.capacity_policy import CapacityPolicy
from photo_worker.runtime.capacity_policy_installation import install_capacity_policy_timing
from test_processing_worker_activation import activation_input, activate
from test_rtx_worker_activation import activation_input as gpu_input, activate as gpu_activate

ROOT = Path(__file__).resolve().parents[2]


def snapshot(db, values):
    worker = db.execute('SELECT to_jsonb(w) FROM photo.worker_registrations w WHERE worker_id=%s',
                        (values['worker_id'],)).fetchone()[0]
    node = db.execute('SELECT to_jsonb(n) FROM photo.resource_nodes n WHERE node_id=%s',
                      (values['node_id'],)).fetchone()[0]
    slots = db.execute("SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY slot_name),'[]'::jsonb) "
                       'FROM photo.resource_slots s WHERE node_id=%s', (values['node_id'],)).fetchone()[0]
    return dict(expected_worker=worker, expected_node=node, expected_slots=slots)


def prepare(db, kind='media'):
    values = gpu_input(db) if kind == 'gpu' else activation_input(db, kind)
    (gpu_activate if kind == 'gpu' else activate)(db, values)
    db.execute('UPDATE photo.resource_nodes SET enabled=false,ram_available_bytes=0,'
               'vram_available_bytes=0,scratch_available_bytes=0 WHERE node_id=%s', (values['node_id'],))
    db.execute('UPDATE photo.worker_registrations SET enabled=false WHERE worker_id=%s', (values['worker_id'],))
    raw = json.loads((ROOT/'configs/capacity-rtx3060.json').read_text())
    if kind == 'gpu': raw['photo_ram_budget_bytes'] = 12 << 30
    elif kind == 'cpu': raw['control_ram_budget_bytes'] = (8 << 30) - 1048576
    else: raw['photo_ram_budget_bytes'] = 8 << 30
    policy = CapacityPolicy.model_validate(raw)
    pin = install_capacity_policy_timing(db, policy)
    payload = dict(phase='stage', resource_class=kind, backend='cuda' if kind == 'gpu' else 'cpu',
                   new_policy_sha=pin,
                   new_ram_bytes=policy.control_ram_budget_bytes if kind == 'cpu' else policy.photo_ram_budget_bytes,
                   new_vram_bytes=policy.gpu_budget_bytes if kind == 'gpu' else 0,
                   new_scratch_bytes=int(values['scratch_budget_bytes']), **snapshot(db, values))
    payload['preflight'] = dict(status='OPERATOR_REVIEWED_STOPPED_HOST_CAPACITY',
        reviewed_at=datetime.now(timezone.utc).isoformat(), worker_id=values['worker_id'], new_policy_sha=pin,
        scheduler_stopped=True, worker_stopped=True, rabbit_empty=True,
        physical_ram_bytes=33514270720, total_worker_caps_bytes=28 << 30, os_reserve_bytes=3 << 30,
        host_evidence_sha256='1'*64, resource_measurement_sha256='2'*64, runtime_lock_sha256='3'*64)
    return values, payload


def apply(db, payload):
    source = (ROOT/'deploy/reconfigure-worker-capacity.sql').read_text()
    source = '\n'.join(line for line in source.splitlines()
                       if not line.startswith('\\') and line.strip() not in ('BEGIN;', 'COMMIT;'))
    db.execute(re.sub(r":'capacity_change_json'", lambda _: sql.Literal(json.dumps(payload)).as_string(db), source))


def resume_payload(db, values, payload):
    db.execute('DROP TABLE capacity_change_input')
    resumed = {**copy.deepcopy(payload), **snapshot(db, values), 'phase':'resume',
               'doctor_report_jcs':values['doctor_report_jcs'],
               'doctor_report_file_sha':values['doctor_report_file_sha']}
    resumed['preflight'].update(resource_and_quality_review_passed=True, qualification_evidence_sha256='4'*64)
    return resumed


@pytest.mark.parametrize('kind', ['cpu', 'media', 'archive', 'gpu'])
def test_capacity_change_preserves_authority_and_slots_until_real_heartbeat(db, kind):
    values, payload = prepare(db, kind)
    before = copy.deepcopy(payload)
    apply(db, payload)
    state = snapshot(db, values)
    assert state['expected_slots'] == before['expected_slots']
    w, n = state['expected_worker'], state['expected_node']
    for key in ('worker_id', 'node_id', 'db_principal', 'certificate_sha256', 'lock_id', 'qualified_operations'):
        assert w[key] == before['expected_worker'][key]
    assert not w['enabled'] and not n['enabled'] and n['doctor_report_sha256'] is None
    assert w['capacity'] == {} and w['reported_capacity_policy_sha'] is None
    assert n['ram_budget_bytes'] == payload['new_ram_bytes']
    resumed = resume_payload(db, values, payload)
    apply(db, resumed)
    after = snapshot(db, values)
    assert after['expected_slots'] == before['expected_slots']
    assert after['expected_worker']['qualified_operations'] == before['expected_worker']['qualified_operations']
    assert after['expected_node']['drain_generation'] == before['expected_node']['drain_generation']
    assert after['expected_node']['doctor_report_sha256'] == values['doctor_report_file_sha']
    assert after['expected_node']['enabled'] and after['expected_worker']['enabled']
    assert (after['expected_node']['ram_available_bytes'], after['expected_node']['vram_available_bytes'],
            after['expected_node']['scratch_available_bytes'], after['expected_node']['reported_at']) == (0, 0, 0, None)
    assert db.execute('SELECT photo.capacity_policy_worker_fresh(w) FROM photo.worker_registrations w '
                      'WHERE worker_id=%s', (values['worker_id'],)).fetchone() == (False,)


@pytest.mark.parametrize('field,value', [('certificate_sha256','c'*64),('lock_id','d'*64),
    ('qualified_operations',{}),('capacity_policy_sha','e'*64),('db_principal','not_this_worker')])
def test_stale_identity_snapshot_is_rejected_atomically(db, field, value):
    values, payload = prepare(db)
    before = snapshot(db, values)
    payload['expected_worker'][field] = value
    with pytest.raises(errors.RaiseException, match='CAS_OR_IDLE_CONFLICT'):
        with db.transaction(): apply(db, payload)
    assert snapshot(db, values) == before


def relational_task(db, values, binding):
    task = uuid4()
    # Actual ready params and complete relational parents: guards stay enabled.
    # These are isolated relational fixtures, not physical processing evidence.
    owner = db.execute('SELECT owner_user_id FROM photo.tenants WHERE id=%s',
                       (values['tenant_id'],)).fetchone()[0]
    artifact = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) "
                          "VALUES(%s,'capacity-test',%s,%s,2,'application/json') RETURNING id",
                          (values['tenant_id'], str(uuid4()), 'a'*64)).fetchone()[0]
    input_snapshot = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) "
                                "VALUES(%s,'input','user',%s,%s,1) RETURNING id",
                                (values['tenant_id'], owner, 'b'*64)).fetchone()[0]
    run = db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) "
                     "VALUES(%s,'apply','running',%s,'{}',%s,%s,true) RETURNING id",
                     (values['tenant_id'], input_snapshot, 'c'*64, owner)).fetchone()[0]
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,"
               "resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,"
               "worker_id,dispatch_worker_id,lease_until) VALUES(%s,%s,%s,'capacity-test','asset.verify','1',"
               "'media',%s,'[]',%s,%s,'{}',%s,%s,CASE WHEN %s THEN clock_timestamp()+interval '1 minute' END)",
        (task, values['tenant_id'], run, 'running' if binding == 'running' else 'queued', artifact, 'a'*64,
         values['worker_id'] if binding == 'running' else None,
         values['worker_id'] if binding == 'dispatch_only' else None, binding == 'running'))
    return task


@pytest.mark.parametrize('condition', ['enabled_worker','enabled_node','nonzero_available','occupied_slot','slot_lease'])
def test_busy_or_live_node_is_never_reconfigured(db, condition):
    values, payload = prepare(db)
    if condition == 'enabled_worker':
        db.execute('UPDATE photo.worker_registrations SET enabled=true WHERE worker_id=%s',(values['worker_id'],))
    elif condition == 'enabled_node':
        db.execute('UPDATE photo.resource_nodes SET enabled=true WHERE node_id=%s',(values['node_id'],))
    elif condition == 'nonzero_available':
        db.execute('UPDATE photo.resource_nodes SET ram_available_bytes=1 WHERE node_id=%s',(values['node_id'],))
    elif condition == 'occupied_slot':
        task = relational_task(db, values, 'occupied')
        db.execute("UPDATE photo.resource_slots SET tenant_id=%s,task_id=%s,fencing_token=0,"
                   "lease_until=clock_timestamp()+interval '1 minute' WHERE node_id=%s",
                   (values['tenant_id'], task, values['node_id']))
        db.execute('SET CONSTRAINTS ALL IMMEDIATE')
        db.execute('SET CONSTRAINTS ALL DEFERRED')
    else:
        db.execute("UPDATE photo.resource_slots SET lease_until=clock_timestamp()+interval '1 hour' WHERE node_id=%s",(values['node_id'],))
    payload.update(snapshot(db, values))
    before = snapshot(db, values)
    with pytest.raises(errors.RaiseException, match='CAS_OR_IDLE_CONFLICT'):
        with db.transaction(): apply(db, payload)
    assert snapshot(db, values) == before


@pytest.mark.parametrize('change',[{'rabbit_empty':False}, {'worker_stopped':False}, {'scheduler_stopped':False},
    {'physical_ram_bytes':28 << 30}, {'resource_measurement_sha256':None},
    {'reviewed_at':(datetime.now(timezone.utc)-timedelta(hours=1)).isoformat()}])
def test_missing_or_stale_external_review_is_refused(db, change):
    values, payload = prepare(db)
    payload['preflight'].update(change)
    before = snapshot(db, values)
    with pytest.raises(errors.RaiseException, match='EXTERNAL_PREFLIGHT_INVALID'):
        with db.transaction(): apply(db, payload)
    assert snapshot(db, values) == before


@pytest.mark.parametrize('role', ['photo_api','photo_fn_owner','pg_read_all_data'])
def test_reconfiguration_rechecks_inherited_privileges(db, role):
    values, payload = prepare(db)
    db.execute(sql.SQL('GRANT {} TO {}').format(sql.Identifier(role), sql.Identifier(values['db_principal'])))
    with pytest.raises(errors.RaiseException, match='LOGIN_PRIVILEGES_INVALID'):
        with db.transaction(): apply(db, payload)


@pytest.mark.parametrize('defect',['bad_doctor_hash','wrong_backend','missing_quality','changed_budget','changed_allowlist'])
def test_resume_cannot_fabricate_readiness_or_new_qualification(db, defect):
    values, payload = prepare(db)
    apply(db, payload)
    resumed = resume_payload(db, values, payload)
    if defect == 'bad_doctor_hash': resumed['doctor_report_file_sha'] = 'f'*64
    elif defect == 'wrong_backend':
        report = json.loads(resumed['doctor_report_jcs']); report['backend'] = 'cuda'
        resumed['doctor_report_jcs'] = json.dumps(report)
        resumed['doctor_report_file_sha'] = hashlib.sha256(resumed['doctor_report_jcs'].encode()).hexdigest()
    elif defect == 'missing_quality': resumed['preflight'].pop('qualification_evidence_sha256')
    elif defect == 'changed_budget': resumed['new_ram_bytes'] -= 1
    else: resumed['expected_worker']['qualified_operations'] = {'unknown.operation':['cpu']}
    before = snapshot(db, values)
    with pytest.raises(errors.RaiseException):
        with db.transaction(): apply(db, resumed)
    assert snapshot(db, values) == before


@pytest.mark.parametrize('binding', ['dispatch_only', 'running', 'reserved_expired'])
def test_pending_binding_and_unreleased_reservation_block_change(db, binding):
    values, payload = prepare(db)
    task = relational_task(db, values, binding)
    if binding == 'reserved_expired':
        db.execute("INSERT INTO photo.resource_reservations(tenant_id,task_id,node_id,scratch_bytes,ram_bytes,"
                   "vram_bytes,expires_at) VALUES(%s,%s,%s,1,1,0,clock_timestamp()-interval '1 day')",
                   (values['tenant_id'], task, values['node_id']))
    db.execute('SET CONSTRAINTS ALL IMMEDIATE')
    db.execute('SET CONSTRAINTS ALL DEFERRED')
    before = snapshot(db, values)
    with pytest.raises(errors.RaiseException, match='CAS_OR_IDLE_CONFLICT'):
        with db.transaction(): apply(db, payload)
    assert snapshot(db, values) == before
