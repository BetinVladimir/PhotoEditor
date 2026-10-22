"""Authored SQL settlement scenarios. Physical purge is a maintenance caller precondition.
The isolated adapter executes these scenarios against a current-head private DB;
execution evidence is recorded under output-cleanup-refund-isolated4. Physical
object-store deletion is not performed here and remains a cleaner-test boundary.
"""
from uuid import uuid4
import pytest
from test_v27_shown_retouch_live_io import claimed_fixture
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


def output(db, ready):
    x=claimed_fixture(db)
    initial=db.execute("SELECT id,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_id=%s AND holder_type='run' AND state='reserved'",(x['tenant'],x['run'])).fetchone()
    usage=db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(x['tenant'],)).fetchone()
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_gateway')
        ticket=db.execute('SELECT * FROM photo.begin_output(%s,1,%s,%s,%s,%s,%s,2,%s,%s,%s)',(x['task'],x['fence'],x['worker'],'f'*64,uuid4(),'pixels','a'*64,'application/x-npy','test')).fetchone()
        assert ticket[0]=='created'
        if ready:
            assert db.execute('SELECT * FROM photo.complete_output(%s,1,%s,%s,%s,%s,%s,2,%s,%s)',(x['task'],x['fence'],x['worker'],'f'*64,ticket[1],'output-version','a'*64,'application/x-npy')).fetchone()[0]=='committed'
        db.execute('RESET ROLE')
    db.execute("UPDATE photo.tasks SET lease_until=clock_timestamp()-interval '1 second' WHERE id=%s",(x['task'],))
    db.execute("UPDATE photo.output_uploads SET cleanup_after=clock_timestamp()-interval '1 second' WHERE id=%s",(ticket[1],))
    return x,initial,usage,ticket


def maintenance(db,sql,args=()):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_maintenance')
        row=db.execute(sql,args).fetchone()
        db.execute('RESET ROLE')
        return row


@pytest.mark.parametrize('ready',[False,True])
def test_confirmed_cleanup_restores_exact_run_allocation_once(database,ready):
    x,q,usage,ticket=output(database,ready)
    claim=maintenance(database,'SELECT * FROM photo.claim_output_cleanup()')
    assert claim[0]==ticket[1]
    assert maintenance(database,'SELECT photo.finish_output_cleanup(%s,%s)',(claim[0],uuid4()))==(False,)
    assert database.execute('SELECT bytes FROM photo.quota_reservations WHERE id=%s',(q[0],)).fetchone()==(q[1]-2,)
    # Represents the call made only after the physical purge has returned confirmed.
    assert maintenance(database,'SELECT photo.finish_output_cleanup(%s,%s)',claim[:2])==(True,)
    assert database.execute('SELECT bytes FROM photo.quota_reservations WHERE id=%s',(q[0],)).fetchone()==(q[1],)
    assert database.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(x['tenant'],)).fetchone()==usage
    assert database.execute('SELECT state,returned_to_run_reservation FROM photo.output_uploads WHERE id=%s',(ticket[1],)).fetchone()==('cleaned',True)
    assert maintenance(database,'SELECT photo.finish_output_cleanup(%s,%s)',claim[:2])==(False,)
    database.execute("UPDATE photo.output_uploads SET cleanup_after=clock_timestamp()-interval '1 second' WHERE id=%s",(ticket[1],))
    revisit=maintenance(database,'SELECT * FROM photo.claim_output_cleanup()')
    assert revisit[0]==ticket[1]
    assert maintenance(database,'SELECT photo.finish_output_cleanup(%s,%s)',revisit[:2])==(True,)
    assert database.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(x['tenant'],)).fetchone()==usage


