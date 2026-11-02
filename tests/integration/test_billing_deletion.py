"""LEG-F02/06: durable deletion queue on a disposable database, no erasure effects."""
from hashlib import sha256
from pathlib import Path
from uuid import uuid4
import psycopg
import pytest

ROOT = Path(__file__).resolve().parents[2]
pytestmark = [pytest.mark.integration, pytest.mark.migration_head(17)]
HASH = sha256(b'deletion-test-input').digest()
RESULT = sha256(b'deletion-test-result').digest()


@pytest.fixture
def queue(database):
    with psycopg.connect(**database, autocommit=True) as c:
        for name in ('02666_billing_legal_schema.sql', '02667_billing_legal_auth_tables.sql',
                     '02669_billing_deletion_steps.sql', '02670_billing_deletion_step_functions.sql'):
            with c.transaction(): c.execute((ROOT / 'backend/db/migrations' / name).read_text().split('-- +goose Down')[0])
        yield c, database


def request(c, state='purging'):
    case = c.execute("INSERT INTO photo.privacy_cases(subject_ref,kind,deadline_at,assigned_queue) VALUES (%s,'erasure',clock_timestamp()+interval '1 day','erasure') RETURNING case_id", (uuid4(),)).fetchone()[0]
    return c.execute('INSERT INTO photo.profile_deletion_requests(privacy_case_id,state,ownership_snapshot_hash,auth_epoch_at_acceptance,subject_lifecycle_epoch) VALUES (%s,%s,%s,1,1) RETURNING request_id', (case,state,HASH)).fetchone()[0]


def service(c, sql, values, role='photo_maintenance'):
    with c.transaction():
        c.execute('SET LOCAL ROLE ' + role)
        return c.execute(sql, values).fetchall()


def enqueue(c, req, name='purge_profile', ordinal=1, fingerprint=HASH):
    return service(c,'SELECT photo.account_enqueue_deletion_step(%s,%s,%s,%s)',(req,name,ordinal,fingerprint))[0][0]


def claim(c, worker=None, count=10, lease=60):
    return service(c,'SELECT * FROM photo.account_claim_deletion_step(%s,%s,%s)',(worker or uuid4(),count,lease))


def finish(c, step, worker, token, outcome='completed', result=RESULT, code=None, delay=None):
    return service(c,'SELECT photo.account_record_deletion_step(%s,%s,%s,%s,%s,%s,%s)',(step,worker,token,outcome,result,code,delay))[0][0]


def test_enqueue_idempotency_and_conflicting_input(queue):
    c,_=queue; req=request(c); step=enqueue(c,req)
    assert enqueue(c,req)==step
    with pytest.raises(psycopg.errors.RaiseException,match='DELETION_STEP_HASH_CONFLICT'):
        enqueue(c,req,fingerprint=RESULT)
    assert c.execute('SELECT count(*) FROM photo.profile_deletion_steps').fetchone()[0]==1


def test_steps_are_sequential_and_completion_replay_is_exact(queue):
    c,_=queue; req=request(c); first=enqueue(c,req,'revoke_sessions',1); second=enqueue(c,req,'purge_profile',2); worker=uuid4()
    rows=claim(c,worker); assert len(rows)==1 and rows[0][0]==first
    assert claim(c)==[]
    assert finish(c,first,worker,rows[0][3])=='completed'
    assert finish(c,first,worker,rows[0][3])=='completed'
    with pytest.raises(psycopg.errors.RaiseException,match='DELETION_STEP_HASH_CONFLICT'):
        finish(c,first,worker,rows[0][3],result=HASH)
    assert claim(c)[0][0]==second


