"""Real PostgreSQL terminal gates; seeded metadata is not producer/model proof."""
from hashlib import sha256
import json
from uuid import UUID, uuid4, uuid5

import pytest
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes
from test_v27_target_checkpoint import seed_checkpoint, save

OPS = ('mask.person', 'face.detect', 'face.landmarks', 'face.parse', 'mask.face_skin', 'defects.propose')


def seed_terminal_target(db, operation, state='failed', sealed=True, corrupt_field=None):
    packet, case, _, manifest = seed_checkpoint(db,terminal_anchor=True)
    tenant = case['Tenant']
    detector = UUID(packet['run_id'])
    detector_task = uuid5(uuid5(detector, 'workflow'), 'face.detect:0')
    config = json.loads(packet['config_text'])
    config['v27_detector_prepare']['task_id'] = str(detector_task)
    params = packet['registration']['params']['artifact_id']
    def task(run, task_id, op, task_state):
        db.execute("""INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,state)
        VALUES(%s,%s,%s,%s,%s,'1',%s,'[]',%s,%s,'{}',%s)""", (task_id,tenant,run,op+':0',op,'cpu' if op in ('mask.face_skin','defects.propose') else 'gpu',params,'d'*64,task_state))
    task(detector, detector_task, 'face.detect', 'ready')
    db.execute('SET LOCAL ROLE photo_fn_owner')
    db.execute('UPDATE photo.runs SET v27_initial_recovery_enabled=true WHERE id=%s', (detector,))
    db.execute('RESET ROLE')
    body = canonical_bytes(manifest).decode()
    assert save(db,packet,body)
    entry = manifest['entries'][1]
    namespace = UUID(entry['namespace'])
    run = uuid5(namespace, 'semantic.defect.prepare:reference' if operation=='defects.propose' else 'semantic.'+operation+':reference')
    task_id = uuid5(uuid5(run,'workflow'), operation+':0')
    key = {'mask.person':'person','face.detect':'detector','mask.face_skin':'skin','defects.propose':'defect'}.get(operation,'face')
    native = dict(config['v27_detector_prepare'],source=entry['binding'],target_namespace=str(namespace),target_ordinal=1,
                  target_stage_id=str(uuid5(namespace,'stage:'+operation)),manifest_sha=sha256(body.encode()).hexdigest(),
                  operation=operation,task_id=str(task_id),input_digest='d'*64,params=dict(artifact_id=params))
    if corrupt_field is not None:
        native[corrupt_field]=0 if corrupt_field=='target_ordinal' else {} if corrupt_field in ('source','intent','snapshot','params') else str(uuid4())
    child_config = dict(project_id=case['Project'], **{'v27_target_'+key+'_prepare':native})
    db.execute("""INSERT INTO photo.runs(id,tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by,status,graph_sealed,failure_policy,cancel_requested)
      VALUES(%s,%s,'v2',%s,%s,%s,%s,%s,%s,'fail_fast',%s)""",(run,tenant,case['Snapshot'],Jsonb(child_config),'e'*64,case['Actor'],'queued' if sealed else 'cancelling',sealed,not sealed))
    task(run,task_id,operation,state)
    return packet,case,run,task_id,child_config,key


def observe(db,packet,case):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
        rows=db.execute('SELECT operation FROM photo.v27_native_initial_terminal_state(%s,%s,%s,%s,%s)',
          (case['Tenant'],case['Project'],case['Actor'],case['Session'],packet['parent_run_id'])).fetchall()
        db.execute('RESET ROLE')
        return rows


@pytest.mark.parametrize('operation',OPS)
@pytest.mark.parametrize('state,sealed,expected',[('ready',True,False),('retry_wait',True,False),('failed',True,True),('cancelled',True,True),('skipped',True,True),('blocked',False,True),('failed',False,False),('ready',False,False),('retry_wait',False,False)])
def test_target_terminal_matrix(db,operation,state,sealed,expected):
    packet,case,run,task,config,key=seed_terminal_target(db,operation,state,sealed)
    assert observe(db,packet,case)==[(operation if expected else None,)]
    db.execute('SET LOCAL ROLE photo_scheduler')
    claimed=db.execute('SELECT detector_run_id,lease_token FROM photo.claim_v27_native_initial_pipeline(%s,60)',(case['Project'],)).fetchone()
    db.execute('RESET ROLE')
    assert claimed and str(claimed[0])==packet['run_id']
    def record(token):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            result=db.execute("SELECT photo.record_v27_native_initial_pipeline_handoff(%s,%s,%s,'failed')",(case['Tenant'],claimed[0],token)).fetchone()[0]
            db.execute('RESET ROLE')
            return result
    assert not record(uuid4())
    assert record(claimed[1])==expected
    if expected:
        assert not record(claimed[1])
        assert db.execute('SELECT v27_initial_recovery_done,v27_initial_recovery_token FROM photo.runs WHERE id=%s',(claimed[0],)).fetchone()==(True,None)
    assert db.execute('SELECT count(*) FROM photo.task_results WHERE task_id=%s',(task,)).fetchone()==(0,)
    assert db.execute('SELECT count(*) FROM photo.runs WHERE id=%s',(packet['parent_run_id'],)).fetchone()==(0,)


