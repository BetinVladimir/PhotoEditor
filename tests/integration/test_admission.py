import json
from uuid import uuid4
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from photo_worker.contracts.wire import TaskCommand
from test_task_leases import queued_task,claim,commit


def ready_task(db):
    x=queued_task(db)
    db.execute('SELECT photo.release_task_resources(%s,%s,0)',(x['tenant'],x['task']))
    db.execute("UPDATE photo.tasks SET state='ready',dispatch_command=NULL WHERE id=%s",(x['task'],))
    db.execute("UPDATE photo.resource_nodes SET enabled=true,ram_available_bytes=ram_budget_bytes,scratch_available_bytes=scratch_budget_bytes,reported_at=clock_timestamp(),doctor_report_sha256=%s WHERE node_id=%s",('a'*64,x['node']))
    db.execute("UPDATE photo.worker_registrations SET qualified_operations='{\"asset.verify\":[\"cpu\"]}' WHERE worker_id=%s",(x['worker'],))
    return x


def reserve(db,x,ram=1024,vram=0,scratch=1024,node=None):
    return db.execute('SELECT * FROM photo.reserve_dispatch(%s,%s,%s,%s,%s,%s,%s,%s)',(x['task'],node or x['node'],ram,vram,scratch,uuid4(),uuid4(),'')).fetchone()


def test_reservation_command_and_commit_transaction(db):
    x=ready_task(db)
    db.execute('SET LOCAL ROLE photo_scheduler')
    state,command=reserve(db,x)
    assert state=='queued'
    parsed=TaskCommand.model_validate_json(json.dumps(command))
    assert parsed.operation=='asset.verify' and parsed.timeout_seconds==300 and parsed.attempt==1
    assert reserve(db,x)[0]=='waiting_resource'  # occupied physical slot
    db.execute('RESET ROLE')
    assert db.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s',(x['tenant'],)).fetchone()[0]==1
    assert db.execute('SELECT dispatch_worker_id FROM photo.tasks WHERE id=%s',(x['task'],)).fetchone()[0]==x['worker']
    assert db.execute('SELECT routing_key FROM photo.outbox WHERE tenant_id=%s',(x['tenant'],)).fetchone()[0]=='worker.'+str(x['worker'])+'.io.verify'
    x['command']=command
    db.execute('SET LOCAL ROLE photo_worker')
    assert claim(db,x,worker=uuid4())[0]=='unauthorized'
    from psycopg import errors
    with pytest.raises(errors.InsufficientPrivilege),db.transaction():
        db.execute('SELECT * FROM photo.claim_task_reserved(%s,1,%s,%s)',(x['task'],x['worker'],Jsonb(command)))
    assert claim(db,x)[0]=='claimed'
    assert commit(db,x,1)[0]=='committed'
    db.execute('RESET ROLE')
    assert db.execute('SELECT state FROM photo.resource_reservations WHERE task_id=%s',(x['task'],)).fetchone()[0]=='released'


def test_admission_checks_fresh_capacity_and_worker_qualification(db):
    x=ready_task(db)
    assert reserve(db,x,ram=10000001)[0]=='waiting_resource'
    db.execute("UPDATE photo.resource_nodes SET reported_at=clock_timestamp()-interval '31 seconds' WHERE node_id=%s",(x['node'],))
    assert reserve(db,x)[0]=='waiting_resource'
    db.execute('UPDATE photo.worker_registrations SET certificate_sha256=%s WHERE worker_id=%s',('f'*64,x['worker']))
    db.execute('SET LOCAL ROLE photo_worker')
    assert not db.execute("SELECT has_function_privilege('photo_worker','photo.report_worker_capacity(uuid,uuid,bigint,bigint,bigint)','EXECUTE')").fetchone()[0]
    assert db.execute('SELECT photo.heartbeat_cpu_worker(%s,%s,%s,9999999999,9999999999)',(x['worker'],'media',['asset.verify'])).fetchone()[0] is True
    db.execute('RESET ROLE')
    assert db.execute('SELECT ram_available_bytes FROM photo.resource_nodes WHERE node_id=%s',(x['node'],)).fetchone()[0]==10000000
    db.execute("UPDATE photo.worker_registrations SET qualified_operations='{}' WHERE worker_id=%s",(x['worker'],))
    assert reserve(db,x)[0]=='waiting_worker'
    assert db.execute('SELECT state FROM photo.tasks WHERE id=%s',(x['task'],)).fetchone()[0]=='ready'
    assert db.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s',(x['tenant'],)).fetchone()[0]==0


def test_unsealed_cancelled_and_missing_input(db):
    x=ready_task(db)
    db.execute('UPDATE photo.runs SET graph_sealed=false WHERE id=%s',(x['run'],))
    assert reserve(db,x)[0]=='unsealed'
    db.execute('UPDATE photo.runs SET graph_sealed=true,cancel_requested=true WHERE id=%s',(x['run'],))
    assert reserve(db,x)[0]=='cancelled'
    db.execute('UPDATE photo.runs SET cancel_requested=false WHERE id=%s',(x['run'],))
    # The immutable ready-artifact guard now rejects this invalid graph before
    # it can reach admission; admission's input_unavailable remains a recovery
    # defense for rows created before the guard existed.
    with pytest.raises(errors.ForeignKeyViolation,match='ARTIFACT_REFERENCE_MISSING'),db.transaction():
        db.execute('UPDATE photo.tasks SET inputs=%s WHERE id=%s',(Jsonb([dict(artifact_id=str(uuid4()),sha256='f'*64)]),x['task']))


