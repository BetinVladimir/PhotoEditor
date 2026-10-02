"""Actual SQL reservation/commit accounting above uint32; no object bytes here."""
from uuid import uuid4
from psycopg.types.json import Jsonb
from test_gateway_outputs import setup,begin,complete


def test_large_output_quota_commit_replay_and_fence(db):
    item=setup(db)
    size=2**32+1024*1024+17
    db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}',%s::jsonb) WHERE id=%s",(str(size+1024),item['tenant']))
    db.execute('UPDATE photo.tasks SET execution_profile=execution_profile||%s WHERE id=%s',
        (Jsonb({'resource_estimate':{'output_bytes':size+1}}),item['task']))
    db.execute("UPDATE photo.operation_output_policies SET max_bytes=%s WHERE operation='asset.verify' AND version='1' AND role='verified'",(size+1,))
    key=uuid4()
    db.execute('SET LOCAL ROLE photo_gateway')
    ticket=begin(db,item,key,size=size)
    assert ticket[0]=='created'
    assert begin(db,item,key,size=size)[0]=='busy'
    assert begin(db,item,uuid4(),size=2)[0]=='quota'
    assert complete(db,item,ticket[1],size=size,fence=2)[0]=='stale'
    assert complete(db,item,ticket[1],size=size-1)[0]=='conflict'
    db.execute('RESET ROLE')
    assert db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(item['tenant'],)).fetchone()==(size,0)
    db.execute('SET LOCAL ROLE photo_gateway')
    assert complete(db,item,ticket[1],size=size)==('committed',ticket[2],'a'*64)
    assert complete(db,item,ticket[1],size=size)==('replayed',ticket[2],'a'*64)
    assert begin(db,item,key,size=size)[0]=='replayed'
    assert begin(db,item,uuid4(),size=2)[0]=='quota'
    db.execute('RESET ROLE')
    assert db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(item['tenant'],)).fetchone()==(0,size)
    assert db.execute('SELECT bytes,produced_attempt,produced_fence,object_version FROM photo.artifacts WHERE id=%s',(ticket[2],)).fetchone()==(size,1,1,'v1')
    assert db.execute('SELECT count(*) FROM photo.output_uploads WHERE task_id=%s',(item['task'],)).fetchone()[0]==1
