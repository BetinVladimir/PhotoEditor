"""Real SQL role, pagination and envelope rejection; never publishes events."""
from uuid import UUID, uuid4
import psycopg
from psycopg.types.json import Jsonb
import pytest
from test_database import tenant
from photo_worker.runtime.restore_outbox_inventory import pending_task_inventory

SIGNATURE='photo.restore_task_outbox_page(text,uuid,integer)'


def test_restore_inventory_narrow_authority(db):
    for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway',
                 'photo_maintenance','photo_portal','photo_auth'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,SIGNATURE)).fetchone()==(False,)
    assert db.execute("SELECT has_function_privilege('photo_publisher',%s,'EXECUTE')",(SIGNATURE,)).fetchone()==(True,)
    assert db.execute("SELECT has_table_privilege('photo_publisher','photo.outbox','SELECT')").fetchone()==(False,)
    row=db.execute('SELECT prosecdef,pg_get_userbyid(proowner),proconfig FROM pg_proc WHERE oid=%s::regprocedure',(SIGNATURE,)).fetchone()
    assert row[0] and row[1]=='photo_fn_owner' and 'search_path=pg_catalog, photo' in row[2]


@pytest.mark.parametrize('mode',['foreign_database','null_database','zero_limit','large_limit','null_limit'])
def test_restore_inventory_scope_refusal(db,mode):
    database=db.execute('SELECT current_database()').fetchone()[0];limit=128
    if mode=='foreign_database':database+='_wrong'
    if mode=='null_database':database=None
    if mode=='zero_limit':limit=0
    if mode=='large_limit':limit=129
    if mode=='null_limit':limit=None
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_publisher')
            db.execute('SELECT * FROM photo.restore_task_outbox_page(%s,NULL,%s)',(database,limit))


def test_restore_inventory_keyset_pages_and_same_count_substitution(db):
    scope,_=tenant(db,str(uuid4())+'@example.test')
    database=db.execute('SELECT current_database()').fetchone()[0]
    # Dedicated per-test DB may contain setup events: keep the assertion scoped
    # to complete current pending inventory rather than deleting unrelated rows.
    inserted=[]
    for _ in range(205):
        key=uuid4();inserted.append(key)
        db.execute("INSERT INTO photo.outbox(id,tenant_id,event_id,destination,routing_key,payload) VALUES(%s,%s,%s,'tasks','route',%s)",
                   (key,scope,uuid4(),Jsonb({'task':str(key)})))
    sent=uuid4()
    db.execute("INSERT INTO photo.outbox(id,tenant_id,event_id,destination,routing_key,payload,sent_at) VALUES(%s,%s,%s,'tasks','route','{}',now())",(sent,scope,uuid4()))
    def read():
        result=[];after=None
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_publisher')
            while True:
                page=db.execute('SELECT * FROM photo.restore_task_outbox_page(%s,%s,128)',(database,after)).fetchall()
                assert len(page)<=128
                result.extend(page)
                if len(page)<128:break
                after=page[-1][0]
        return result
    rows=read();ids=[r[0] for r in rows]
    assert set(inserted)<=set(ids) and sent not in ids
    assert ids==sorted(set(ids))
    digest=pending_task_inventory(rows,expected_count=len(rows),check_active=lambda:None)
    db.execute('UPDATE photo.outbox SET payload=%s WHERE id=%s',(Jsonb({'changed':True}),inserted[0]))
    changed=read();assert len(changed)==len(rows)
    assert pending_task_inventory(changed,expected_count=len(changed),check_active=lambda:None)!=digest


@pytest.mark.parametrize('mode',['oversized_payload','array_payload','oversized_destination','nil_event'])
def test_restore_inventory_rejects_unbounded_or_invalid_envelope(db,mode):
    scope,_=tenant(db,str(uuid4())+'@example.test')
    database=db.execute('SELECT current_database()').fetchone()[0]
    payload={'blob':'x'*(1<<20)} if mode=='oversized_payload' else ([] if mode=='array_payload' else {})
    destination='x'*4097 if mode=='oversized_destination' else 'tasks'
    event=UUID(int=0) if mode=='nil_event' else uuid4()
    db.execute('INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(%s,%s,%s,%s,%s)',(scope,event,destination,'route',Jsonb(payload)))
    with pytest.raises(psycopg.errors.InvalidParameterValue,match='RESTORE_OUTBOX_INVENTORY_ENVELOPE'):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_publisher')
            after=None
            while True:
                page=db.execute('SELECT * FROM photo.restore_task_outbox_page(%s,%s,128)',(database,after)).fetchall()
                if len(page)<128:break
                after=page[-1][0]
