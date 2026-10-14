"""Real CPU assembly SQL authority; metadata fixtures, no object quality claim."""
from uuid import uuid4
from psycopg import errors
import pytest
from test_v27_semantic_final_producer_gate import fixture


def test_cpu_assembly_live_authorities_and_revocation(db):
    tid,task,_,producer,_,candidate,scope=fixture(db,snapshot_authority=True,qualification=True)
    run,params,digest=db.execute('SELECT run_id,params_artifact_id,input_digest FROM photo.tasks WHERE id=%s',(task,)).fetchone()
    worker,node=uuid4(),'cpu-assembly-'+str(uuid4())
    db.execute("INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes) VALUES(%s,4000000000,0,2000000000)",(node,))
    db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions) VALUES(%s,%s,'[\"cpu\"]','[\"cpu\"]',%s,now(),'{}',session_user,%s,true,'{\"v2.semantic.assemble\":[\"cpu\"]}','[\"v2.2\"]')",(tid,worker,'a'*64,node))
    db.execute("UPDATE photo.tasks SET state='running',canonical_result_id=NULL,worker_id=%s,lease_until=clock_timestamp()+interval '60 seconds',deadline=clock_timestamp()+interval '120 seconds' WHERE id=%s",(worker,task))
    db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity,tenant_id,task_id,fencing_token,lease_until) VALUES(%s,'cpu1','cpu','{}',%s,%s,1,clock_timestamp()+interval '60 seconds')",(node,tid,task))
    args=(tid,run,task,1,1,worker,params,'f'*64,digest)
    def read(name,extra=(),values=args,rows=False):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_worker')
            try:
                result=db.execute('SELECT * FROM photo.'+name+'('+','.join(['%s']*(len(values)+len(extra)))+')',(*values,*extra))
                return result.fetchall() if rows else result.fetchone()[0]
            finally:db.execute('RESET ROLE')
    qualify='v27_semantic_assembly_worker_qualification'
    snapshot='v27_semantic_assembly_worker_snapshot'
    provenance='v27_semantic_assembly_worker_producer'
    report='v27_semantic_assembly_worker_report_evidence'
    assert read(qualify)==scope['qualification']
    assert read(report)['pin']==scope['qualification']
    source=read(snapshot,rows=True)
    assert len(source)==1 and source[0][0]==scope['root'] and source[0][4]==0
    origin=read(provenance,(candidate,'9'*64),rows=True)
    assert len(origin)==1 and origin[0][0]==producer
    assert read(provenance,(candidate,'0'*64),rows=True)==[]
    for index,value in [(0,uuid4()),(1,uuid4()),(2,uuid4()),(3,2),(4,2),(5,uuid4()),(6,uuid4()),(7,'0'*64),(8,'0'*64)]:
        changed=list(args);changed[index]=value
        assert read(qualify,values=tuple(changed)) is None
    for sql,values in [
        ('UPDATE photo.worker_registrations SET enabled=false WHERE worker_id=%s',(worker,)),
        ("UPDATE photo.worker_registrations SET db_principal='other_login' WHERE worker_id=%s",(worker,)),
        ("UPDATE photo.tasks SET lease_until=clock_timestamp()-interval '1 second' WHERE id=%s",(task,)),
        ('UPDATE photo.resource_slots SET fencing_token=2 WHERE task_id=%s',(task,)),
        ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(scope['qualification']['policy_id'],)),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(sql,values)
            assert read(qualify) is None and read(report) is None
            assert read(snapshot,rows=True)==[] and read(provenance,(candidate,'9'*64),rows=True)==[]
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(producer,))
        assert read(provenance,(candidate,'9'*64),rows=True)==[]
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],))
        assert read(snapshot,rows=True)==[]
    with pytest.raises(errors.InsufficientPrivilege):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_api')
            db.execute('SELECT photo.'+qualify+'('+','.join(['%s']*9)+')',args)