def test_expired_lease_reclaimed_and_old_worker_cannot_complete(queue):
    c,_=queue; step=enqueue(c,request(c)); old_worker=uuid4(); old=claim(c,old_worker)[0]
    c.execute("UPDATE photo.profile_deletion_steps SET lease_until=clock_timestamp()-interval '1 second' WHERE step_id=%s",(step,))
    new_worker=uuid4(); new=claim(c,new_worker)[0]
    assert old[3]!=new[3]
    with pytest.raises(psycopg.errors.RaiseException,match='DELETION_FENCE_STALE'): finish(c,step,old_worker,old[3])
    assert finish(c,step,new_worker,new[3])=='completed'
    assert c.execute('SELECT attempts FROM photo.profile_deletion_steps WHERE step_id=%s',(step,)).fetchone()[0]==2


def test_expired_lease_without_reclaim_cannot_complete(queue):
    c,_=queue; step=enqueue(c,request(c)); worker=uuid4(); row=claim(c,worker)[0]
    c.execute("UPDATE photo.profile_deletion_steps SET lease_until=clock_timestamp()-interval '1 second' WHERE step_id=%s",(step,))
    with pytest.raises(psycopg.errors.RaiseException,match='DELETION_FENCE_STALE'): finish(c,step,worker,row[3])


def test_retry_is_idempotent_and_waits_for_backoff(queue):
    c,_=queue; step=enqueue(c,request(c)); worker=uuid4(); row=claim(c,worker)[0]
    assert finish(c,step,worker,row[3],'retry',None,'PROVIDER_TIMEOUT',60)=='retry'
    assert finish(c,step,worker,row[3],'retry',None,'PROVIDER_TIMEOUT',60)=='retry'
    assert claim(c)==[]
    with pytest.raises(psycopg.errors.RaiseException,match='DELETION_STEP_HASH_CONFLICT'):
        finish(c,step,worker,row[3],'retry',None,'PROVIDER_TIMEOUT',30)
    c.execute("UPDATE photo.profile_deletion_steps SET next_retry_at=clock_timestamp()-interval '1 second' WHERE step_id=%s",(step,))
    assert claim(c)[0][0]==step


def test_locked_step_is_skipped_without_claiming_successor(queue):
    c,config=queue; req=request(c); step=enqueue(c,req,'revoke_sessions',1); enqueue(c,req,'purge_profile',2)
    with psycopg.connect(**config,autocommit=True) as other:
        with c.transaction():
            c.execute('SET LOCAL ROLE photo_maintenance')
            assert c.execute('SELECT * FROM photo.account_claim_deletion_step(%s,1,60)',(uuid4(),)).fetchone()[0]==step
            assert claim(other)==[]
    assert claim(c)==[]


@pytest.mark.parametrize('state',['preflight','ownership_action_required','completed','partially_retained','failed'])
def test_inactive_request_cannot_enqueue(queue,state):
    c,_=queue
    with pytest.raises(psycopg.errors.RaiseException,match='DELETION_REQUEST_NOT_ACTIVE'): enqueue(c,request(c,state))


@pytest.mark.parametrize('count,lease',[(0,60),(101,60),(1,0),(1,3601)])
def test_claim_bounds(queue,count,lease):
    c,_=queue
    with pytest.raises(psycopg.errors.RaiseException,match='DELETION_CLAIM_INPUT_INVALID'): claim(c,count=count,lease=lease)


@pytest.mark.parametrize('role',['photo_api','photo_auth','photo_worker','photo_scheduler'])
def test_only_maintenance_can_claim(queue,role):
    c,_=queue
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        service(c,'SELECT * FROM photo.account_claim_deletion_step(%s,1,60)',(uuid4(),),role)


def test_maintenance_has_no_direct_table_write_or_secret_payload(queue):
    c,_=queue
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        service(c,"INSERT INTO photo.profile_deletion_steps(request_id,step_key,ordinal,input_hash) VALUES (%s,'purge_profile',1,%s)",(uuid4(),HASH))
    columns={x[0] for x in c.execute("SELECT column_name FROM information_schema.columns WHERE table_schema='photo' AND table_name='profile_deletion_steps'")}
    assert not columns.intersection({'payload','password','email','raw_error','provider_secret'})
