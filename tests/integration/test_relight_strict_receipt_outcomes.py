"""Own metadata-only strict read classification, not admitted relight/GPU proof."""
from uuid import uuid4
from concurrent.futures import ThreadPoolExecutor
from time import monotonic, sleep
import pytest
import psycopg
from psycopg.types.json import Jsonb


def scope(db):
    actor,_=db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)",(f"strict-relight-{uuid4()}@test.example",)).fetchone()
    tenant=db.execute("SELECT id FROM photo.tenants WHERE owner_user_id=%s",(actor,)).fetchone()[0]
    session=db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,%s,'native',clock_timestamp()+interval '1 hour') RETURNING id",(actor,uuid4().bytes+uuid4().bytes,uuid4())).fetchone()[0]
    project=db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Strict relight receipt') RETURNING id",(tenant,)).fetchone()[0]
    return tenant,project,actor,session


@pytest.mark.parametrize('lane',['relight','analysis','stored','analysis_stored'])
@pytest.mark.parametrize('mode,expected',[('absent','absent'),('draft','absent'),('sealed_invalid','denied'),('changed_request','conflict'),('expired','denied'),('revoked','denied'),('downgraded','denied'),('wrong_context','denied')])
def test_strict_receipt_absence_is_not_authority_refusal(db,lane,mode,expected):
    tenant,project,actor,session=scope(db)
    run=uuid4();request={'run_id':str(run)};pin={};plan={'operation':'depth.predict','config':{'tenant_id':str(tenant),'project_id':str(project),'actor_id':str(actor),'auth_session_id':str(session),'snapshot_id':str(uuid4()),'base':{}}}
    config={'project_id':str(project),'v28_relight_request':request,'v28_relight_pin':pin,'auth_session_id':str(session)} if lane in ('relight','stored') else {'v28_analysis_plan':plan}
    if mode in ('draft','sealed_invalid','changed_request'):
        artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role) VALUES(%s,'photo',%s,'fixture-v1',%s,100,'application/json','working_image') RETURNING id",(tenant,str(uuid4()),'a'*64)).fetchone()[0]
        snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,artifact_id) VALUES(%s,'input','user',%s,%s,1,%s) RETURNING id",(tenant,actor,'a'*64,artifact)).fetchone()[0]
        db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,%s,'v2','draft',%s,%s,%s,%s,%s)",(run,tenant,snapshot,Jsonb({} if mode=='changed_request' else config),'b'*64,actor,mode=='sealed_invalid'))
    if mode=='expired':db.execute("UPDATE photo.sessions SET expires_at=clock_timestamp() WHERE id=%s",(session,))
    if mode=='revoked':db.execute("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s",(session,))
    if mode=='downgraded':db.execute("UPDATE photo.memberships SET role='viewer' WHERE tenant_id=%s AND user_id=%s",(tenant,actor))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(uuid4() if mode=='wrong_context' else tenant),))
        if lane=='analysis_stored':
            intent={'schema_version':1,'snapshot_id':plan['config']['snapshot_id'],'base':plan['config']['base'],'operation':plan['operation']}
            result=db.execute('SELECT photo.read_v28_relight_analysis_stored_outcome(%s,%s,%s,%s,%s,%s)',(tenant,project,run,actor,session,Jsonb(intent))).fetchone()[0]
        elif lane=='stored':
            result=db.execute('SELECT photo.read_v28_relight_stored_request_outcome(%s,%s,%s,%s,%s,%s)',(tenant,project,run,actor,session,Jsonb(request))).fetchone()[0]
        elif lane=='relight':
            result=db.execute('SELECT photo.read_v28_relight_request_outcome(%s,%s,%s,%s,%s,%s,%s)',(tenant,project,run,actor,session,Jsonb(request),Jsonb(pin))).fetchone()[0]
        else:
            result=db.execute('SELECT photo.read_v28_relight_analysis_outcome(%s,%s,%s,%s,%s)',(tenant,run,actor,session,Jsonb(plan))).fetchone()[0]
        db.execute('RESET ROLE')
    assert result=={'outcome':expected}


