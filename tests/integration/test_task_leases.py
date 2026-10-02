"""Exercise real PostgreSQL locks/fences; no substitute in-memory database."""
import json
from uuid import uuid4
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from test_database import tenant
from photo_worker.contracts.base import canonical_bytes


def queued_task(db):
    tid, user = tenant(db, str(uuid4())+'@example.test')
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)', (tid,))
    params = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) VALUES(%s,'test',%s,%s,2,'application/json') RETURNING id", (tid,str(uuid4()),'a'*64)).fetchone()[0]
    snapshot = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES(%s,'input','user',%s,%s,1) RETURNING id",(tid,user,'b'*64)).fetchone()[0]
    run = db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'apply','running',%s,'{}',%s,%s,true) RETURNING id",(tid,snapshot,'c'*64,user)).fetchone()[0]
    worker, task = uuid4(), uuid4()
    node = 'test-'+str(uuid4())
    db.execute("INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes) VALUES(%s,10000000,0,10000000)",(node,))
    command = dict(schema_version=1,event_id=str(uuid4()),tenant_id=str(tid),run_id=str(run),task_id=str(task),attempt=1,
                   operation='asset.verify',operation_version='1',resource_class='media',correlation_id=str(uuid4()),
                   inputs=[],params=dict(artifact_id=str(params),sha256='a'*64),model=None,timeout_seconds=600,traceparent='')
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,dispatch_command) VALUES(%s,%s,%s,'verify','asset.verify','1','media','queued','[]',%s,%s,%s,%s)",
               (task,tid,run,params,'d'*64,Jsonb(dict(backend='cpu',precision='fp32')),Jsonb(command)))
    db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations) VALUES(%s,%s,'[\"media\"]','[\"cpu\"]',%s,now(),'{}',session_user,%s,true,'{\"asset.verify\":[\"cpu\"]}')",(tid,worker,'e'*64,node))
    db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity,tenant_id,task_id,fencing_token,lease_until) VALUES(%s,'media1','media','{}',%s,%s,0,now()+interval '60 seconds')",(node,tid,task))
    db.execute("INSERT INTO photo.resource_reservations(tenant_id,task_id,node_id,scratch_bytes,ram_bytes,vram_bytes,expires_at) VALUES(%s,%s,%s,1024,1024,0,now()+interval '60 seconds')",(tid,task,node))
    db.execute('SET CONSTRAINTS ALL IMMEDIATE')
    db.execute('SET CONSTRAINTS ALL DEFERRED')
    return dict(tenant=tid,run=run,worker=worker,task=task,command=command,node=node)


def claim(db, x, **override):
    return db.execute('SELECT * FROM photo.claim_task(%s,%s,%s,%s)',(x['task'],override.get('attempt',1),override.get('worker',x['worker']),Jsonb(override.get('command',x['command'])))).fetchone()


def heartbeat(db, x, fence):
    return db.execute('SELECT * FROM photo.heartbeat_task(%s,1,%s,%s,%s)',(x['task'],fence,x['worker'],Jsonb(dict(done=1,total=2)))).fetchone()


def manifest(outputs=None):
    return canonical_bytes(dict(outputs=outputs or [],metrics=dict(duration_ms=12,peak_memory_bytes=100),review_required=False)).decode()


def commit(db,x,fence,body=None):
    return db.execute('SELECT * FROM photo.commit_task_success(%s,1,%s,%s,%s)',(x['task'],fence,x['worker'],body or manifest())).fetchone()


def test_claim_identity_command_and_duplicate(db):
    x=queued_task(db)
    db.execute('SET LOCAL ROLE photo_worker')
    assert claim(db,x,worker=uuid4())[0]=='unauthorized'
    assert claim(db,x,command={**x['command'],'operation':'image.color'})[0]=='stale'
    assert claim(db,x,attempt=2)[0]=='stale'
    got=claim(db,x)
    assert got[0]=='claimed' and got[1]==1 and got[2]<=got[3]
    assert got[4]['command']==x['command']
    assert claim(db,x)[0]=='busy'
    assert heartbeat(db,x,999)[0] is False
    assert heartbeat(db,x,1)[0] is True
    with pytest.raises(errors.InsufficientPrivilege),db.transaction():
        db.execute('SELECT * FROM photo.tasks')
    with pytest.raises(errors.InsufficientPrivilege),db.transaction():
        db.execute('SELECT photo.release_task_resources(%s,%s,1)',(x['tenant'],x['task']))
    db.execute('RESET ROLE')
    assert db.execute('SELECT count(*) FROM photo.task_attempts WHERE task_id=%s',(x['task'],)).fetchone()[0]==1


