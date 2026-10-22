"""Installed Editor authority + real qualified registration activation; unexecuted.

No model/pixel quality is claimed by relational registration fixtures.
"""
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb
from test_task_leases import queued_task

OPS=['editor.effects_seal','editor.layers_precompose']

def installation(db,operation):
    item=queued_task(db)
    db.execute('UPDATE photo.worker_registrations SET resource_classes=%s,backends=%s,qualified_operations=%s WHERE tenant_id=%s AND worker_id=%s',(Jsonb(['cpu']),Jsonb(['cpu']),Jsonb({operation:['cpu']}),item['tenant'],item['worker']))
    db.execute("UPDATE photo.resource_nodes SET enabled=true,reported_at=clock_timestamp(),doctor_report_sha256=%s WHERE node_id=%s",('a'*64,item['node']))
    return item

def configure(db,item,operation,enabled=True):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_maintenance')
        result=db.execute('SELECT photo.configure_editor_internal_dispatch(%s,%s,%s,%s)',(item['tenant'],item['worker'],operation,enabled)).fetchone()[0]
        db.execute('RESET ROLE')
        return result

@pytest.mark.parametrize('operation',OPS)
def test_installed_cpu_editor_can_activate_and_revoke_without_mount_cycle(db,operation):
    item=installation(db,operation)
    assert db.execute('SELECT photo.editor_internal_dispatch_installed(%s)',(operation,)).fetchone()==(True,)
    db.execute("UPDATE photo.operation_registry SET v2_enabled=false WHERE operation=%s",(operation,))
    # Installation remains ready while disabled, so worker may mount then
    # heartbeat; activation only follows the already qualified registration.
    assert db.execute('SELECT photo.editor_internal_dispatch_installed(%s)',(operation,)).fetchone()==(True,)
    assert configure(db,item,operation)
    assert db.execute("SELECT v2_enabled FROM photo.operation_registry WHERE operation=%s AND version='1'",(operation,)).fetchone()==(True,)
    assert configure(db,item,operation,False)
    assert db.execute("SELECT v2_enabled FROM photo.operation_registry WHERE operation=%s AND version='1'",(operation,)).fetchone()==(False,)

@pytest.mark.parametrize('operation',OPS)
@pytest.mark.parametrize('missing',['qualification','missing_backend','worker','node','foreign','role','output','private_helper_acl'])
def test_editor_activation_refuses_missing_runtime_or_authority(db,operation,missing):
    item=installation(db,operation)
    db.execute("UPDATE photo.operation_registry SET v2_enabled=false WHERE operation=%s",(operation,))
    if missing=='qualification':db.execute("UPDATE photo.worker_registrations SET qualified_operations='{}' WHERE worker_id=%s",(item['worker'],))
    elif missing=='missing_backend':db.execute('UPDATE photo.worker_registrations SET qualified_operations=%s WHERE worker_id=%s',(Jsonb({operation:{}}),item['worker']))
    elif missing=='private_helper_acl':db.execute('GRANT EXECUTE ON FUNCTION photo.editor_effect_seal_adoption_scope(uuid,uuid,integer,bigint,uuid) TO photo_scheduler') if operation.endswith('seal') else db.execute('ALTER FUNCTION photo.record_editor_precompose(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_api')
    elif missing=='worker':db.execute("UPDATE photo.worker_registrations SET last_seen=clock_timestamp()-interval '1 hour' WHERE worker_id=%s",(item['worker'],))
    elif missing=='node':db.execute("UPDATE photo.resource_nodes SET reported_at=clock_timestamp()-interval '1 hour' WHERE node_id=%s",(item['node'],))
    elif missing=='foreign':item['tenant']=uuid4()
    elif missing=='role':db.execute("ALTER FUNCTION photo.editor_layers_precompose_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_api" if operation.endswith('precompose') else "ALTER FUNCTION photo.editor_effect_seal_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_api")
    else:db.execute("DELETE FROM photo.operation_output_policies WHERE operation=%s AND role='image_result'",(operation,))
    assert not configure(db,item,operation)
    assert db.execute("SELECT v2_enabled FROM photo.operation_registry WHERE operation=%s AND version='1'",(operation,)).fetchone()==(False,)

@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal'])
def test_editor_activation_is_maintenance_only(db,role):
    assert db.execute("SELECT has_function_privilege(%s,'photo.configure_editor_internal_dispatch(uuid,uuid,text,boolean)','EXECUTE')",(role,)).fetchone()==(False,)


@pytest.mark.migration_head(2546)
def test_editor_internal_inventory_upgrade_preserves_public_wrapper_identity(db):
    from pathlib import Path
    signatures=['photo.tenant_capabilities()','photo.claim_task(uuid,integer,uuid,jsonb)','photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text)']
    def inventory():
        return [db.execute('SELECT oid,proowner,proacl::text,prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone() for signature in signatures]
    before=inventory()
    migration=Path(__file__).resolve().parents[2]/'backend/db/migrations/02547_editor_internal_dispatch_activation.sql'
    db.execute(migration.read_text().split('-- +goose Down')[0])
    assert inventory()==before
    for signature in signatures:
        source=db.execute('SELECT prosrc FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()[0]
        for operation in [*OPS,'capture.prepare']:
            assert "'"+operation+"'" in source
    for operation in OPS:
        assert db.execute('SELECT photo.editor_internal_dispatch_installed(%s)',(operation,)).fetchone()==(True,)