@pytest.mark.parametrize('signature', ['photo.read_v28_relight_request_outcome(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb)','photo.read_v28_relight_analysis_outcome(uuid,uuid,uuid,uuid,jsonb)','photo.read_v28_relight_stored_request_outcome(uuid,uuid,uuid,uuid,uuid,jsonb)','photo.read_v28_relight_analysis_stored_outcome(uuid,uuid,uuid,uuid,uuid,jsonb)'])
def test_strict_receipt_entry_is_scheduler_only(db,signature):
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(signature,)).fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')",(signature,)).fetchone()[0]


def wait_for_database_condition(check, description, timeout=8):
    deadline = monotonic() + timeout
    while monotonic() < deadline:
        if check():
            return
        sleep(0.02)
    pytest.fail(description, pytrace=False)


@pytest.mark.parametrize('lane', ['relight', 'analysis', 'stored', 'analysis_stored'])
@pytest.mark.parametrize('change', ['elapsed_expiry', 'revocation', 'membership', 'tenant'])
def test_strict_receipt_rechecks_authority_after_observed_lock_wait(database, lane, change):
    """Observe an actual tenant-lock wait before changing authority.

    No qualified receipt or GPU claim: even absence must not authorize admission
    when authority expires while the read is blocked. The elapsed expiry
    deadline is later than transaction start, so transaction now() is unsafe.
    """
    with psycopg.connect(**database) as seed:
        tenant, project, actor, session = scope(seed)
    run = uuid4()
    request = {'run_id': str(run)}
    plan = {'config': {'project_id': str(project)}}
    with psycopg.connect(**database) as blocker, psycopg.connect(**database) as reader, psycopg.connect(**database, autocommit=True) as observer:
        blocker.execute('SELECT id FROM photo.tenants WHERE id=%s FOR UPDATE', (tenant,))
        reader.execute("SET LOCAL statement_timeout='12s'")
        reader.execute("SET LOCAL lock_timeout='10s'")
        reader.execute('SET LOCAL ROLE photo_scheduler')
        reader.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
        reader_pid = reader.execute('SELECT pg_backend_pid()').fetchone()[0]
        blocker_pid = blocker.execute('SELECT pg_backend_pid()').fetchone()[0]

        def read_outcome():
            if lane == 'analysis_stored':
                return reader.execute('SELECT photo.read_v28_relight_analysis_stored_outcome(%s,%s,%s,%s,%s,%s)',
                                      (tenant, project, run, actor, session, Jsonb(plan))).fetchone()[0]
            if lane == 'stored':
                return reader.execute('SELECT photo.read_v28_relight_stored_request_outcome(%s,%s,%s,%s,%s,%s)',
                                      (tenant, project, run, actor, session, Jsonb(request))).fetchone()[0]
            if lane == 'relight':
                return reader.execute('SELECT photo.read_v28_relight_request_outcome(%s,%s,%s,%s,%s,%s,%s)',
                                      (tenant, project, run, actor, session, Jsonb(request), Jsonb({}))).fetchone()[0]
            return reader.execute('SELECT photo.read_v28_relight_analysis_outcome(%s,%s,%s,%s,%s)',
                                  (tenant, run, actor, session, Jsonb(plan))).fetchone()[0]

        with ThreadPoolExecutor(max_workers=1) as executor:
            pending = executor.submit(read_outcome)
            try:
                wait_for_database_condition(
                    lambda: observer.execute('SELECT %s=ANY(pg_blocking_pids(%s))', (blocker_pid, reader_pid)).fetchone()[0],
                    'Receipt reader never reached the held tenant lock')
                assert not pending.done()
                if change == 'elapsed_expiry':
                    blocker.execute("UPDATE photo.sessions SET expires_at=clock_timestamp()+interval '200 milliseconds' WHERE id=%s", (session,))
                    wait_for_database_condition(
                        lambda: blocker.execute('SELECT expires_at<=clock_timestamp() FROM photo.sessions WHERE id=%s', (session,)).fetchone()[0],
                        'Fixture session did not expire during observed lock wait')
                elif change == 'revocation':
                    blocker.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (session,))
                elif change == 'membership':
                    blocker.execute("UPDATE photo.memberships SET role='viewer' WHERE tenant_id=%s AND user_id=%s", (tenant, actor))
                else:
                    blocker.execute("UPDATE photo.tenants SET status='deleting' WHERE id=%s", (tenant,))
                blocker.commit()
                assert pending.result(timeout=5) == {'outcome': 'denied'}
            finally:
                # Release the blocker before executor shutdown even on failed
                # observations; the bounded SQL timeouts stop a stuck reader.
                blocker.rollback()
