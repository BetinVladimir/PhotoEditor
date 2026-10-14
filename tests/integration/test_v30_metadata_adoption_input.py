"""Current metadata adoption input authority; authored, NOT RUN."""
from uuid import uuid4
import pytest


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal'])
def test_metadata_adoption_input_scheduler_only(db,role):
    signature='photo.read_v30_metadata_adoption_input(uuid,uuid,integer,bigint,uuid)'
    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()==(role=='photo_scheduler',)


def test_missing_metadata_result_has_no_adoption_input(db):
    tenant=uuid4()
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        assert db.execute('SELECT * FROM photo.read_v30_metadata_adoption_input(%s,%s,%s,%s,%s)',
            (tenant,uuid4(),1,2,uuid4())).fetchall()==[]
