"""Outbox leases, replay and crash recovery against disposable PostgreSQL."""
from hashlib import sha256
from pathlib import Path
from uuid import uuid4
import psycopg
import pytest

ROOT=Path(__file__).resolve().parents[2]
pytestmark=[pytest.mark.integration,pytest.mark.migration_head(17)]
RESULT=sha256(b'recipient-acknowledgement-fixture').digest()

@pytest.fixture
def outbox(database):
    with psycopg.connect(**database,autocommit=True) as c:
        for name in ('02598_billing_catalog_scaffold.sql','02600_billing_quota_accounting.sql',
                     '02660_billing_finance_schema.sql','02662_billing_provider_schema.sql',
                     '02672_billing_outbox_worker_schema.sql','02673_billing_outbox_worker_functions.sql'):
            with c.transaction():c.execute((ROOT/'backend/db/migrations'/name).read_text().split('-- +goose Down')[0])
        yield c,database

def insert(c,topic='billing.webhook.apply'):
    return c.execute("INSERT INTO photo.billing_outbox(id,topic,business_key,payload) VALUES(gen_random_uuid(),%s,%s,'{\"test\":true}') RETURNING id",(topic,str(uuid4()))).fetchone()[0]

def call(c,sql,args,role='photo_maintenance'):
    with c.transaction():
        c.execute('SET LOCAL ROLE '+role)
        return c.execute(sql,args).fetchall()

def claim(c,worker,topic='billing.webhook.apply',count=10,lease=60):
    return call(c,'SELECT * FROM photo.billing_claim_outbox(%s,%s,%s,%s)',(topic,worker,count,lease))

def finish(c,message,worker,token,outcome='sent',result=RESULT,code=None,delay=None):
    return call(c,'SELECT photo.billing_finish_outbox(%s,%s,%s,%s,%s,%s,%s)',(message,worker,token,outcome,result,code,delay))[0][0]

def test_claim_topic_scope_and_sent_replay(outbox):
    c,_=outbox;message=insert(c);other=insert(c,'billing.email');worker=uuid4()
    rows=claim(c,worker);assert len(rows)==1 and rows[0][0]==message
    assert rows[0][2]=={'test':True}
    token=rows[0][3]
    assert finish(c,message,worker,token)=='sent'
    assert finish(c,message,worker,token)=='sent'
    assert claim(c,worker)==[]
    with pytest.raises(psycopg.errors.RaiseException,match='BILLING_OUTBOX_HASH_CONFLICT'):
        finish(c,message,worker,token,result=sha256(b'changed').digest())
    assert claim(c,worker,'billing.email')[0][0]==other

def test_expired_processing_reclaimed_and_stale_finish_denied(outbox):
    c,_=outbox;message=insert(c);worker=uuid4();old=claim(c,worker)[0]
    c.execute("UPDATE photo.billing_outbox SET lease_until=clock_timestamp()-interval '1 second' WHERE id=%s",(message,))
    with pytest.raises(psycopg.errors.RaiseException,match='BILLING_FENCE_STALE'):finish(c,message,worker,old[3])
    replacement=uuid4();new=claim(c,replacement)[0];assert new[3]!=old[3]
    with pytest.raises(psycopg.errors.RaiseException,match='BILLING_FENCE_STALE'):finish(c,message,worker,old[3])
    assert finish(c,message,replacement,new[3])=='sent'
    assert c.execute('SELECT attempts FROM photo.billing_outbox WHERE id=%s',(message,)).fetchone()[0]==2

def test_retry_backoff_and_dead_letter_evidence(outbox):
    c,_=outbox;message=insert(c);worker=uuid4();row=claim(c,worker)[0]
    assert finish(c,message,worker,row[3],'retry',None,'RECIPIENT_TIMEOUT',60)=='retry'
    assert finish(c,message,worker,row[3],'retry',None,'RECIPIENT_TIMEOUT',60)=='retry'
    assert claim(c,worker)==[]
    c.execute("UPDATE photo.billing_outbox SET next_retry_at=clock_timestamp()-interval '1 second' WHERE id=%s",(message,))
    new=claim(c,worker)[0]
    assert finish(c,message,worker,new[3],'dead',None,'RECIPIENT_REJECTED')=='dead'
    assert finish(c,message,worker,new[3],'dead',None,'RECIPIENT_REJECTED')=='dead'
    assert claim(c,worker)==[]
    assert c.execute('SELECT last_error_code,finish_hash FROM photo.billing_outbox WHERE id=%s',(message,)).fetchone()==('RECIPIENT_REJECTED',sha256(b'["dead", null, "RECIPIENT_REJECTED", null]').digest())

def test_skip_locked_and_no_concurrent_duplicate(outbox):
    c,config=outbox;first=insert(c);second=insert(c);worker=uuid4()
    with psycopg.connect(**config,autocommit=True) as other:
        with c.transaction():
            c.execute('SELECT id FROM photo.billing_outbox WHERE id=%s FOR UPDATE',(first,))
            claimed=claim(other,worker)
            assert [r[0] for r in claimed]==[second]
        assert [r[0] for r in claim(c,uuid4())]==[first]
        assert claim(other,worker)==[]

@pytest.mark.parametrize('role',['photo_api','photo_auth','photo_worker','photo_scheduler'])
def test_runtime_roles_cannot_claim_or_finish(outbox,role):
    c,_=outbox
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        call(c,'SELECT * FROM photo.billing_claim_outbox(%s,%s,1,60)',('billing.email',uuid4()),role)
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        call(c,"SELECT photo.billing_finish_outbox(%s,%s,%s,'sent',%s,NULL,NULL)",(uuid4(),uuid4(),uuid4(),RESULT),role)

@pytest.mark.parametrize('count,lease',[(0,60),(101,60),(1,0),(1,3601)])
def test_claim_bounds(outbox,count,lease):
    c,_=outbox
    with pytest.raises(psycopg.errors.RaiseException,match='BILLING_OUTBOX_INPUT_INVALID'):claim(c,uuid4(),count=count,lease=lease)
