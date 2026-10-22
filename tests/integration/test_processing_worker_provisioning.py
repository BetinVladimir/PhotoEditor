"""Disabled processing identities retain the same restricted LOGIN boundary."""
from pathlib import Path
import re
import pytest
from psycopg import sql,errors
from test_rtx_worker_provisioning import registration_input

ROOT=Path(__file__).resolve().parents[2]


def provision_processing(db,values):
    source=(ROOT/'deploy/provision-processing-worker.sql').read_text()
    source='\n'.join(line for line in source.splitlines() if not line.startswith('\\') and line.strip() not in ('BEGIN;','COMMIT;'))
    source=re.sub(r":'([a-z_]+)'",lambda m:sql.Literal(values[m[1]]).as_string(db),source)
    db.execute(source)


@pytest.mark.parametrize('kind',['cpu','media','archive'])
def test_processing_registration_is_disabled_and_has_no_slots(db,kind):
    values=registration_input(db);values['resource_class']=kind
    if kind=='cpu':values['ram_budget_bytes']=str(8*1024**3)
    provision_processing(db,values)
    assert db.execute('SELECT enabled,resource_classes,backends,qualified_operations FROM photo.worker_registrations WHERE worker_id=%s',(values['worker_id'],)).fetchone()==(False,[kind],['cpu'],{})
    assert db.execute('SELECT enabled,vram_budget_bytes,doctor_report_sha256 FROM photo.resource_nodes WHERE node_id=%s',(values['node_id'],)).fetchone()==(False,0,None)
    assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE node_id=%s',(values['node_id'],)).fetchone()==(0,)


@pytest.mark.parametrize('change',[{'resource_class':'gpu'},{'resource_class':'cuda'},{'resource_class':''},{'ram_budget_bytes':'1'}])
def test_invalid_processing_scope_is_atomic(db,change):
    values={**registration_input(db),'resource_class':'media',**change}
    with pytest.raises(errors.RaiseException):
        with db.transaction():provision_processing(db,values)
    assert db.execute('SELECT count(*) FROM photo.resource_nodes WHERE node_id=%s',(values['node_id'],)).fetchone()==(0,)


@pytest.mark.parametrize('role',['photo_api','photo_scheduler','photo_fn_owner','pg_read_all_data'])
def test_processing_identity_cannot_inherit_privileged_role(db,role):
    values={**registration_input(db),'resource_class':'media'}
    db.execute(sql.SQL('GRANT {} TO {}').format(sql.Identifier(role),sql.Identifier(values['db_principal'])))
    with pytest.raises(errors.RaiseException,match='LOGIN_NOT_PROVISIONED'):
        with db.transaction():provision_processing(db,values)
    assert db.execute('SELECT count(*) FROM photo.worker_registrations WHERE worker_id=%s',(values['worker_id'],)).fetchone()==(0,)
