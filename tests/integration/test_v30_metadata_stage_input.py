"""Lease-bound metadata stage input SQL tests; authored, NOT RUN."""
from uuid import uuid4
import pytest


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal'])
def test_metadata_stage_input_scheduler_only(db,role):
    signature='photo.read_v30_metadata_stage_input(uuid,uuid,uuid,uuid)'
    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()==(role=='photo_scheduler',)


def test_metadata_stage_input_missing_lease_returns_no_work(db):
    tenant=uuid4()
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        assert db.execute('SELECT * FROM photo.read_v30_metadata_stage_input(%s,%s,%s,%s)',
            (tenant,uuid4(),uuid4(),uuid4())).fetchall()==[]