def gpu_task(db):
    x=ready_task(db)
    db.execute("UPDATE photo.tasks SET resource_class='gpu',operation='features.scene',execution_profile=%s WHERE id=%s",(Jsonb(dict(backend='mps',model_lock_id='e'*64)),x['task']))
    db.execute("UPDATE photo.resource_slots SET resource_class='gpu' WHERE node_id=%s",(x['node'],))
    db.execute('UPDATE photo.resource_nodes SET vram_budget_bytes=10000000,vram_available_bytes=10000000 WHERE node_id=%s',(x['node'],))
    db.execute("UPDATE photo.worker_registrations SET resource_classes='[\"gpu\"]',backends='[\"mps\"]',qualified_operations='{\"features.scene\":[\"mps\"]}' WHERE worker_id=%s",(x['worker'],))
    return x


def test_model_lock_and_gpu_quota_release_exactly_once(db):
    x=gpu_task(db)
    db.execute('UPDATE photo.worker_registrations SET lock_id=%s WHERE worker_id=%s',('f'*64,x['worker']))
    assert reserve(db,x,vram=1024)[0]=='waiting_worker'
    db.execute('UPDATE photo.worker_registrations SET lock_id=%s WHERE worker_id=%s',('e'*64,x['worker']))
    status,command=reserve(db,x,vram=1024)
    assert status=='queued' and command['model']==dict(id='dinov2_vits14',lock_id='e'*64)
    assert db.execute('SELECT active_gpu_tasks FROM photo.tenant_usage WHERE tenant_id=%s',(x['tenant'],)).fetchone()[0]==1
    x['command']=command
    assert claim(db,x)[0]=='claimed'
    assert commit(db,x,1)[0]=='committed'
    assert commit(db,x,1)[0]=='replayed'
    assert db.execute('SELECT active_gpu_tasks FROM photo.tenant_usage WHERE tenant_id=%s',(x['tenant'],)).fetchone()[0]==0


def test_physical_node_budget_concurrent_tenants(database):
    import psycopg
    from concurrent.futures import ThreadPoolExecutor
    with psycopg.connect(**database) as conn:
        a=ready_task(conn);b=ready_task(conn)
        conn.execute("UPDATE photo.resource_slots SET node_id=%s,slot_name='media2' WHERE node_id=%s",(a['node'],b['node']))
        conn.execute('UPDATE photo.worker_registrations SET node_id=%s WHERE worker_id=%s',(a['node'],b['worker']))
    def dispatch(x):
        with psycopg.connect(**database) as conn:
            conn.execute('SET LOCAL ROLE photo_scheduler')
            return reserve(conn,x,ram=6000000,node=a['node'])[0]
    with ThreadPoolExecutor(max_workers=2) as pool:
        result=list(pool.map(dispatch,[a,b]))
    assert sorted(result)==['queued','waiting_resource']
    with psycopg.connect(**database) as conn:
        assert conn.execute("SELECT sum(ram_bytes) FROM photo.resource_reservations WHERE node_id=%s AND state='reserved'",(a['node'],)).fetchone()[0]==6000000


def test_fairness_selects_lower_virtual_finish(db):
    a=ready_task(db);b=ready_task(db)
    db.execute('INSERT INTO photo.scheduler_tenants(tenant_id,virtual_finish) VALUES(%s,8),(%s,2)',(a['tenant'],b['tenant']))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT task_id FROM photo.pick_ready_task()').fetchone()[0]==b['task']


def test_preview_streak_yields_to_batch(db):
    x=ready_task(db)
    preview=uuid4()
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,priority) SELECT %s,tenant_id,run_id,'preview',operation,operation_version,resource_class,'ready',inputs,params_artifact_id,input_digest,execution_profile,1 FROM photo.tasks WHERE id=%s",(preview,x['task']))
    db.execute('INSERT INTO photo.scheduler_tenants(tenant_id,preview_streak) VALUES(%s,0)',(x['tenant'],))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT task_id FROM photo.pick_ready_task()').fetchone()[0]==preview
    db.execute('RESET ROLE')
    db.execute('UPDATE photo.scheduler_tenants SET preview_streak=5 WHERE tenant_id=%s',(x['tenant'],))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT task_id FROM photo.pick_ready_task()').fetchone()[0]==x['task']


def test_operation_backend_qualification_is_not_cartesian_product(db):
    x=ready_task(db)
    db.execute("UPDATE photo.worker_registrations SET backends='[\"cpu\",\"mps\"]',qualified_operations='{\"asset.verify\":[\"mps\"],\"features.scene\":[\"cpu\"]}' WHERE worker_id=%s",(x['worker'],))
    assert reserve(db,x)[0]=='waiting_worker'
