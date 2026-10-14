"""Durable metadata pin boundary; authored, NOT RUN."""
from uuid import uuid4
import pytest


@pytest.mark.parametrize('role,allowed',[('photo_scheduler',True),('photo_api',False),('photo_worker',False),('photo_gateway',False),('photo_portal',False)])
def test_metadata_pin_scheduler_only(db,role,allowed):
    assert db.execute("SELECT has_function_privilege(%s,'photo.pin_v30_delivery_metadata_proof(uuid,uuid,uuid,text,text)','EXECUTE')",(role,)).fetchone()==(allowed,)


def test_metadata_pin_requires_existing_fenced_barrier(db):
    from hashlib import sha256
    raw='{}'
    with db.transaction():
        tid=uuid4()
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tid),))
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.pin_v30_delivery_metadata_proof(%s,%s,%s,%s,%s)',
            (tid,uuid4(),uuid4(),raw,sha256(raw.encode()).hexdigest())).fetchone()==(False,)



@pytest.mark.parametrize('role,allowed',[('photo_scheduler',True),('photo_api',False),('photo_worker',False),('photo_gateway',False),('photo_portal',False)])
def test_metadata_read_scheduler_only(db,role,allowed):
    assert db.execute("SELECT has_function_privilege(%s,'photo.read_v30_delivery_metadata_proof(uuid,uuid,uuid)','EXECUTE')",(role,)).fetchone()==(allowed,)


def test_metadata_read_missing_or_foreign_barrier(db):
    with db.transaction():
        tenant=uuid4()
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        db.execute('SET LOCAL ROLE photo_scheduler')
        for tid in (tenant,uuid4()):
            assert db.execute('SELECT * FROM photo.read_v30_delivery_metadata_proof(%s,%s,%s)',(tid,uuid4(),uuid4())).fetchall()==[]
