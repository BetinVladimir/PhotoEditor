from uuid import uuid4
import pytest


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_gateway','photo_portal','photo_scheduler'])
def test_metadata_adoption_acl(db,role):
    signature='photo.adopt_v30_delivery_metadata(uuid,uuid,integer,bigint,uuid,text,text,text,text,text,bigint)'
    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()==(role=='photo_scheduler',)


def test_metadata_adoption_missing_scope_refused(db):
    with db.transaction():
        tenant=uuid4()
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        args=(tenant,uuid4(),1,1,uuid4(),'{}','{}','{}','{}','{}',10)
        assert db.execute('SELECT photo.adopt_v30_delivery_metadata(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()==(False,)
