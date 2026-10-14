from uuid import uuid4
import pytest


@pytest.mark.parametrize('signature',[
 'photo.claim_v30_metadata_barrier(uuid,uuid)',
 'photo.update_v30_metadata_barrier_lease(uuid,uuid,uuid,uuid,boolean)'])
@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_gateway','photo_portal','photo_scheduler'])
def test_metadata_barrier_lease_acl(db,signature,role):
    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()==(role=='photo_scheduler',)


def test_metadata_barrier_lease_missing_scope(db):
    with db.transaction():
        tenant,child=uuid4(),uuid4()
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        assert db.execute('SELECT * FROM photo.claim_v30_metadata_barrier(%s,%s)',(tenant,child)).fetchall()==[]
        assert db.execute('SELECT photo.update_v30_metadata_barrier_lease(%s,%s,%s,%s,false)',(tenant,child,uuid4(),uuid4())).fetchone()==(False,)
