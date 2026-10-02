"""Actual PostgreSQL output reservation, replay and fencing contracts."""
from uuid import uuid4
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from test_task_leases import queued_task,claim,commit,manifest


def setup(db):
    x=queued_task(db)
    db.execute("UPDATE photo.worker_registrations SET certificate_sha256=%s WHERE worker_id=%s",('f'*64,x['worker']))
    db.execute("UPDATE photo.tasks SET execution_profile=execution_profile || %s WHERE id=%s",(Jsonb({'resource_estimate':{'output_bytes':100}}),x['task']))
    db.execute("INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES('asset.verify','1','verified','[\"application/json\"]',100,3) ON CONFLICT(operation,version,role) DO UPDATE SET max_bytes=100,max_count=3")
    assert claim(db,x)[0]=='claimed'
    return x


def begin(db,x,key,**changes):
    return db.execute('SELECT * FROM photo.begin_output(%s,1,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
        (x['task'],changes.get('fence',1),x['worker'],'f'*64,key,changes.get('role','verified'),changes.get('size',2),changes.get('sha','a'*64),'application/json','test')).fetchone()


def complete(db,x,upload,**changes):
    return db.execute('SELECT * FROM photo.complete_output(%s,1,%s,%s,%s,%s,%s,%s,%s,%s)',
        (x['task'],changes.get('fence',1),x['worker'],'f'*64,upload,changes.get('version','v1'),changes.get('size',2),'a'*64,'application/json')).fetchone()


def test_output_reservation_commit_and_replay(db):
    x=setup(db);key=uuid4()
    db.execute('SET LOCAL ROLE photo_gateway')
    first=begin(db,x,key)
    assert first[0]=='created'
    assert begin(db,x,key)[0]=='busy'
    assert begin(db,x,key,size=3)[0]=='conflict'
    with pytest.raises(errors.InsufficientPrivilege),db.transaction():db.execute('SELECT * FROM photo.output_uploads')
    db.execute('RESET ROLE')
    assert db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(x['tenant'],)).fetchone()==(2,0)
    db.execute('SET LOCAL ROLE photo_gateway')
    assert complete(db,x,first[1],size=3)[0]=='conflict'
    saved=complete(db,x,first[1]);assert saved==('committed',first[2],'a'*64)
    assert complete(db,x,first[1])==('replayed',first[2],'a'*64)
    assert complete(db,x,first[1],version='different')[0]=='conflict'
    assert begin(db,x,key)[0]=='replayed'
    db.execute('RESET ROLE')
    assert db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(x['tenant'],)).fetchone()==(0,2)
    artifact=db.execute('SELECT created_by_task_id,produced_attempt,produced_fence,verified_at,output_role FROM photo.artifacts WHERE id=%s',(first[2],)).fetchone()
    assert artifact[:3]==(x['task'],1,1) and artifact[3] is not None and artifact[4]=='verified'
    db.execute('SET LOCAL ROLE photo_worker')
    assert commit(db,x,1,manifest([dict(artifact_id=str(first[2]),sha256='a'*64)]))[0]=='committed'


def test_output_policy_quota_and_revoked_attempt(db):
    x=setup(db)
    db.execute('SET LOCAL ROLE photo_gateway')
    assert begin(db,x,uuid4(),role='other')[0]=='forbidden'
    first=begin(db,x,uuid4(),size=70);assert first[0]=='created'
    assert begin(db,x,uuid4(),size=40)[0]=='quota'
    assert begin(db,x,uuid4(),fence=2)[0]=='stale'
    db.execute('RESET ROLE')
    db.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(x['run'],))
    db.execute('SET LOCAL ROLE photo_gateway')
    assert complete(db,x,first[1],size=70)[0]=='stale'
    db.execute('RESET ROLE')
    assert db.execute('SELECT count(*) FROM photo.artifacts WHERE id=%s',(first[2],)).fetchone()[0]==0
    # Retain quota until exact-key orphan reconciliation; no speculative release.
    assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(x['tenant'],)).fetchone()[0]==70


def test_tenant_storage_quota_checked_before_ticket(db):
    x=setup(db)
    db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','1') WHERE id=%s",(x['tenant'],))
    db.execute('SET LOCAL ROLE photo_gateway')
    assert begin(db,x,uuid4())[0]=='quota'
    db.execute('RESET ROLE')
    assert db.execute('SELECT count(*) FROM photo.output_uploads WHERE task_id=%s',(x['task'],)).fetchone()[0]==0


def test_cleanup_never_selects_verified_and_releases_reserve_once(db):
    x=setup(db)
    db.execute('SET LOCAL ROLE photo_gateway')
    pending=begin(db,x,uuid4())
    ready=begin(db,x,uuid4())
    assert complete(db,x,ready[1])[0]=='committed'
    db.execute('RESET ROLE')
    db.execute("UPDATE photo.output_uploads SET cleanup_after=clock_timestamp()-interval '1 second' WHERE task_id=%s",(x['task'],))
    db.execute('SET LOCAL ROLE photo_maintenance')
    # Still running with a live lease: even forcibly due pending rows are kept.
    assert db.execute('SELECT * FROM photo.claim_output_cleanup()').fetchone() is None
    db.execute('RESET ROLE')
    db.execute("UPDATE photo.tasks SET state='cancelled',lease_until=NULL WHERE id=%s",(x['task'],))
    db.execute('SET LOCAL ROLE photo_maintenance')
    ticket=db.execute('SELECT * FROM photo.claim_output_cleanup()').fetchone()
    assert ticket[0]==pending[1]
    assert db.execute('SELECT * FROM photo.claim_output_cleanup()').fetchone() is None
    assert not db.execute('SELECT photo.finish_output_cleanup(%s,%s)',(ticket[0],uuid4())).fetchone()[0]
    assert db.execute('SELECT photo.finish_output_cleanup(%s,%s)',ticket[:2]).fetchone()[0]
    assert not db.execute('SELECT photo.finish_output_cleanup(%s,NULL)',(ticket[0],)).fetchone()[0]
    db.execute('RESET ROLE')
    assert db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(x['tenant'],)).fetchone()==(0,2)
    db.execute("UPDATE photo.output_uploads SET cleanup_after=clock_timestamp()-interval '1 second' WHERE id=%s",(pending[1],))
    db.execute('SET LOCAL ROLE photo_maintenance')
    again=db.execute('SELECT * FROM photo.claim_output_cleanup()').fetchone()
    assert again[0]==ticket[0] and again[1]!=ticket[1]
    assert db.execute('SELECT photo.finish_output_cleanup(%s,%s)',again[:2]).fetchone()[0]
    db.execute('RESET ROLE')
    assert db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(x['tenant'],)).fetchone()==(0,2)
    assert db.execute('SELECT count(*) FROM photo.artifacts WHERE id=%s',(ready[2],)).fetchone()[0]==1
