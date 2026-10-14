from uuid import uuid4
import pytest


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_gateway','photo_portal','photo_scheduler'])
def test_metadata_barrier_acl(db,role):
    signature='photo.begin_v30_metadata_barrier(uuid,uuid,integer,bigint,uuid,text,text,text,bigint)'
    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()==(role=='photo_scheduler',)


@pytest.mark.parametrize('size',[None,0,10])
def test_metadata_barrier_missing_child_refused(db,size):
    with db.transaction():
        tenant=uuid4()
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        assert db.execute('SELECT photo.begin_v30_metadata_barrier(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (tenant,uuid4(),1,1,uuid4(),'{}','{}','{}',size)).fetchone()==(False,)