@pytest.mark.parametrize('field',('parent_run_id','auth_session_id','request_sha','source','intent','snapshot','target_namespace','target_ordinal','target_stage_id','manifest_sha','task_id','input_digest','params'))
def test_target_terminal_refuses_foreign_identity(db,field):
    packet,case,run,task,config,key=seed_terminal_target(db,'mask.person',corrupt_field=field)
    assert observe(db,packet,case)==[(None,)]


@pytest.mark.parametrize('mode',('failure','cancel'))
@pytest.mark.parametrize('operation',OPS)
def test_target_terminal_actual_process_one(database,operation,mode):
    import os
    import subprocess
    from pathlib import Path
    import psycopg
    root=Path(__file__).resolve().parents[2]
    with psycopg.connect(**database) as conn:
        packet,case,run,task,config,key=seed_terminal_target(conn,operation,'ready' if mode=='cancel' else 'failed')
    fixture=dict(Tenant=case['Tenant'],Project=case['Project'],Actor=case['Actor'],Session=case['Session'],Parent=packet['parent_run_id'],Run=str(run),Operation=operation,Mode=mode)
    env={**os.environ,'PHOTO_TEST_DATABASE_URL':psycopg.conninfo.make_conninfo(**database),'PHOTO_TEST_V27_TARGET_TERMINAL_CASE':json.dumps(fixture)}
    result=subprocess.run(['go','test','-count=1','-v','-run','^TestIntegrationNativeTargetTerminalProcessOne$','./internal/semanticproposal'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr
    assert 'V27_TARGET_TERMINAL_PROCESS_ONE_OK' in result.stdout
    with psycopg.connect(**database) as conn:
        assert conn.execute('SELECT v27_initial_recovery_done,v27_initial_recovery_token,v27_initial_recovery_after FROM photo.runs WHERE id=%s',(packet['run_id'],)).fetchone()==(True,None,None)
        assert conn.execute('SELECT count(*) FROM photo.runs WHERE id=%s',(packet['parent_run_id'],)).fetchone()==(0,)
        assert conn.execute('SELECT state,canonical_result_id FROM photo.tasks WHERE id=%s',(task,)).fetchone()==('cancelled' if mode=='cancel' else 'failed',None)


@pytest.mark.parametrize('operation',OPS)
def test_target_terminal_recorder_holds_target_rows(database,operation):
    import psycopg
    from psycopg import errors
    with psycopg.connect(**database) as conn:
        packet,case,run,task,config,key=seed_terminal_target(conn,operation)
        conn.execute('SET LOCAL ROLE photo_scheduler')
        detector,token=conn.execute('SELECT detector_run_id,lease_token FROM photo.claim_v27_native_initial_pipeline(%s,60)',(case['Project'],)).fetchone()
        conn.execute('RESET ROLE')
    with psycopg.connect(**database) as reader, psycopg.connect(**database) as writer:
        reader.execute('SET LOCAL ROLE photo_scheduler')
        assert reader.execute("SELECT photo.record_v27_native_initial_pipeline_handoff(%s,%s,%s,'failed')",(case['Tenant'],detector,token)).fetchone()==(True,)
        with pytest.raises(errors.LockNotAvailable):
            with writer.transaction():
                writer.execute("SET LOCAL lock_timeout='150ms'")
                writer.execute('UPDATE photo.tasks SET cancel_requested=NOT cancel_requested WHERE id=%s',(task,))
        reader.rollback()
        with writer.transaction(force_rollback=True):
            writer.execute("SET LOCAL lock_timeout='150ms'")
            assert writer.execute('UPDATE photo.tasks SET cancel_requested=NOT cancel_requested WHERE id=%s',(task,)).rowcount==1


@pytest.mark.parametrize('operation',OPS)
def test_target_terminal_unsealed_attempt_is_not_authority(db,operation):
    packet,case,run,task,config,key=seed_terminal_target(db,operation,'blocked',False)
    assert observe(db,packet,case)==[(operation,)]
    db.execute('UPDATE photo.tasks SET attempt=1 WHERE id=%s',(task,))
    assert observe(db,packet,case)==[(None,)]


@pytest.mark.parametrize('operation',OPS)
def test_target_terminal_wrong_resource_class_is_not_authority(db,operation):
    packet,case,run,task,config,key=seed_terminal_target(db,operation)
    assert observe(db,packet,case)==[(operation,)]
    db.execute("UPDATE photo.tasks SET resource_class=CASE WHEN resource_class='cpu' THEN 'gpu' ELSE 'cpu' END WHERE id=%s",(task,))
    assert observe(db,packet,case)==[(None,)]
