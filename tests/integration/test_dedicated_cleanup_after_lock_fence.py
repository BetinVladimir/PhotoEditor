"""Authored actual finish SQL after-lock races over persisted cleaning metadata.
No reusable claim/adoption fixture exists in this suite. This targeted fixture models
the persisted post-claim boundary; it does not qualify claim, adoption or physical IO.
No scenario was executed while authoring.
"""
from concurrent.futures import ThreadPoolExecutor
from time import monotonic,sleep
from uuid import uuid4
import psycopg
from psycopg import sql
import pytest
from test_database import tenant


def cleaning_receipt(db,kind):
    tid,actor=tenant(db,str(uuid4())+'@cleanup.test')
    db.execute('INSERT INTO photo.tenant_usage(tenant_id,storage_bytes) VALUES(%s,2)',(tid,))
    source=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at,output_role) VALUES(%s,'test',%s,'source-v1',%s,2,'image/jpeg',clock_timestamp(),'staging_upload') RETURNING id",(tid,'staging/tenants/'+str(tid)+'/'+str(uuid4()),'a'*64)).fetchone()[0]
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES(%s,'input','user',%s,%s,1) RETURNING id",(tid,actor,'b'*64)).fetchone()[0]
    run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed,cancel_requested) VALUES(%s,'ingest','cancelled',%s,'{}',%s,%s,true,true) RETURNING id",(tid,snapshot,'c'*64,actor)).fetchone()[0]
    token=uuid4()
    if kind=='original':
        artifact=uuid4();key='originals/tenants/'+str(tid)+'/'+str(artifact)
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role) VALUES(%s,%s,'test',%s,'cleanup-v1',%s,2,'image/jpeg','deleting',clock_timestamp(),'original')",(artifact,tid,key,'a'*64))
        row=db.execute("INSERT INTO photo.ingest_originals(tenant_id,run_id,ordinal,source_id,source_sha,artifact_id,token,bytes,bucket,media_type,state,object_version,completed_at,cleanup_state,cleanup_token,cleanup_lease_until) VALUES(%s,%s,0,%s,%s,%s,%s,2,'test','image/jpeg','ready','cleanup-v1',clock_timestamp(),'cleaning',%s,clock_timestamp()+interval '2 seconds') RETURNING artifact_id,cleanup_lease_until",(tid,run,source,'a'*64,artifact,uuid4(),token)).fetchone()
    else:
        artifact=source
        db.execute("UPDATE photo.artifacts SET state='deleting' WHERE id=%s",(artifact,))
        row=db.execute("INSERT INTO photo.staging_cleanup(tenant_id,run_id,ordinal,artifact_id,staging_bytes,state,next_at,token,lease_until,bucket,object_key,object_version,sha256,media_type) SELECT %s,%s,0,id,2,'cleaning',clock_timestamp(),%s,clock_timestamp()+interval '2 seconds',bucket,object_key,object_version,sha256,media_type FROM photo.artifacts WHERE id=%s RETURNING id,lease_until",(tid,run,token,artifact)).fetchone()
    return tid,artifact,row[0],token,row[1]


@pytest.mark.parametrize('kind',['original','staging'])
def test_dedicated_finish_rechecks_cleanup_lease_after_artifact_wait(database,kind):
    function='finish_original_cleanup' if kind=='original' else 'finish_staging_cleanup'
    with psycopg.connect(**database) as seed:
        tid,artifact,receipt,token,lease=cleaning_receipt(seed,kind)
    with psycopg.connect(**database) as blocker,psycopg.connect(**database,autocommit=True) as observer,psycopg.connect(**database) as maintenance:
        blocker.execute('SELECT id FROM photo.artifacts WHERE id=%s FOR UPDATE',(artifact,))
        assert observer.execute('SELECT clock_timestamp()<%s',(lease,)).fetchone()==(True,)
        pid=maintenance.info.backend_pid
        def finish():
            maintenance.execute('SET LOCAL ROLE photo_maintenance')
            maintenance.execute("SET LOCAL statement_timeout='8s'")
            return maintenance.execute(sql.SQL('SELECT photo.{}(%s,%s)').format(sql.Identifier(function)),(receipt,token)).fetchone()
        with ThreadPoolExecutor(max_workers=1) as pool:
            future=pool.submit(finish);deadline=monotonic()+5
            try:
                while True:
                    waiting=observer.execute("SELECT wait_event_type='Lock' FROM pg_stat_activity WHERE pid=%s",(pid,)).fetchone()
                    if waiting and waiting[0]:break
                    assert not future.done(),'finish did not reach artifact wait'
                    assert monotonic()<deadline,'artifact lock wait not observed'
                    sleep(.01)
                observer.execute('SELECT pg_sleep(greatest(0,extract(epoch FROM %s::timestamptz-clock_timestamp()))+.05)',(lease,))
            finally:blocker.rollback()
            assert future.result(timeout=5)==(False,)
    with psycopg.connect(**database) as inspect:
        assert inspect.execute('SELECT storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()==(2,)
        assert inspect.execute('SELECT state,gc_storage_settled FROM photo.artifacts WHERE id=%s',(artifact,)).fetchone()==('deleting',False)
        if kind=='original':
            inspect.execute("UPDATE photo.ingest_originals SET cleanup_lease_until=clock_timestamp()+interval '60 seconds' WHERE artifact_id=%s",(receipt,))
        else:
            inspect.execute("UPDATE photo.staging_cleanup SET lease_until=clock_timestamp()+interval '60 seconds' WHERE id=%s",(receipt,))
        inspect.execute('SET LOCAL ROLE photo_maintenance')
        assert inspect.execute(sql.SQL('SELECT photo.{}(%s,%s)').format(sql.Identifier(function)),(receipt,token)).fetchone()==(True,)
        assert inspect.execute(sql.SQL('SELECT photo.{}(%s,%s)').format(sql.Identifier(function)),(receipt,token)).fetchone()==(True,)
        inspect.execute('RESET ROLE')
        assert inspect.execute('SELECT storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()==(0,)
        assert inspect.execute('SELECT state,gc_storage_settled FROM photo.artifacts WHERE id=%s',(artifact,)).fetchone()==('deleted',True)
