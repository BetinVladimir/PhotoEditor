"""Relational operator guards only; synthetic reports never qualify real processing."""
import hashlib,json,re
from pathlib import Path
import pytest
from psycopg import sql,errors
from test_rtx_worker_provisioning import registration_input
from test_processing_worker_provisioning import provision_processing

ROOT=Path(__file__).resolve().parents[2]


def activation_input(db,kind='media'):
    values=registration_input(db);values['resource_class']=kind
    if kind=='cpu':values['ram_budget_bytes']=str(8*1024**3)
    provision_processing(db,values)
    row=db.execute("SELECT operation FROM photo.operation_registry WHERE resource_class=%s AND version='1' AND backends ? 'cpu' AND model_id IS NULL ORDER BY operation LIMIT 1",(kind,)).fetchone()
    assert row is not None
    report=json.dumps(dict(schema_version=1,backend='cpu',lock_id=values['model_lock_sha'],models=[]),sort_keys=True,separators=(',',':'))
    values.update(doctor_report_jcs=report,doctor_report_file_sha=hashlib.sha256(report.encode()).hexdigest(),qualified_operations_json=json.dumps({row[0]:['cpu']}))
    return values


def activate(db,values):
    source=(ROOT/'deploy/activate-processing-worker.sql').read_text()
    source='\n'.join(line for line in source.splitlines() if not line.startswith('\\') and line.strip() not in ('BEGIN;','COMMIT;'))
    db.execute(re.sub(r":'([a-z_]+)'",lambda m:sql.Literal(values[m[1]]).as_string(db),source))


@pytest.mark.parametrize('kind,count',[('cpu',2),('media',1),('archive',1)])
def test_exact_processing_activation_is_stale_until_authenticated_heartbeat(db,kind,count):
    values=activation_input(db,kind);activate(db,values)
    assert db.execute('SELECT enabled,qualified_operations,last_seen,capacity_policy_reported_at FROM photo.worker_registrations WHERE worker_id=%s',(values['worker_id'],)).fetchone()[:2]==(True,json.loads(values['qualified_operations_json']))
    assert db.execute('SELECT enabled,ram_available_bytes,vram_available_bytes,scratch_available_bytes,reported_at FROM photo.resource_nodes WHERE node_id=%s',(values['node_id'],)).fetchone()==(True,0,0,0,None)
    assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE node_id=%s AND resource_class=%s',(values['node_id'],kind)).fetchone()==(count,)
    assert db.execute("SELECT last_seen='epoch',reported_capacity_policy_sha IS NULL,capacity_policy_reported_at IS NULL FROM photo.worker_registrations WHERE worker_id=%s",(values['worker_id'],)).fetchone()==(True,True,True)


@pytest.mark.parametrize('change',[{'resource_class':'gpu'},{'doctor_report_file_sha':'f'*64},{'qualified_operations_json':'{}'},{'qualified_operations_json':'{"unknown.op":["cpu"]}'},{'qualified_operations_json':'{"asset.verify":["cuda"]}'},{'certificate_sha':'c'*64},{'model_lock_sha':'d'*64}])
def test_activation_scope_mismatch_keeps_registration_disabled(db,change):
    values=activation_input(db)
    with pytest.raises(errors.RaiseException):
        with db.transaction():activate(db,{**values,**change})
    assert db.execute('SELECT enabled,qualified_operations FROM photo.worker_registrations WHERE worker_id=%s',(values['worker_id'],)).fetchone()==(False,{})
    assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE node_id=%s',(values['node_id'],)).fetchone()==(0,)


@pytest.mark.parametrize('role',['photo_api','photo_fn_owner','pg_read_all_data'])
def test_activation_rechecks_privileges_after_provisioning(db,role):
    values=activation_input(db)
    db.execute(sql.SQL('GRANT {} TO {}').format(sql.Identifier(role),sql.Identifier(values['db_principal'])))
    with pytest.raises(errors.RaiseException,match='LOGIN_PRIVILEGES_INVALID'):
        with db.transaction():activate(db,values)


def test_node_ram_drift_is_rejected(db):
    values=activation_input(db)
    db.execute('UPDATE photo.resource_nodes SET ram_budget_bytes=ram_budget_bytes-1 WHERE node_id=%s',(values['node_id'],))
    with pytest.raises(errors.RaiseException,match='IDENTITY_CONFLICT'):
        with db.transaction():activate(db,values)