def test_success_replay_conflict_single_event_and_resource_release(db):
    x=queued_task(db)
    db.execute('SET LOCAL ROLE photo_worker')
    assert claim(db,x)[0]=='claimed'
    saved=commit(db,x,1)
    assert saved[0]=='committed'
    assert commit(db,x,1)==('replayed',saved[1],saved[2])
    changed=json.loads(manifest());changed['metrics']['duration_ms']=13
    assert commit(db,x,1,canonical_bytes(changed).decode())[0]=='conflict'
    assert claim(db,x)[0]=='terminal'
    assert heartbeat(db,x,1)[0] is False
    db.execute('RESET ROLE')
    assert db.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s',(x['tenant'],)).fetchone()[0]==1
    assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(x['task'],)).fetchone()[0]==0
    assert db.execute('SELECT state FROM photo.resource_reservations WHERE task_id=%s',(x['task'],)).fetchone()[0]=='released'


def test_expired_lease_never_resurrected(db):
    x=queued_task(db)
    assert claim(db,x)[0]=='claimed'
    db.execute("UPDATE photo.tasks SET lease_until=clock_timestamp()-interval '1 second' WHERE id=%s",(x['task'],))
    db.execute('SET LOCAL ROLE photo_worker')
    assert heartbeat(db,x,1)[0] is False
    assert commit(db,x,1)[0]=='stale'


def test_cancel_between_claim_and_commit(db):
    x=queued_task(db)
    assert claim(db,x)[0]=='claimed'
    db.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(x['run'],))
    db.execute('SET LOCAL ROLE photo_worker')
    assert heartbeat(db,x,1)[2] is True
    assert commit(db,x,1)[0]=='cancelled'
    error=dict(code='TASK_CANCELLED',retryable=False,message_key='errors.cancelled',args={})
    assert db.execute('SELECT photo.commit_task_failure(%s,1,1,%s,%s,%s)',(x['task'],x['worker'],Jsonb(error),Jsonb({}))).fetchone()[0]=='committed'
    assert db.execute('SELECT photo.commit_task_failure(%s,1,1,%s,%s,%s)',(x['task'],x['worker'],Jsonb(error),Jsonb({}))).fetchone()[0]=='replayed'
    db.execute('RESET ROLE')
    assert db.execute('SELECT state FROM photo.tasks WHERE id=%s',(x['task'],)).fetchone()[0]=='cancelled'


def test_output_requires_gateway_verified_matching_attempt(db):
    x=queued_task(db)
    assert claim(db,x)[0]=='claimed'
    aid=uuid4()
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'test',%s,%s,20,'application/json',%s,1,1)",(aid,x['tenant'],str(aid),'f'*64,x['task']))
    body=manifest([dict(artifact_id=str(aid),sha256='f'*64)])
    db.execute('SET LOCAL ROLE photo_worker')
    with pytest.raises(errors.InvalidParameterValue,match='OUTPUT_NOT_VERIFIED'),db.transaction():commit(db,x,1,body)
    db.execute('RESET ROLE')
    db.execute('UPDATE photo.artifacts SET verified_at=clock_timestamp() WHERE id=%s',(aid,))
    db.execute('SET LOCAL ROLE photo_worker')
    assert commit(db,x,1,body)[0]=='committed'


def test_hard_deadline_caps_heartbeat(db):
    x=queued_task(db)
    db.execute('UPDATE photo.tasks SET timeout_seconds=1 WHERE id=%s',(x['task'],))
    db.execute('SET LOCAL ROLE photo_worker')
    got=claim(db,x)
    beat=heartbeat(db,x,1)
    assert beat[0] and beat[1]==got[3]


def test_revoked_worker_cannot_commit(db):
    x=queued_task(db)
    assert claim(db,x)[0]=='claimed'
    db.execute('UPDATE photo.worker_registrations SET enabled=false WHERE worker_id=%s',(x['worker'],))
    db.execute('SET LOCAL ROLE photo_worker')
    assert heartbeat(db,x,1)[0] is False
    assert commit(db,x,1)[0]=='stale'


