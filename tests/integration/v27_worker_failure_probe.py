"""Actual claimed worker failure protocol; injected error, no model inference."""
from dataclasses import replace
import json
import os
from uuid import UUID
import psycopg
from photo_worker.contracts.wire import TaskCommand, TaskError, TaskMetrics
from photo_worker.runtime.database import Lease, TaskDatabase
from wire_runtime_login import worker_login


def main():
    dsn=os.environ['PHOTO_TEST_DATABASE_URL']
    task=UUID(os.environ['PHOTO_TEST_V27_GATEWAY_TASK'])
    with psycopg.connect(dsn) as db:
        row=db.execute('SELECT worker_id,attempt,fencing_token,lease_until,deadline,execution_profile,input_digest,dispatch_command FROM photo.tasks WHERE id=%s',(task,)).fetchone()
    command=TaskCommand.model_validate_json(json.dumps(row[7]))
    assert command.task_id==task and command.operation in ('face.detect','mask.person','face.landmarks','face.parse','mask.face_skin','defects.propose')
    lease=Lease(command,row[0],row[2],row[3],row[4],row[5],row[6])
    code=os.environ.get('PHOTO_TEST_V27_FAILURE_CODE','INFERENCE_FAILED')
    assert code in ('INFERENCE_FAILED','RESOURCE_OOM')
    error=TaskError(code=code,retryable=os.environ.get('PHOTO_TEST_V27_FAILURE_RETRYABLE')=='1',message_key='errors.resource_oom' if code=='RESOURCE_OOM' else 'worker.inference_failed',args={})
    metrics=TaskMetrics(duration_ms=0,peak_memory_bytes=0,bytes_written=0)
    with worker_login(dsn, task) as runtime_dsn:
        worker=TaskDatabase(runtime_dsn)
        old_raw=os.environ.get('PHOTO_TEST_V27_OLD_FAILURE_IDENTITY')
        stale_only=os.environ.get('PHOTO_TEST_V27_FAILURE_STALE_ONLY')=='1'
        assert not stale_only or old_raw
        if old_raw:
            old=json.loads(old_raw)
            assert str(command.task_id)==old['task'] and command.attempt==old['attempt']+1 and lease.fence>old['fence']
            stale=replace(lease,command=command.model_copy(update={'attempt':old['attempt']}),worker_id=UUID(old['worker']),fence=old['fence'])
            with psycopg.connect(dsn) as snapshot:
                before=snapshot.execute('SELECT to_jsonb(t) FROM photo.tasks t WHERE id=%s',(task,)).fetchone()
            assert worker.commit_failure(stale,error.model_copy(update={'retryable':True}),metrics)=='stale'
            with psycopg.connect(dsn) as snapshot:
                assert snapshot.execute('SELECT to_jsonb(t) FROM photo.tasks t WHERE id=%s',(task,)).fetchone()==before
            print('V27_WORKER_OLD_ATTEMPT_STALE_OK')
            if stale_only:
                print('V27_WORKER_RETRY_SUCCESS_READY_OK')
                return
        assert worker.commit_failure(replace(lease,fence=lease.fence+1),error,metrics)=='stale'
        assert worker.commit_failure(lease,error,metrics)=='committed'
        assert worker.commit_failure(lease,error,metrics)=='replayed'
        assert worker.commit_failure(lease,error.model_copy(update={'args':{'changed':True}}),metrics)=='conflict'
        with psycopg.connect(dsn) as db:
            assert db.execute('SELECT state,cancel_requested,canonical_result_id,next_attempt_at FROM photo.tasks WHERE id=%s',(task,)).fetchone()==('retry_wait',False,None,None)
            attempt=db.execute('SELECT error,finished_at FROM photo.task_attempts WHERE task_id=%s AND attempt=%s',(task,command.attempt)).fetchone()
            assert attempt[0]==error.model_dump(mode='json') and attempt[1] is not None
            assert db.execute('SELECT count(*) FROM photo.task_results WHERE task_id=%s',(task,)).fetchone()==(0,)
            assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(task,)).fetchone()==(0,)
            assert db.execute("SELECT count(*) FROM photo.resource_reservations WHERE task_id=%s AND state='reserved'",(task,)).fetchone()==(0,)

            config,actor=db.execute('SELECT config_snapshot,created_by FROM photo.runs WHERE tenant_id=%s AND id=%s',(command.tenant_id,command.run_id)).fetchone()
            key={'mask.person':'v27_person_prepare','face.detect':'v27_detector_prepare','face.landmarks':'v27_face_prepare','face.parse':'v27_face_prepare','mask.face_skin':'v27_skin_prepare','defects.propose':'v27_defect_native_prepare'}[command.operation]
            if key not in config:
                key={'mask.person':'v27_target_person_prepare','face.detect':'v27_target_detector_prepare','face.landmarks':'v27_target_face_prepare','face.parse':'v27_target_face_prepare','mask.face_skin':'v27_target_skin_prepare','defects.propose':'v27_target_defect_prepare'}[command.operation]
            native=config[key]
            db.execute('SET LOCAL ROLE photo_api')
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(command.tenant_id),))
            assert db.execute('SELECT * FROM photo.v27_native_initial_terminal_state(%s,%s,%s,%s,%s)',(command.tenant_id,config['project_id'],actor,native['auth_session_id'],native['parent_run_id'])).fetchall()==[(None,)]
        print('V27_WORKER_FAILURE_COMMITTED_OK')


if __name__=='__main__':
    main()
