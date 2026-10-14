from uuid import uuid4
import pytest


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_gateway','photo_portal','photo_scheduler'])
def test_metadata_activation_recovery_acl(db,role):
    assert db.execute("SELECT has_function_privilege(%s,'photo.v30_metadata_task_activated(uuid,uuid,uuid,jsonb)','EXECUTE')",(role,)).fetchone()==(role=='photo_scheduler',)


def test_metadata_activation_recovery_absent_barrier(db):
    with db.transaction():
        tenant=uuid4()
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        assert db.execute('SELECT photo.v30_metadata_task_activated(%s,%s,%s,%s::jsonb)',(tenant,uuid4(),uuid4(),'{}')).fetchone()==(False,)
