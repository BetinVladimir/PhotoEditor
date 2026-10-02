"""Controlled failed-task metadata verifies cross-tenant finalizer scheduling."""
from uuid import uuid4, uuid5
import pytest
import psycopg
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes
from hashlib import sha256
from layer_original_fixture import seed_original_composition


def seed_failed_batch(db,frames=1):
    case=seed_original_composition(db)
    tenant,actor,project,snapshot,session=(case[k] for k in ('Tenant','Actor','Project','Snapshot','Session'))
    batch=uuid4();child=uuid5(batch,'render.batch:0')
    tasks=[uuid5(uuid5(child,'workflow'),f'render.compose:{ordinal}') for ordinal in range(frames)]
    params=case['Artifact']['artifact_id'];digest='b'*64
    config=dict(project_id=project,v26_render=dict(auth_session_id=session,nodes=[dict(task_id=str(task),params=case['Artifact'],input_digest=digest) for task in tasks]))
    config_sha=sha256(canonical_bytes(config)).hexdigest()
    manifest=dict(schema_version=1,project_id=project,input_snapshot_id=snapshot,auth_session_id=session,policy_sha='a'*64,parts=[dict(run_id=str(child),config_sha=config_sha,frame_count=frames,first_ordinal=0,output_bytes=frames)])
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES(%s,%s,'bulk','draft',%s,%s,%s,%s)",(batch,tenant,snapshot,Jsonb(manifest),sha256(canonical_bytes(manifest)).hexdigest(),actor))
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES(%s,%s,'v2','failed',%s,%s,%s,%s,'fail_fast',true)",(child,tenant,snapshot,Jsonb(config),config_sha,actor))
    for ordinal,task in enumerate(tasks):
        db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,terminal_reason) VALUES(%s,%s,%s,%s,'render.compose','1','gpu','failed','[]',%s,%s,'{}','fixture_failure')",(task,tenant,child,f'render.compose:{ordinal}',params,digest))
    return tenant,batch


def test_inactive_tenant_does_not_starve_active_batch(db):
    inactive,older=seed_failed_batch(db)
    active,newer=seed_failed_batch(db)
    db.execute("UPDATE photo.runs SET created_at=clock_timestamp()-interval '2 days' WHERE id=%s",(older,))
    # Only change lifecycle metadata; no deletion job, object cleanup or purge executes.
    db.execute("UPDATE photo.tenants SET status='deleting' WHERE id=%s",(inactive,))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.finalize_v26_render_batch()').fetchone()==(True,)
    assert db.execute('SELECT photo.finalize_v26_render_batch()').fetchone()==(False,)
    db.execute('RESET ROLE')
    assert db.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(older,)).fetchone()==('draft',1)
    assert db.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(newer,)).fetchone()==('failed',2)
    assert db.execute('SELECT count(*) FROM photo.run_events WHERE run_id=%s',(older,)).fetchone()==(0,)
    # The skipped row remains eligible after active status is restored in this rollback fixture.
    db.execute("UPDATE photo.tenants SET status='active' WHERE id=%s",(inactive,))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.finalize_v26_render_batch()').fetchone()==(True,)
    assert db.execute('SELECT photo.finalize_v26_render_batch()').fetchone()==(False,)
    db.execute('RESET ROLE')
    assert db.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(older,)).fetchone()==('failed',2)


@pytest.mark.parametrize('frames,terminal',[(1,'completed'),(2,'completed_with_errors')])
def test_successful_batch_requires_current_verified_canonical_root(db,frames,terminal):
    tenant,batch=seed_failed_batch(db,frames)
    child,task=db.execute("SELECT r.id,t.id FROM photo.runs r JOIN photo.tasks t ON t.run_id=r.id AND t.tenant_id=r.tenant_id WHERE r.tenant_id=%s AND r.kind='v2' AND t.node_key='render.compose:0'",(tenant,)).fetchone()
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,now(),now(),'cuda','{}')",(tenant,task,uuid4()))
    root=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'test',%s,'v1',%s,64,'application/json','document',now(),%s,1,1) RETURNING id",(tenant,str(uuid4()),'a'*64,task)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET execution_profile='{\"renderer_version\":\"v2.2\"}' WHERE id=%s",(task,))
    db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) SELECT %s,tenant_id,(config_snapshot->>'project_id')::uuid,id,input_snapshot_id,'preview','v2.2',%s FROM photo.runs WHERE id=%s",(uuid5(child,'workflow'),'a'*64,child))
    db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version,completed_at) SELECT tenant_id,%s,1,1,%s,%s,%s,id,'document',bucket,object_key,sha256,bytes,media_type,'ready',object_version,now() FROM photo.artifacts WHERE id=%s",(task,uuid4(),'a'*64,uuid4(),root))
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,task,Jsonb([dict(artifact_id=str(root),sha256='a'*64)]),'c'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',attempt=1,fencing_token=1,canonical_result_id=%s,terminal_reason=NULL WHERE id=%s",(result,task))
    db.execute("UPDATE photo.runs SET status=%s WHERE id=%s",('failed' if frames>1 else 'completed',child))
    for mutation,identity in [
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",root),
        ("UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s",root),
        ("UPDATE photo.tasks SET fencing_token=2 WHERE id=%s",task),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(mutation,(identity,))
            db.execute('SET LOCAL ROLE photo_scheduler')
            assert db.execute('SELECT photo.finalize_v26_render_batch()').fetchone()==(False,)
            db.execute('RESET ROLE')
            assert db.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(batch,)).fetchone()==('draft',1)
    for mutation,identity in [
        ("UPDATE photo.artifacts SET object_version=NULL WHERE id=%s",root),
        ("UPDATE photo.artifacts SET sha256=repeat('b',64) WHERE id=%s",root),
        ("UPDATE photo.task_results SET fencing_token=2 WHERE id=%s",result),
    ]:
        with pytest.raises(psycopg.errors.CheckViolation,match='immutable field'),db.transaction():
            db.execute(mutation,(identity,))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.finalize_v26_render_batch()').fetchone()==(True,)
    assert db.execute('SELECT photo.finalize_v26_render_batch()').fetchone()==(False,)
    db.execute('RESET ROLE')
    assert db.execute('SELECT status,revision,graph_sealed FROM photo.runs WHERE id=%s',(batch,)).fetchone()==(terminal,2,True)
    assert db.execute("SELECT count(*) FROM photo.run_events WHERE run_id=%s AND type=%s",(batch,'run.'+terminal)).fetchone()==(1,)
    assert db.execute("SELECT * FROM photo.v26_render_batch_counts(%s,%s)",(tenant,batch)).fetchone()==(frames,1,frames-1,0,0)
