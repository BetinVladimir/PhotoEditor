from uuid import uuid4
import pytest


@pytest.mark.parametrize('name',['v30_metadata_transport_ready','activate_v30_metadata_task'])
@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_gateway','photo_portal','photo_scheduler'])
def test_metadata_transport_activation_acl(db,name,role):
    signature='photo.'+name+'(uuid,uuid,uuid,jsonb)'
    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()==(role=='photo_scheduler',)


@pytest.mark.parametrize('name',['v30_metadata_transport_ready','activate_v30_metadata_task'])
def test_metadata_transport_activation_missing_proof_refuses(db,name):
    with db.transaction():
        tenant=uuid4()
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        assert db.execute('SELECT photo.'+name+'(%s,%s,%s,%s::jsonb)',(tenant,uuid4(),uuid4(),'{}')).fetchone()==(False,)