def test_reaper_invalidates_fence_and_schedules_once(db):
    x=queued_task(db)
    assert claim(db,x)[0]=='claimed'
    db.execute("UPDATE photo.tasks SET lease_until=clock_timestamp()-interval '1 second' WHERE id=%s",(x['task'],))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.reap_tasks(10)').fetchone()[0]==1
    assert db.execute('SELECT photo.reap_tasks(10)').fetchone()[0]==0
    pending=db.execute('SELECT * FROM photo.pending_task_failures(10)').fetchone()
    assert pending[:5]==(x['task'],1,2,0,0)
    assert pending[6]['code']=='LEASE_LOST'
    assert db.execute("SELECT photo.plan_task_retry(%s,1,2,'retry_wait',1,0,clock_timestamp(),%s,'LEASE_LOST')",(x['task'],Jsonb(pending[7]))).fetchone()[0] is True
    assert db.execute("SELECT photo.plan_task_retry(%s,1,2,'retry_wait',1,0,clock_timestamp(),%s,'LEASE_LOST')",(x['task'],Jsonb(pending[7]))).fetchone()[0] is False
    assert db.execute('SELECT photo.activate_task_retries(10)').fetchone()[0]==1
    db.execute('SET LOCAL ROLE photo_worker')
    assert commit(db,x,1)[0]=='stale'
    assert heartbeat(db,x,1)[0] is False
    assert claim(db,x)[0]=='stale'
    db.execute('RESET ROLE')
    state=db.execute('SELECT state,attempt,retry_count,fencing_token FROM photo.tasks WHERE id=%s',(x['task'],)).fetchone()
    assert state==('ready',1,1,2)


def test_expired_dispatch_does_not_consume_attempt(db):
    x=queued_task(db)
    db.execute("UPDATE photo.resource_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE task_id=%s",(x['task'],))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.reap_tasks(10)').fetchone()[0]==1
    db.execute('RESET ROLE')
    assert db.execute('SELECT state,attempt,fencing_token FROM photo.tasks WHERE id=%s',(x['task'],)).fetchone()==('ready',0,1)
    assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(x['task'],)).fetchone()[0]==0


def test_parallel_claim_only_one_execution(database):
    import psycopg
    from concurrent.futures import ThreadPoolExecutor
    from psycopg.conninfo import make_conninfo
    from photo_worker.contracts.wire import TaskCommand
    from photo_worker.runtime.database import TaskDatabase
    with psycopg.connect(**database) as conn:
        x=queued_task(conn)
    task_db=TaskDatabase(make_conninfo(**database))
    command=TaskCommand.model_validate_json(json.dumps(x['command']))
    with ThreadPoolExecutor(max_workers=2) as pool:
        outcomes=list(pool.map(lambda _: task_db.claim(command,x['worker']).outcome,range(2)))
    assert sorted(outcomes)==['busy','claimed']


def test_executor_heartbeat_and_redelivery(database):
    import psycopg
    import time
    from psycopg.conninfo import make_conninfo
    from photo_worker.runtime.database import TaskDatabase
    from photo_worker.runtime.executor import Executor,Operation,OperationResult
    with psycopg.connect(**database) as conn:
        x=queued_task(conn)
    calls=[]
    def operation(ctx):
        calls.append(ctx.lease.command.task_id)
        ctx.progress(1,2)
        time.sleep(.15)
        ctx.check_active()
        ctx.progress(2,2)
        return OperationResult([])
    executor=Executor(TaskDatabase(make_conninfo(**database)),x['worker'],[Operation('asset.verify','media',operation)],heartbeat_seconds=.02)
    raw=json.dumps(x['command']).encode()
    assert executor.process(raw)=='committed'
    assert executor.process(raw)=='terminal'
    assert calls==[x['task']]
    with psycopg.connect(**database) as conn:
        assert conn.execute('SELECT progress FROM photo.tasks WHERE id=%s',(x['task'],)).fetchone()[0]['done']>=1
        assert conn.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s',(x['tenant'],)).fetchone()[0]==1


