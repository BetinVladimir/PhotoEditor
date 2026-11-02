"""User-global optional preferences: auth, consent audit, replay and concurrent CAS."""
from concurrent.futures import ThreadPoolExecutor
from hashlib import sha256
from pathlib import Path
from threading import Barrier
from uuid import uuid4
import psycopg
import pytest
ROOT=Path(__file__).resolve().parents[2]
pytestmark=[pytest.mark.integration,pytest.mark.migration_head(17)]

@pytest.fixture
def preferences(database):
    with psycopg.connect(**database,autocommit=True) as c:
        for name in ('02666_billing_legal_schema.sql','02674_billing_privacy_preferences_schema.sql','02675_billing_privacy_preferences_functions.sql'):
            with c.transaction():c.execute((ROOT/'backend/db/migrations'/name).read_text().split('-- +goose Down')[0])
        user=c.execute('INSERT INTO photo.users(email_normalized) VALUES(%s) RETURNING id',(str(uuid4())+'@privacy.invalid',)).fetchone()[0]
        session=c.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') RETURNING id",(user,sha256(str(uuid4()).encode()).digest())).fetchone()[0]
        yield c,database,user,session

def update(c,user,session,analytics=True,marketing=None,revision=1,key=None,epoch=0,role='photo_auth'):
    with c.transaction():
        c.execute('SET LOCAL ROLE '+role)
        return c.execute("SELECT * FROM photo.account_update_privacy_preferences(%s,%s,%s,%s,%s,%s,%s,'web')",(user,session,epoch,revision,analytics,marketing,key or str(uuid4()))).fetchone()

def test_defaults_and_two_independent_audit_changes(preferences):
    c,_,user,session=preferences
    row=update(c,user,session,False,False)
    assert row[:3]==(1,False,False)
    assert c.execute('SELECT count(*) FROM photo.account_legal_events').fetchone()[0]==0
    row=update(c,user,session,True,True)
    assert row[:3]==(2,True,True)
    assert c.execute('SELECT purpose,action FROM photo.account_legal_events ORDER BY purpose').fetchall()==[('cookie_analytics','granted'),('marketing','granted')]
    row=update(c,user,session,False,None,revision=2)
    assert row[:3]==(3,False,True)
    assert c.execute("SELECT count(*) FROM photo.account_legal_events WHERE action='revoked'").fetchone()[0]==1
    assert c.execute('SELECT status FROM photo.users WHERE id=%s',(user,)).fetchone()[0]=='active'

def test_exact_replay_returns_original_snapshot_after_later_change(preferences):
    c,_,user,session=preferences;key=str(uuid4())
    original=update(c,user,session,key=key)
    assert update(c,user,session,key=key)==original
    assert update(c,user,session,False,revision=2)[:3]==(3,False,False)
    assert update(c,user,session,key=key)==original
    assert c.execute('SELECT count(*) FROM photo.account_legal_events').fetchone()[0]==2
    with pytest.raises(psycopg.errors.RaiseException,match='PRIVACY_IDEMPOTENCY_CONFLICT'):
        update(c,user,session,False,key=key)

def test_changed_revision_is_conflict_and_no_audit_side_effect(preferences):
    c,_,user,session=preferences;update(c,user,session)
    with pytest.raises(psycopg.errors.RaiseException,match='PRIVACY_REVISION_CONFLICT'):update(c,user,session,False)
    assert c.execute('SELECT count(*) FROM photo.account_legal_events').fetchone()[0]==1

@pytest.mark.parametrize('mutation',['wrong_user','wrong_epoch','revoked','expired','inactive'])
def test_auth_is_checked_before_replay(preferences,mutation):
    c,_,user,session=preferences;key=str(uuid4());update(c,user,session,key=key);epoch=0
    if mutation=='wrong_user':user=uuid4()
    elif mutation=='wrong_epoch':epoch=1
    elif mutation=='revoked':c.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(session,))
    elif mutation=='expired':c.execute("UPDATE photo.sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(session,))
    else:c.execute("UPDATE photo.users SET status='disabled' WHERE id=%s",(user,))
    with pytest.raises(psycopg.errors.RaiseException,match='BILLING_AUTHORITY_INVALID'):update(c,user,session,key=key,epoch=epoch)

@pytest.mark.parametrize('same_key',[False,True])
def test_concurrent_first_updates(preferences,same_key):
    c,config,user,session=preferences;barrier=Barrier(2);key=str(uuid4())
    def worker():
        with psycopg.connect(**config,autocommit=True) as other:
            barrier.wait()
            try:return update(other,user,session,key=key if same_key else None)
            except psycopg.errors.RaiseException as exc:return str(exc).splitlines()[0]
    with ThreadPoolExecutor(max_workers=2) as pool:results=list(pool.map(lambda _:worker(),range(2)))
    successes=[r for r in results if isinstance(r,tuple)]
    assert len(successes)==(2 if same_key else 1)
    assert all(r[:3]==(2,True,False) for r in successes)
    if not same_key:assert 'PRIVACY_REVISION_CONFLICT' in results
    assert c.execute('SELECT count(*) FROM photo.account_legal_events').fetchone()[0]==1

@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler','photo_maintenance'])
def test_only_auth_can_update_and_no_direct_audit_mutation(preferences,role):
    c,_,user,session=preferences
    with pytest.raises(psycopg.errors.InsufficientPrivilege):update(c,user,session,role=role)
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        with c.transaction():
            c.execute('SET LOCAL ROLE '+role)
            c.execute('DELETE FROM photo.account_legal_events')

def test_event_history_append_only_for_function_owner(preferences):
    c,_,user,session=preferences;update(c,user,session)
    for operation in ('DELETE FROM photo.account_legal_events','UPDATE photo.account_legal_events SET action=action'):
        with pytest.raises(psycopg.errors.InsufficientPrivilege):
            with c.transaction():
                c.execute('SET LOCAL ROLE photo_fn_owner');c.execute(operation)


def test_real_http_auth_service_and_database(preferences):
    import os
    import shutil
    import subprocess
    from psycopg.conninfo import make_conninfo
    _,database,_,_=preferences
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database))
    result=subprocess.run([shutil.which('go') or 'go','test','./internal/httpapi','-run','^TestIntegrationPrivacyPreferencesActualRoute$','-count=1','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr
    assert 'SKIP' not in result.stdout
    assert '--- PASS: TestIntegrationPrivacyPreferencesActualRoute' in result.stdout


def test_session_revoke_lock_does_not_deadlock_privacy_update(preferences):
    c,config,user,session=preferences
    with psycopg.connect(**config,autocommit=True) as revoker:
        with revoker.transaction():
            revoker.execute('SELECT id FROM photo.sessions WHERE id=%s FOR UPDATE',(session,))
            with pytest.raises(psycopg.errors.LockNotAvailable):
                update(c,user,session)
            assert c.execute('SELECT count(*) FROM photo.account_legal_events').fetchone()[0]==0
    assert update(c,user,session)[:3]==(2,True,False)
