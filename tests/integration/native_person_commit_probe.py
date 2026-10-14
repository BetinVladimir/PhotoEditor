"""Worker SQL commit boundary using synthetic lease/output metadata.

No dispatcher claim, object bytes, model inference or scientific qualification is
claimed. Only commit_task_success may create the canonical person result here.
"""
from hashlib import sha256
from uuid import uuid4
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes


def verify_person_worker_commit(db,tenant,run,task,worker,outputs,guard_error):
    node='native-person-result-'+str(uuid4())
    db.execute("INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes) VALUES(%s,2147483648,1073741824,1073741824)",(node,))
    db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions) VALUES(%s,%s,'[\"gpu\"]','[\"cuda\"]',%s,clock_timestamp(),'{}',session_user,%s,true,'{\"mask.person\":[\"cuda\"]}','[\"v2.2\"]')",(tenant,worker,'e'*64,node))
    db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity,tenant_id,task_id,fencing_token,lease_until) VALUES(%s,'gpu1','gpu','{}',%s,%s,1,clock_timestamp()+interval '60 seconds')",(node,tenant,task))
    db.execute("INSERT INTO photo.resource_reservations(tenant_id,task_id,node_id,scratch_bytes,ram_bytes,vram_bytes,fencing_token,expires_at) VALUES(%s,%s,%s,1024,1024,1024,1,clock_timestamp()+interval '60 seconds')",(tenant,task,node))
    db.execute("UPDATE photo.tasks SET state='running',worker_id=%s,lease_until=clock_timestamp()+interval '60 seconds',deadline=clock_timestamp()+interval '600 seconds' WHERE tenant_id=%s AND id=%s",(worker,tenant,task))
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,backend,profile) SELECT tenant_id,id,attempt,fencing_token,%s,clock_timestamp(),'cuda',execution_profile FROM photo.tasks WHERE tenant_id=%s AND id=%s",(worker,tenant,task))
    manifest=dict(outputs=outputs,metrics=dict(duration_ms=12,peak_memory_bytes=128),review_required=False)
    body=canonical_bytes(manifest).decode()
    def commit(payload=body,fence=1):
        return db.execute('SELECT * FROM photo.commit_task_success(%s,1,%s,%s,%s)',(task,fence,worker,payload)).fetchone()
    db.execute('SET LOCAL ROLE photo_worker')
    try:
        with pytest.raises(errors.InsufficientPrivilege),db.transaction():
            db.execute('SELECT * FROM photo.task_results')
        for i in range(4):
            invalid=dict(manifest,outputs=outputs[:i]+outputs[i+1:])
            with pytest.raises(errors.CheckViolation,match=guard_error),db.transaction():
                commit(canonical_bytes(invalid).decode())
        assert commit(fence=2)==('stale',None,None)
        saved=commit()
        assert saved[0]=='committed' and saved[1] is not None and saved[2]==sha256(body.encode()).hexdigest()
        assert commit()==('replayed',saved[1],saved[2])
        changed=dict(manifest,metrics=dict(duration_ms=13,peak_memory_bytes=128))
        assert commit(canonical_bytes(changed).decode())==('conflict',None,None)
    finally:
        db.execute('RESET ROLE')
    assert db.execute('SELECT state,canonical_result_id,lease_until FROM photo.tasks WHERE tenant_id=%s AND id=%s',(tenant,task)).fetchone()==('succeeded',saved[1],None)
    assert db.execute('SELECT count(*) FROM photo.task_results WHERE tenant_id=%s AND task_id=%s',(tenant,task)).fetchone()==(1,)
    assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND routing_key='task.succeeded' AND payload->>'task_id'=%s",(tenant,str(task))).fetchone()==(1,)
    assert db.execute('SELECT state FROM photo.resource_reservations WHERE tenant_id=%s AND task_id=%s',(tenant,task)).fetchone()==('released',)
    assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(task,)).fetchone()==(0,)
    assert db.execute('SELECT finished_at IS NOT NULL FROM photo.task_attempts WHERE tenant_id=%s AND task_id=%s AND attempt=1',(tenant,task)).fetchone()==(True,)