def test_rabbit_worker_ack_and_poison_dead_letter(database,rabbit_vhost):
    import psycopg
    import pika
    from threading import Event
    from psycopg.conninfo import make_conninfo
    from photo_worker.runtime.database import TaskDatabase
    from photo_worker.runtime.executor import Executor,Operation,OperationResult
    from photo_worker.runtime.transport import Transport
    with psycopg.connect(**database) as conn:
        x=queued_task(conn)
    queue_name='q.worker.'+str(x['worker'])+'.media'
    routing='worker.'+str(x['worker'])+'.io.verify'
    calls=[]
    def operation(ctx):
        calls.append(ctx.lease.command.task_id)
        return OperationResult([])
    executor=Executor(TaskDatabase(make_conninfo(**database)),x['worker'],[Operation('asset.verify','media',operation)])
    connection=pika.BlockingConnection(pika.URLParameters(rabbit_vhost))
    ch=connection.channel()
    # Test bootstrap owns topology, matching the production Go registry.
    ch.exchange_declare('photo.commands.v1',exchange_type='topic',durable=True)
    ch.exchange_declare('photo.dead.v1',exchange_type='topic',durable=True)
    ch.queue_declare(queue_name,durable=True,arguments={'x-queue-type':'quorum','x-delivery-limit':20,'x-consumer-timeout':14400000,'x-dead-letter-exchange':'photo.dead.v1','x-dead-letter-routing-key':'dead.media'})
    ch.queue_bind(queue_name,'photo.commands.v1',routing)
    ch.queue_declare('q.dead.media',durable=True,arguments={'x-queue-type':'quorum'})
    ch.queue_bind('q.dead.media','photo.dead.v1','dead.media')
    ch.confirm_delivery()
    props=pika.BasicProperties(content_type='application/json',delivery_mode=2)
    raw=json.dumps(x['command']).encode()
    for data in [raw,raw,b'{"invalid":true}']:
        ch.basic_publish('photo.commands.v1',routing,data,props,mandatory=True)
    worker=Transport(rabbit_vhost,'media',executor,development=True)
    assert worker.consume(Event(),max_messages=3)==3
    assert calls==[x['task']]
    assert ch.basic_get(queue_name)[0] is None
    import time
    until=time.monotonic()+5
    while True:
        method,_,body=ch.basic_get('q.dead.media',auto_ack=True)
        if method is not None:break
        assert time.monotonic()<until
        time.sleep(.05)
    assert body==b'{"invalid":true}'
    connection.close()


@pytest.mark.parametrize('kind,code,retryable',[
    ('network','DEPENDENCY_UNAVAILABLE',True),
    ('hash','ARTIFACT_HASH_MISMATCH',False),
    ('scratch','DISK_QUOTA',False),
])
def test_executor_artifact_failure_policy(database,kind,code,retryable):
    import psycopg
    from psycopg.conninfo import make_conninfo
    from photo_worker.runtime.database import TaskDatabase
    from photo_worker.runtime.executor import Executor,Operation
    from photo_worker.runtime.gateway import GatewayError
    from photo_worker.runtime.materialize import ArtifactTransferError
    from photo_worker.runtime.scratch import DiskQuota
    with psycopg.connect(**database) as conn:
        x=queued_task(conn)
    def operation(ctx):
        if kind=='network':raise GatewayError('DEPENDENCY_UNAVAILABLE',True)
        if kind=='hash':raise ArtifactTransferError('ARTIFACT_HASH_MISMATCH')
        raise DiskQuota('local diagnostic must not be persisted')
    executor=Executor(TaskDatabase(make_conninfo(**database)),x['worker'],[Operation('asset.verify','media',operation)])
    assert executor.process(json.dumps(x['command']).encode())=='committed'
    with psycopg.connect(**database) as conn:
        error=conn.execute('SELECT error FROM photo.task_attempts WHERE task_id=%s',(x['task'],)).fetchone()[0]
        assert error['code']==code and error['retryable'] is retryable and error['args']=={}
        assert 'local diagnostic' not in json.dumps(error)
        assert conn.execute('SELECT state FROM photo.tasks WHERE id=%s',(x['task'],)).fetchone()[0]=='retry_wait'


def test_gateway_role_checks_live_lease_and_certificate(db):
    from psycopg import errors
    x=queued_task(db)
    assert claim(db,x)[0]=='claimed'
    certificate='c'*64
    db.execute('UPDATE photo.worker_registrations SET certificate_sha256=%s WHERE worker_id=%s',(certificate,x['worker']))
    def lookup(fence=1,cert=certificate):
        return db.execute('SELECT tenant_id,run_id,task_id FROM photo.authorize_gateway_task(%s,1,%s,%s,%s)',(x['task'],fence,x['worker'],cert)).fetchall()
    db.execute('SET LOCAL ROLE photo_gateway')
    assert lookup()==[(x['tenant'],x['run'],x['task'])]
    assert lookup(fence=2)==[]
    assert lookup(cert='d'*64)==[]
    with pytest.raises(errors.InsufficientPrivilege),db.transaction():db.execute('SELECT * FROM photo.artifacts')
    db.execute('RESET ROLE')
    db.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(x['run'],))
    db.execute('SET LOCAL ROLE photo_gateway')
    assert lookup()==[]
    db.execute('RESET ROLE')
    db.execute('UPDATE photo.runs SET cancel_requested=false WHERE id=%s',(x['run'],))
    db.execute('UPDATE photo.worker_registrations SET enabled=false WHERE worker_id=%s',(x['worker'],))
    db.execute('SET LOCAL ROLE photo_gateway')
    assert lookup()==[]
