from uuid import uuid4
import pytest


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_gateway','photo_portal','photo_scheduler'])
def test_metadata_verification_acl(db,role):
    signature='photo.record_v30_metadata_verification(uuid,uuid,integer,bigint,uuid,text,text,text,text,text,bigint)'
    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()==(role=='photo_scheduler',)


def test_metadata_verification_missing_result_refused(db):
    with db.transaction():
        tenant=uuid4()
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        args=(tenant,uuid4(),1,1,uuid4(),'{}','{}','{}','{}','{}',10)
        assert db.execute('SELECT photo.record_v30_metadata_verification(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()==(False,)
