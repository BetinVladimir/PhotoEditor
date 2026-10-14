"""Actual OS-process restart + Rabbit redelivery, SQL media fixture.

One persistent message is published. First process dies after durable SQL commit
before Transport sees the return/ACK. A new process consumes the same message.
This is transport/executor evidence, not native inference/qualification evidence.
"""
import json
import os
from pathlib import Path
import subprocess
import time
import pika
import psycopg
from psycopg.conninfo import make_conninfo
from test_task_leases import queued_task


ROOT=Path(__file__).resolve().parents[2]


def test_worker_death_after_commit_redelivers_without_duplicate_effects(database,rabbit_vhost):
    with psycopg.connect(**database) as conn:
        case=queued_task(conn)
    queue='q.worker.'+str(case['worker'])+'.media'
    connection=pika.BlockingConnection(pika.URLParameters(rabbit_vhost))
    try:
        channel=connection.channel()
        channel.queue_declare(queue,durable=True,arguments={'x-queue-type':'quorum'})
        channel.confirm_delivery()
        channel.basic_publish('',queue,json.dumps(case['command']).encode(),pika.BasicProperties(content_type='application/json',delivery_mode=2),mandatory=True)
        env={**os.environ,'PYTHONPATH':str(ROOT/'workers')+':'+str(ROOT/'src'),
             'PHOTO_TEST_RESTART_DSN':make_conninfo(**database),
             'PHOTO_TEST_RESTART_BROKER':str(rabbit_vhost),
             'PHOTO_TEST_RESTART_WORKER':str(case['worker'])}
        def run(mode):
            return subprocess.run([str(ROOT/'.venv-platform/bin/python'),str(ROOT/'tests/integration/worker_commit_restart_probe.py')],env={**env,'PHOTO_TEST_RESTART_MODE':mode},cwd=ROOT,capture_output=True,text=True,timeout=30)
        first=run('crash-after-commit')
        assert first.returncode==73, 'First worker did not stop at the durable-commit boundary'
        assert 'DURABLE_COMMIT_BEFORE_ACK' in first.stdout
        with psycopg.connect(**database) as conn:
            before=conn.execute('SELECT to_jsonb(t) FROM photo.tasks t WHERE id=%s',(case['task'],)).fetchone()
            assert before[0]['state']=='succeeded'
            assert conn.execute('SELECT count(*) FROM photo.task_results WHERE task_id=%s',(case['task'],)).fetchone()==(1,)
            assert conn.execute('SELECT count(*) FROM photo.task_attempts WHERE task_id=%s',(case['task'],)).fetchone()==(1,)
            outbox=conn.execute('SELECT to_jsonb(o) FROM photo.outbox o WHERE tenant_id=%s ORDER BY id',(case['tenant'],)).fetchall()
            assert len(outbox)==1
        until=time.monotonic()+5
        while channel.queue_declare(queue,passive=True).method.message_count!=1:
            assert time.monotonic()<until, 'Unacknowledged message was not returned by broker'
            time.sleep(.05)
        second=run('restart')
        assert second.returncode==0, 'Restarted worker failed terminal redelivery'
        assert 'RESTART_TERMINAL_REDELIVERY' in second.stdout
        assert channel.queue_declare(queue,passive=True).method.message_count==0
        with psycopg.connect(**database) as conn:
            assert conn.execute('SELECT to_jsonb(t) FROM photo.tasks t WHERE id=%s',(case['task'],)).fetchone()==before
            assert conn.execute('SELECT count(*) FROM photo.task_results WHERE task_id=%s',(case['task'],)).fetchone()==(1,)
            assert conn.execute('SELECT count(*) FROM photo.task_attempts WHERE task_id=%s',(case['task'],)).fetchone()==(1,)
            assert conn.execute('SELECT to_jsonb(o) FROM photo.outbox o WHERE tenant_id=%s ORDER BY id',(case['tenant'],)).fetchall()==outbox
            assert conn.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(case['task'],)).fetchone()==(0,)
            assert conn.execute("SELECT count(*) FROM photo.resource_reservations WHERE task_id=%s AND state='reserved'",(case['task'],)).fetchone()==(0,)
    finally:
        connection.close()
