"""Live scoped routing metadata, not admission authority."""
from hashlib import sha256
from uuid import uuid4
import pytest
from psycopg import errors
from test_v27_target_checkpoint import seed_checkpoint, save, ROLES
from photo_worker.contracts.base import canonical_bytes


def lookup(db,args):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(args[0],))
        row=db.execute('SELECT * FROM photo.semantic_native_target_checkpoint(%s,%s,%s,%s,%s,%s,%s)',args).fetchone()
        db.execute('RESET ROLE')
        return row


def test_checkpoint_reader_scope_revoke_and_absence(db):
    packet,case,_,manifest=seed_checkpoint(db)
    args=[case['Tenant'],case['Project'],case['Actor'],case['Session'],packet['parent_run_id'],packet['request_sha'],case['Snapshot']]
    assert lookup(db,args) is None
    body=canonical_bytes(manifest).decode()
    assert save(db,packet,body)
    assert lookup(db,args)==(body,sha256(body.encode()).hexdigest())
    for i in range(7):
        changed=args.copy();changed[i]='f'*64 if i==5 else str(uuid4())
        assert lookup(db,changed) is None
    db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(case['Session'],))
    assert lookup(db,args) is None
    db.execute('UPDATE photo.sessions SET revoked_at=NULL WHERE id=%s',(case['Session'],))
    assert lookup(db,args)
    db.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(packet['run_id'],))
    assert lookup(db,args) is None


def test_checkpoint_reader_denied_roles(db):
    signature='photo.semantic_native_target_checkpoint(uuid,uuid,uuid,uuid,uuid,text,uuid)'
    for role in ROLES:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0]==(role=='photo_api')
        if role!='photo_api':
            with pytest.raises(errors.InsufficientPrivilege):
                with db.transaction():
                    db.execute('SET LOCAL ROLE '+role)
                    db.execute('SELECT * FROM photo.semantic_native_target_checkpoint(%s,%s,%s,%s,%s,%s,%s)',[str(uuid4()) for _ in range(5)]+['f'*64,str(uuid4())])