def test_ready_orphan_with_live_artifact_root_is_never_claimed(database):
    x,q,usage,ticket=output(database,True)
    # A separate same-project V2 task is a legitimate live consumer root.
    # A running shown task cannot acquire new inputs after its graph is sealed.
    consumer=database.execute("SELECT t.id,w.project_id FROM photo.tasks t JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id WHERE t.tenant_id=%s AND w.project_id=(SELECT project_id FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s LIMIT 1) ORDER BY t.id LIMIT 1",(x['tenant'],x['tenant'],x['task'])).fetchone()
    assert consumer is not None
    assert database.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) SELECT %s,%s,%s,%s,%s,'NpyArray',coalesce(max(ordinal),-1)+1 FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s",(x['tenant'],consumer[1],consumer[0],ticket[2],'a'*64,x['tenant'],consumer[0])).rowcount==1
    assert maintenance(database,'SELECT * FROM photo.claim_output_cleanup()') is None
    assert database.execute('SELECT state,gc_storage_settled FROM photo.artifacts WHERE id=%s',(ticket[2],)).fetchone()==('ready',False)


@pytest.mark.parametrize('mutation',["state='released'","expires_at=clock_timestamp()-interval '1 second'","principal_id='00000000-0000-0000-0000-000000000001'::uuid"])
def test_cleanup_does_not_refund_ineligible_reservation(database,mutation):
    x,q,usage,ticket=output(database,True)
    database.execute('UPDATE photo.quota_reservations SET '+mutation+' WHERE id=%s',(q[0],))
    claim=maintenance(database,'SELECT * FROM photo.claim_output_cleanup()')
    assert maintenance(database,'SELECT photo.finish_output_cleanup(%s,%s)',claim[:2])==(True,)
    assert database.execute('SELECT returned_to_run_reservation FROM photo.output_uploads WHERE id=%s',(ticket[1],)).fetchone()==(False,)
    assert database.execute('SELECT storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(x['tenant'],)).fetchone()==(usage[1],)


def test_retry_wait_does_not_consume_attempt_before_output_settlement(database):
    x,q,usage,ticket=output(database,False)
    assert maintenance(database,'SELECT photo.reap_tasks(1)')==(1,)
    task=database.execute('SELECT attempt,fencing_token,retry_count,oom_count,execution_profile FROM photo.tasks WHERE id=%s',(x['task'],)).fetchone()
    from psycopg.types.json import Jsonb
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_scheduler')
        assert database.execute("SELECT photo.plan_task_retry(%s,%s,%s,'retry_wait',%s,%s,clock_timestamp(),%s,'lease_lost')",(x['task'],task[0],task[1],task[2]+1,task[3],Jsonb(task[4]))).fetchone()==(True,)
        assert database.execute('SELECT photo.activate_task_retries(1)').fetchone()==(0,)
        database.execute('RESET ROLE')
    assert database.execute('SELECT state,attempt FROM photo.tasks WHERE id=%s',(x['task'],)).fetchone()==('retry_wait',task[0])
    claim=maintenance(database,'SELECT * FROM photo.claim_output_cleanup()')
    assert maintenance(database,'SELECT photo.finish_output_cleanup(%s,%s)',claim[:2])==(True,)
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_scheduler')
        assert database.execute('SELECT photo.activate_task_retries(1)').fetchone()==(1,)
        database.execute('RESET ROLE')
    assert database.execute('SELECT state,attempt FROM photo.tasks WHERE id=%s',(x['task'],)).fetchone()==('ready',task[0])


def scheduler_renew(db):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        result=db.execute('SELECT photo.renew_next_retry_cleanup_quota()').fetchone()
        db.execute('RESET ROLE')
        return result


def test_cleanup_wait_renews_only_existing_capacity_in_bounded_chunks(database):
    x,q,usage,ticket=output(database,False)
    assert maintenance(database,'SELECT photo.reap_tasks(1)')==(1,)
    database.execute("UPDATE photo.output_uploads SET cleanup_after=created_at+interval '3 hours' WHERE id=%s",(ticket[1],))
    database.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()+interval '2 minutes' WHERE id=%s",(q[0],))
    before=database.execute('SELECT bytes FROM photo.quota_reservations WHERE id=%s',(q[0],)).fetchone()
    allocated_usage=database.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(x['tenant'],)).fetchone()
    assert scheduler_renew(database)==(True,)
    assert database.execute("SELECT expires_at>clock_timestamp()+interval '10 minutes' AND expires_at<=clock_timestamp()+interval '15 minutes' AND expires_at<=(SELECT max(created_at)+interval '4 hours' FROM photo.output_uploads WHERE run_reservation_id=%s AND NOT returned_to_run_reservation) FROM photo.quota_reservations WHERE id=%s",(q[0],q[0])).fetchone()==(True,)
    assert database.execute('SELECT bytes FROM photo.quota_reservations WHERE id=%s',(q[0],)).fetchone()==before
    assert database.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(x['tenant'],)).fetchone()==allocated_usage
    assert scheduler_renew(database)==(False,)


