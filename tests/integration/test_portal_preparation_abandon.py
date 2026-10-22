"""Authored source scenarios; real selection/start/abandon SQL, no mocked gates.
Uses the existing published accepted-item fixture; no physical encode is claimed.
"""
import hashlib
import json
from uuid import uuid4
import pytest
from test_portal_item_download import setup_downloadable_item
from psycopg.types.json import Jsonb


def digest(body):
    return hashlib.sha256(json.dumps(body, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def context(db):
    tenant, share, guest, session, csrf, item, preset, _ = setup_downloadable_item(db)
    # Existing item-intent fixture uses a minimal old preset; current Download
    # admission requires the full encode-compatible presentation contract.
    config={'format':'jpeg','icc_id':'srgb_v1','subsampling':'444','alpha':'matte','metadata_policy':'strip','quality':90}
    preset=db.execute("INSERT INTO photo.export_presets(tenant_id,name,config,config_hash) VALUES(%s,'Abandon JPEG',%s,%s) RETURNING id",(tenant,Jsonb(config),digest(config))).fetchone()[0]
    db.execute('UPDATE photo.share_links SET allowed_presets=%s WHERE tenant_id=%s AND id=%s',(Jsonb([str(preset)]),tenant,share))
    collection_id,link_version=db.execute('SELECT collection_id,policy_version FROM photo.share_links WHERE tenant_id=%s AND id=%s',(tenant,share)).fetchone()
    session,csrf=uuid4().bytes+uuid4().bytes,uuid4().bytes+uuid4().bytes
    guest=db.execute("INSERT INTO photo.share_sessions(tenant_id,share_id,collection_id,session_hash,link_version,expires_at,locale,csrf_hash) VALUES(%s,%s,%s,%s,%s,clock_timestamp()+interval '1 hour','en',%s) RETURNING id",(tenant,share,collection_id,session,link_version,csrf)).fetchone()[0]
    collection, revision = db.execute('SELECT collection_id,c.revision FROM photo.share_links l JOIN photo.collections c ON c.tenant_id=l.tenant_id AND c.id=l.collection_id WHERE l.tenant_id=%s AND l.id=%s', (tenant, share)).fetchone()
    return dict(tenant=tenant,share=share,guest=guest,session=session,csrf=csrf,item=item,preset=preset,collection=collection,revision=revision,selection=str(uuid4()),start=str(uuid4()))


def selection(db,c):
    sha=digest({'collection_revision':str(c['revision']),'scope':'selected','item_ids':[str(c['item'])]})
    row=db.execute("SELECT outcome,snapshot_id FROM photo.create_portal_selection(%s,%s,%s,%s,%s,%s,'selected',%s,NULL,clock_timestamp()+interval '1 hour')",(c['session'],c['csrf'],c['selection'],sha,uuid4(),c['revision'],[c['item']])).fetchone()
    assert row[0]=='created'
    return sha,row[1]


def abandon(db,c,sha,snapshot=None):
    db.execute('SET LOCAL ROLE photo_portal')
    try:return db.execute('SELECT outcome,receipt FROM photo.abandon_portal_download_preparation(%s,%s,%s,%s,%s,%s,%s)',(c['session'],c['csrf'],c['selection'],sha,c['start'],snapshot,c['preset'])).fetchone()
    finally:db.execute('RESET ROLE')


@pytest.mark.parametrize('known_snapshot',[False,True])
def test_abandon_fences_both_late_producers_without_deleting_source(db,known_snapshot):
    c=context(db)
    sha=digest({'collection_revision':str(c['revision']),'scope':'selected','item_ids':[str(c['item'])]})
    snapshot=None
    if known_snapshot:sha,snapshot=selection(db,c)
    out=abandon(db,c,sha,snapshot);assert out[0]=='abandoned' and out[1]['collection_id']==str(c['collection'])
    # A supplied stale CAS is not reevaluated by abandon; original receipt identity wins.
    assert abandon(db,c,sha,snapshot)==out
    late=db.execute("SELECT outcome FROM photo.create_portal_selection(%s,%s,%s,%s,%s,%s,'selected',%s,NULL,clock_timestamp()+interval '1 hour')",(c['session'],c['csrf'],c['selection'],sha,uuid4(),c['revision'],[c['item']])).fetchone()[0]
    assert late==('replayed' if known_snapshot else 'abandoned')
    start_sha=digest({'preset_id':str(c['preset']),'selection_snapshot_id':str(snapshot or uuid4())})
    outcome=db.execute('SELECT outcome FROM photo.start_portal_download(%s,%s,%s,%s,%s,%s,%s,%s)',(c['session'],c['csrf'],c['start'],start_sha,uuid4(),uuid4(),snapshot or uuid4(),c['preset'])).fetchone()[0]
    assert outcome in ('conflict','abandoned')
    assert db.execute('SELECT count(*) FROM photo.collection_items WHERE tenant_id=%s AND id=%s',(c['tenant'],c['item'])).fetchone()[0]==1
    assert db.execute("SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=%s AND principal_id=%s AND route IN ('Portal.Selection','Portal.Download.Start') AND expires_at>='9999-01-01'",(c['tenant'],c['guest'])).fetchone()[0]==2


def test_committed_start_wins_and_is_not_cancelled(db):
    c=context(db);sha,snapshot=selection(db,c);job,run=uuid4(),uuid4()
    start_sha=digest({'preset_id':str(c['preset']),'selection_snapshot_id':str(snapshot)})
    row=db.execute('SELECT outcome,job_id FROM photo.start_portal_download(%s,%s,%s,%s,%s,%s,%s,%s)',(c['session'],c['csrf'],c['start'],start_sha,job,run,snapshot,c['preset'])).fetchone()
    assert row==('created',job)
    before=db.execute('SELECT response_body,expires_at FROM photo.idempotency_records WHERE tenant_id=%s AND principal_id=%s AND route=%s AND key=%s',(c['tenant'],c['guest'],'Portal.Download.Start',c['start'])).fetchone()
    out=abandon(db,c,sha,snapshot)
    assert out==('accepted',{'download_job_id':str(job),'status':'queued'})
    assert db.execute('SELECT response_body,expires_at FROM photo.idempotency_records WHERE tenant_id=%s AND principal_id=%s AND route=%s AND key=%s',(c['tenant'],c['guest'],'Portal.Download.Start',c['start'])).fetchone()==before
    assert db.execute('SELECT cancel_requested FROM photo.runs WHERE tenant_id=%s AND id=%s',(c['tenant'],run)).fetchone()[0] is False


def test_foreign_body_and_revoked_guest_cannot_reinterpret_receipt(db):
    c=context(db);sha,snapshot=selection(db,c)
    assert abandon(db,c,'f'*64,snapshot)[0]=='conflict'
    db.execute('UPDATE photo.share_sessions SET revoked_at=clock_timestamp() WHERE tenant_id=%s AND id=%s',(c['tenant'],c['guest']))
    assert abandon(db,c,sha,snapshot)[0]=='unauthorized'


def test_waiting_abandon_rechecks_guest_after_link_revocation(database):
    import concurrent.futures
    import time
    import psycopg
    # Dedicated isolated database fixture owns all committed rows. Genuine
    # second transaction waits on the same share link used by both producers.
    with psycopg.connect(**database) as seed:
        c=context(seed)
    sha=digest({'collection_revision':str(c['revision']),'scope':'selected','item_ids':[str(c['item'])]})
    name='portal-abandon-'+uuid4().hex
    with psycopg.connect(**database) as blocker, concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
        blocker.execute('SELECT id FROM photo.share_links WHERE tenant_id=%s AND id=%s FOR UPDATE',(c['tenant'],c['share']))
        def waiting():
            with psycopg.connect(**database,application_name=name) as conn:return abandon(conn,c,sha)
        future=pool.submit(waiting)
        with psycopg.connect(**database,autocommit=True) as observer:
            deadline=time.monotonic()+5
            while time.monotonic()<deadline:
                if observer.execute("SELECT EXISTS(SELECT FROM pg_stat_activity WHERE application_name=%s AND wait_event_type='Lock')",(name,)).fetchone()[0]:break
                time.sleep(.01)
            else:raise AssertionError('abandon did not reach real producer lock')
        blocker.execute('UPDATE photo.share_links SET revoked_at=clock_timestamp() WHERE tenant_id=%s AND id=%s',(c['tenant'],c['share']))
        blocker.commit()
        assert future.result(timeout=5)==('unauthorized',None)
    with psycopg.connect(**database) as conn:
        assert conn.execute('SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=%s AND principal_id=%s',(c['tenant'],c['guest'])).fetchone()[0]==0


def test_existing_unsealed_expiry_is_extended_without_reusing_wrong_start_hash(db):
    c=context(db);sha=digest({'collection_revision':str(c['revision']),'scope':'selected','item_ids':[str(c['item'])]})
    db.execute("INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES(%s,'guest',%s,'Portal.Selection',%s,%s,clock_timestamp()+interval '1 second')",(c['tenant'],c['guest'],c['selection'],sha))
    assert abandon(db,c,sha)[0]=='abandoned'
    assert db.execute("SELECT response_status,extract(year from expires_at) FROM photo.idempotency_records WHERE tenant_id=%s AND principal_id=%s AND route='Portal.Selection' AND key=%s",(c['tenant'],c['guest'],c['selection'])).fetchone()==(410,9999)
    other=context(db)
    db.execute("INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES(%s,'guest',%s,'Portal.Download.Start',%s,%s,clock_timestamp()+interval '1 hour')",(other['tenant'],other['guest'],other['start'],'e'*64))
    other_sha=digest({'collection_revision':str(other['revision']),'scope':'selected','item_ids':[str(other['item'])]})
    assert abandon(db,other,other_sha)[0]=='conflict'
    assert db.execute("SELECT request_hash,response_status FROM photo.idempotency_records WHERE tenant_id=%s AND principal_id=%s AND route='Portal.Download.Start' AND key=%s",(other['tenant'],other['guest'],other['start'])).fetchone()==('e'*64,None)


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler'])
def test_only_portal_role_may_resolve_guest_intent(db,role):
    import psycopg
    c=context(db);sha=digest({'collection_revision':str(c['revision']),'scope':'selected','item_ids':[str(c['item'])]})
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        with db.transaction():
            db.execute('SET LOCAL ROLE '+role)
            db.execute('SELECT * FROM photo.abandon_portal_download_preparation(%s,%s,%s,%s,%s,NULL,%s)',(c['session'],c['csrf'],c['selection'],sha,c['start'],c['preset']))
    db.execute('RESET ROLE')
