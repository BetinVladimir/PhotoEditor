"""Authored real two-connection unchanged-row lock waits; no tests executed."""
from concurrent.futures import ThreadPoolExecutor
from time import monotonic,sleep
from uuid import uuid4
import psycopg
from psycopg import sql
import pytest
from test_database import tenant


def seed_receipt(conn,kind):
    if kind=='task':
        tid,_=tenant(conn,str(uuid4())+'@outbox.test')
        return conn.execute("INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload,publish_lease_until) VALUES(%s,%s,'photo.events.v1','task.failed','{}',clock_timestamp()+interval '2 seconds') RETURNING id,publish_lease_until",(tid,uuid4())).fetchone()
    challenge=conn.execute("INSERT INTO photo.auth_challenges(email_normalized,code_hmac,expires_at,locale,request_ip_hash) VALUES(%s,%s,clock_timestamp()+interval '5 minutes','en',%s) RETURNING id",(str(uuid4())+'@mail.test',bytes(32),bytes(32))).fetchone()[0]
    payload=conn.execute("INSERT INTO photo.mail_payloads(challenge_id,recipient_cipher,payload_cipher,key_id,expires_at,message_id) VALUES(%s,%s,%s,'test',clock_timestamp()+interval '5 minutes',%s) RETURNING id",(challenge,b'recipient',b'payload',str(uuid4()))).fetchone()[0]
    return conn.execute("INSERT INTO photo.auth_mail_outbox(event_id,payload_id,publish_lease_until) VALUES(%s,%s,clock_timestamp()+interval '2 seconds') RETURNING id,publish_lease_until",(uuid4(),payload)).fetchone()


@pytest.mark.parametrize('kind',['task','mail'])
def test_mark_cannot_cross_expiry_waiting_for_unchanged_row(database,kind):
    table=sql.Identifier('photo','outbox' if kind=='task' else 'auth_mail_outbox')
    with psycopg.connect(**database) as seed:
        receipt,lease=seed_receipt(seed,kind)
    with psycopg.connect(**database) as blocker,psycopg.connect(**database,autocommit=True) as observer,psycopg.connect(**database) as publisher:
        blocker.execute(sql.SQL('SELECT id FROM {} WHERE id=%s FOR UPDATE').format(table),(receipt,))
        assert observer.execute('SELECT clock_timestamp()<%s',(lease,)).fetchone()==(True,),'scenario must enter wait before expiry'
        pid=publisher.info.backend_pid
        def mark():
            publisher.execute('SET LOCAL ROLE photo_publisher')
            publisher.execute("SET LOCAL statement_timeout='8s'")
            return publisher.execute('SELECT photo.mark_outbox_sent(%s,%s,%s)',(kind,receipt,lease)).fetchone()
        with ThreadPoolExecutor(max_workers=1) as pool:
            future=pool.submit(mark);deadline=monotonic()+5
            try:
                while True:
                    waiting=observer.execute("SELECT wait_event_type='Lock' FROM pg_stat_activity WHERE pid=%s",(pid,)).fetchone()
                    if waiting and waiting[0]:break
                    assert not future.done(),'mark never waited for row'
                    assert monotonic()<deadline,'row lock wait not observed'
                    sleep(.01)
                observer.execute('SELECT pg_sleep(greatest(0,extract(epoch FROM %s::timestamptz-clock_timestamp()))+.05)',(lease,))
            finally:
                blocker.rollback()
            assert future.result(timeout=5)==(False,)
    with psycopg.connect(**database) as inspect:
        assert inspect.execute(sql.SQL('SELECT sent_at FROM {} WHERE id=%s').format(table),(receipt,)).fetchone()==(None,)


@pytest.mark.parametrize('kind',['task','mail'])
def test_live_mark_exact_replay_and_changed_lease_refusal(db,kind):
    receipt,lease=seed_receipt(db,kind)
    db.execute('SET LOCAL ROLE photo_publisher')
    assert db.execute('SELECT photo.mark_outbox_sent(%s,%s,%s)',(kind,receipt,lease)).fetchone()==(True,)
    assert db.execute('SELECT photo.mark_outbox_sent(%s,%s,%s)',(kind,receipt,lease)).fetchone()==(True,)
    assert db.execute("SELECT photo.mark_outbox_sent(%s,%s,%s::timestamptz+interval '1 second')",(kind,receipt,lease)).fetchone()==(False,)
    assert db.execute('SELECT photo.mark_outbox_sent(%s,%s,NULL)',(kind,receipt)).fetchone()==(False,)