@pytest.mark.parametrize('mutation',['expired','bound_elapsed','cancelled','wrong_principal','terminal'])
def test_cleanup_holding_never_revives_expired_or_foreign_authority(database,mutation):
    x,q,usage,ticket=output(database,False)
    assert maintenance(database,'SELECT photo.reap_tasks(1)')==(1,)
    database.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()+interval '2 minutes' WHERE id=%s",(q[0],))
    if mutation=='expired':
        database.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(q[0],))
    elif mutation=='bound_elapsed':
        database.execute("UPDATE photo.output_uploads SET created_at=clock_timestamp()-interval '5 hours' WHERE id=%s",(ticket[1],))
    elif mutation=='cancelled':
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(x['run'],))
    elif mutation=='wrong_principal':
        database.execute('UPDATE photo.quota_reservations SET principal_id=%s WHERE id=%s',(uuid4(),q[0]))
    else:
        database.execute("UPDATE photo.tasks SET state='failed' WHERE id=%s",(x['task'],))
    before=database.execute('SELECT expires_at,bytes FROM photo.quota_reservations WHERE id=%s',(q[0],)).fetchone()
    assert scheduler_renew(database)==(False,)
    assert database.execute('SELECT expires_at,bytes FROM photo.quota_reservations WHERE id=%s',(q[0],)).fetchone()==before


@pytest.mark.parametrize('ready',[False,True])
def test_concurrent_confirmed_refund_and_renewal_preserve_one_allocation(database,ready):
    """Real entrypoints/locks; committed fixture models confirmed purge only.

    Physical storage verification is intentionally outside this SQL race. No
    counter is changed directly to simulate settlement or successful renewal.
    """
    from concurrent.futures import ThreadPoolExecutor
    from threading import Barrier
    import psycopg
    x,q,usage,ticket=output(database,ready)
    assert maintenance(database,'SELECT photo.reap_tasks(1)')==(1,)
    database.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()+interval '2 minutes' WHERE id=%s",(q[0],))
    claim=maintenance(database,'SELECT * FROM photo.claim_output_cleanup()')
    assert claim[0]==ticket[1]
    database.commit()
    barrier=Barrier(3)
    def finish():
        with psycopg.connect(database.info.dsn) as conn:
            conn.execute("SET LOCAL statement_timeout='8s'")
            barrier.wait(timeout=5)
            return maintenance(conn,'SELECT photo.finish_output_cleanup(%s,%s)',claim[:2])[0]
    def renew():
        with psycopg.connect(database.info.dsn) as conn:
            conn.execute("SET LOCAL statement_timeout='8s'")
            barrier.wait(timeout=5)
            return scheduler_renew(conn)[0]
    with ThreadPoolExecutor(max_workers=3) as pool:
        futures=[pool.submit(finish),pool.submit(finish),pool.submit(renew)]
        results=[future.result(timeout=12) for future in futures]
    assert sorted(results[:2])==[False,True]
    assert type(results[2]) is bool  # valid before refund, absent after it
    assert database.execute('SELECT bytes,state FROM photo.quota_reservations WHERE id=%s',(q[0],)).fetchone()==(q[1],'reserved')
    assert database.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(x['tenant'],)).fetchone()==usage
    assert database.execute('SELECT state,returned_to_run_reservation FROM photo.output_uploads WHERE id=%s',(ticket[1],)).fetchone()==('cleaned',True)
