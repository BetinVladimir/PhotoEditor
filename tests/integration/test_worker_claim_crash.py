"""Real worker dies after claim; actual deadline expiry, no lease-time patch."""
import json
import os
from pathlib import Path
import subprocess
import time
from datetime import datetime,timezone
import pika
import psycopg
from psycopg.conninfo import make_conninfo
from photo_worker.contracts.wire import TaskCommand,TaskError,TaskMetrics
from photo_worker.runtime.database import TaskDatabase,Lease
from test_task_leases import queued_task,manifest

ROOT=Path(__file__).resolve().parents[2]


def test_worker_claim_crash_reaper_invalidates_late_commit(database,rabbit_vhost):
    with psycopg.connect(**database) as conn:
        case=queued_task(conn)
        # Fixture timeout is fixed before dispatch/claim. No persisted lease,
        # deadline, state, fence or retry clock is patched after execution starts.
        conn.execute('UPDATE photo.tasks SET timeout_seconds=2 WHERE id=%s',(case['task'],))
    connection=pika.BlockingConnection(pika.URLParameters(rabbit_vhost))
    try:
        channel=connection.channel()
        queue='q.worker.'+str(case['worker'])+'.media'
        channel.queue_declare(queue,durable=True,arguments={'x-queue-type':'quorum'})
        channel.confirm_delivery()
        channel.basic_publish('',queue,json.dumps(case['command']).encode(),pika.BasicProperties(delivery_mode=2),mandatory=True)
        env={**os.environ,'PYTHONPATH':str(ROOT/'workers')+':'+str(ROOT/'src'),
             'PHOTO_TEST_RESTART_DSN':make_conninfo(**database),'PHOTO_TEST_RESTART_BROKER':str(rabbit_vhost),
             'PHOTO_TEST_RESTART_WORKER':str(case['worker']),'PHOTO_TEST_RESTART_MODE':'crash-after-claim'}
        proc=subprocess.run([str(ROOT/'.venv-platform/bin/python'),str(ROOT/'tests/integration/worker_commit_restart_probe.py')],env=env,cwd=ROOT,capture_output=True,text=True,timeout=30)
        assert proc.returncode==74 and 'CLAIM_DURABLE_BEFORE_OPERATION_COMMIT' in proc.stdout
        with psycopg.connect(**database) as conn:
            row=conn.execute('SELECT state,attempt,fencing_token,lease_until,deadline,execution_profile,input_digest FROM photo.tasks WHERE id=%s',(case['task'],)).fetchone()
            assert row[0]=='running' and row[1]==1
            assert conn.execute('SELECT finished_at,error FROM photo.task_attempts WHERE task_id=%s',(case['task'],)).fetchone()==(None,None)
        old=Lease(TaskCommand.model_validate_json(json.dumps(case['command'])),case['worker'],row[2],row[3],row[4],row[5],row[6])
        delay=(row[4]-datetime.now(timezone.utc)).total_seconds()+.05
        assert delay<3
        if delay>0:time.sleep(delay)
        with psycopg.connect(**database) as conn:
            conn.execute('SET LOCAL ROLE photo_scheduler')
            assert conn.execute('SELECT photo.reap_tasks(10)').fetchone()==(1,)
            assert conn.execute('SELECT photo.reap_tasks(10)').fetchone()==(0,)
        with psycopg.connect(**database) as conn:
            snapshot=conn.execute('SELECT to_jsonb(t) FROM photo.tasks t WHERE id=%s',(case['task'],)).fetchone()
            assert snapshot[0]['state']=='retry_wait' and snapshot[0]['fencing_token']==row[2]+1
            assert snapshot[0]['lease_until'] is None and snapshot[0]['next_attempt_at'] is None
            assert conn.execute("SELECT error->>'code',finished_at IS NOT NULL FROM photo.task_attempts WHERE task_id=%s",(case['task'],)).fetchone()==('TIMEOUT',True)
            events=conn.execute('SELECT to_jsonb(o) FROM photo.outbox o WHERE tenant_id=%s ORDER BY id',(case['tenant'],)).fetchall()
            assert len(events)==1
        worker=TaskDatabase(make_conninfo(**database))
        assert worker.commit_success(old,manifest().encode())[0]=='stale'
        assert worker.commit_failure(old,TaskError(code='INFERENCE_FAILED',retryable=True,message_key='errors.inference_failed',args={}),TaskMetrics(duration_ms=0,peak_memory_bytes=0))=='stale'
        with psycopg.connect(**database) as conn:
            assert conn.execute('SELECT to_jsonb(t) FROM photo.tasks t WHERE id=%s',(case['task'],)).fetchone()==snapshot
            assert conn.execute('SELECT to_jsonb(o) FROM photo.outbox o WHERE tenant_id=%s ORDER BY id',(case['tenant'],)).fetchall()==events
            assert conn.execute('SELECT count(*) FROM photo.task_results WHERE task_id=%s',(case['task'],)).fetchone()==(0,)
            assert conn.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(case['task'],)).fetchone()==(0,)
            assert conn.execute("SELECT count(*) FROM photo.resource_reservations WHERE task_id=%s AND state='reserved'",(case['task'],)).fetchone()==(0,)
        until=time.monotonic()+5
        while channel.queue_declare(queue,passive=True).method.message_count!=1:
            assert time.monotonic()<until
            time.sleep(.05)
    finally:
        connection.close()
