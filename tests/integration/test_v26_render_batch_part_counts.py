from uuid import uuid4
from test_v26_render_finalizer_tenant_scope import seed_failed_batch


def test_part_counts_ignore_undeclared_and_mismatched_tasks(db):
    tenant,batch=seed_failed_batch(db)
    actor,session=db.execute("SELECT created_by,config_snapshot->>'auth_session_id' FROM photo.runs WHERE id=%s",(batch,)).fetchone()
    child,task=db.execute("SELECT run_id,id FROM photo.tasks WHERE tenant_id=%s",(tenant,)).fetchone()
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,terminal_reason) SELECT %s,tenant_id,run_id,'unplanned','render.compose','1',resource_class,'failed',inputs,params_artifact_id,input_digest,execution_profile,'fixture_failure' FROM photo.tasks WHERE id=%s",(uuid4(),task))
    query='SELECT * FROM photo.v26_render_batch_parts(%s,%s,%s,%s)'
    args=(tenant,batch,actor,session)
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute(query,args).fetchone()==(0,child,0,1,'failed',1,0,1,0)
    db.execute('RESET ROLE')
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.tasks SET input_digest=repeat('c',64) WHERE id=%s",(task,))
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute(query,args).fetchone()==(0,child,0,1,'failed',0,0,0,0)
    source=db.execute('SELECT artifact_id FROM photo.snapshot_items WHERE tenant_id=%s LIMIT 1',(tenant,)).fetchone()[0]
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.tasks SET params_artifact_id=%s WHERE id=%s',(source,task))
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute(query,args).fetchone()==(0,child,0,1,'failed',0,0,0,0)
