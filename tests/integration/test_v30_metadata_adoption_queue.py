"""Metadata adoption queue and durable receipt ACL; authored, NOT RUN."""
from uuid import uuid4
import pytest


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal'])
@pytest.mark.parametrize('signature',['photo.next_v30_metadata_adoption(uuid)','photo.v30_metadata_adoption_committed(uuid,uuid,integer,bigint,uuid)'])
def test_metadata_adoption_queue_scheduler_only(db,role,signature):
    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()==(role=='photo_scheduler',)


def test_metadata_adoption_missing_result_has_no_receipt(db):
    tenant=uuid4()
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        assert db.execute('SELECT photo.v30_metadata_adoption_committed(%s,%s,%s,%s,%s)',(tenant,uuid4(),1,2,uuid4())).fetchone()==(False,)
