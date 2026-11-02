"""Deletion preflight records a rights request before resolving studio ownership."""
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from threading import Barrier
from uuid import uuid4
import psycopg
import pytest
from test_billing_privacy_preferences import preferences
ROOT=Path(__file__).resolve().parents[2]
pytestmark=[pytest.mark.integration,pytest.mark.migration_head(17)]

@pytest.fixture
def preflight(preferences):
    c,config,user,session=preferences
    for name in ('02667_billing_legal_auth_tables.sql','02676_billing_deletion_preflight_schema.sql','02677_billing_deletion_preflight_functions.sql'):
        with c.transaction():c.execute((ROOT/'backend/db/migrations'/name).read_text().split('-- +goose Down')[0])
    yield c,config,user,session

def prepare(c,user,session,key=None,epoch=0,role='photo_auth'):
    with c.transaction():
        c.execute('SET LOCAL ROLE '+role)
        return c.execute('SELECT * FROM photo.account_prepare_deletion(%s,%s,%s,%s)',(user,session,epoch,key or str(uuid4()))).fetchone()

def studio(c,user):
    with c.transaction():
        tenant=c.execute("INSERT INTO photo.tenants(name,owner_user_id,quotas,retention) VALUES('preflight',%s,'{}','{}') RETURNING id",(user,)).fetchone()[0]
        c.execute("INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES(%s,%s,'owner','active')",(tenant,user))
    return tenant


def test_preflight_without_tenants_creates_case_and_does_not_revoke(preflight):
    c,_,user,session=preflight;row=prepare(c,user,session)
    assert row[1:]==(1,[],'delete_profile')
    case=c.execute('SELECT user_id,assigned_queue,deadline_at-clock_timestamp() FROM photo.privacy_cases WHERE case_id=%s',(row[0],)).fetchone()
    assert case[0]==user and case[1]=='privacy-self-service'
    assert 29*86400<case[2].total_seconds()<=30*86400
    assert c.execute('SELECT status,auth_epoch,privacy_lifecycle_epoch FROM photo.users WHERE id=%s',(user,)).fetchone()==('active',0,1)
    assert c.execute('SELECT revoked_at FROM photo.sessions WHERE id=%s',(session,)).fetchone()[0] is None
    assert c.execute('SELECT count(*) FROM photo.profile_deletion_requests').fetchone()[0]==0

def test_only_owned_active_studios_are_in_snapshot_and_replay_is_stable(preflight):
    c,_,user,session=preflight;owned=studio(c,user)
    other=c.execute('INSERT INTO photo.users(email_normalized) VALUES(%s) RETURNING id',(str(uuid4())+'@preflight.invalid',)).fetchone()[0]
    studio(c,other);closed=studio(c,user);c.execute("UPDATE photo.tenants SET status='closed' WHERE id=%s",(closed,))
    key=str(uuid4());row=prepare(c,user,session,key)
    assert len(row[2])==1 and row[2][0]['tenant_id']==str(owned)
    studio(c,user)
    assert prepare(c,user,session,key)==row
    assert c.execute('SELECT count(*) FROM photo.privacy_cases').fetchone()[0]==1

def test_concurrent_same_key_has_one_case(preflight):
    c,config,user,session=preflight;key=str(uuid4());barrier=Barrier(2)
    def worker():
        with psycopg.connect(**config,autocommit=True) as other:
            barrier.wait();return prepare(other,user,session,key)
    with ThreadPoolExecutor(max_workers=2) as pool:results=list(pool.map(lambda _:worker(),range(2)))
    assert results[0]==results[1]
    assert c.execute('SELECT count(*) FROM photo.privacy_cases').fetchone()[0]==1

@pytest.mark.parametrize('mutation',['revoked','wrong_user','epoch'])
def test_authority_before_preflight_replay(preflight,mutation):
    c,_,user,session=preflight;key=str(uuid4());prepare(c,user,session,key);epoch=0
    if mutation=='revoked':c.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(session,))
    elif mutation=='wrong_user':user=uuid4()
    else:epoch=1
    with pytest.raises(psycopg.errors.RaiseException,match='BILLING_AUTHORITY_INVALID'):prepare(c,user,session,key,epoch)

@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler','photo_maintenance'])
def test_preflight_restricted_to_auth_role(preflight,role):
    c,_,user,session=preflight
    with pytest.raises(psycopg.errors.InsufficientPrivilege):prepare(c,user,session,role=role)


def test_preflight_actual_http_auth_and_db(preflight):
    import os
    import shutil
    import subprocess
    from psycopg.conninfo import make_conninfo
    _,database,_,_=preflight
    result=subprocess.run([shutil.which('go') or 'go','test','./internal/httpapi','-run','^TestIntegrationDeletionPreflightActualRoute$','-count=1','-v'],cwd=ROOT/'backend',env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database)),capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr
    assert 'SKIP' not in result.stdout
    assert '--- PASS: TestIntegrationDeletionPreflightActualRoute' in result.stdout


def test_preflight_does_not_wait_on_refresh_or_revoke_session_lock(preflight):
    c,config,user,session=preflight
    with psycopg.connect(**config,autocommit=True) as revoker:
        with revoker.transaction():
            revoker.execute('SELECT id FROM photo.sessions WHERE id=%s FOR UPDATE',(session,))
            with pytest.raises(psycopg.errors.LockNotAvailable):
                prepare(c,user,session)
            assert c.execute('SELECT count(*) FROM photo.privacy_cases').fetchone()[0]==0
    assert prepare(c,user,session)[1]==1
