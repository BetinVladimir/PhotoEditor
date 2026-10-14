"""Metadata blocked-task authority and quota boundary; authored, NOT RUN."""
from uuid import uuid4
import pytest


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_gateway','photo_portal','photo_scheduler'])
def test_metadata_task_registration_acl(db,role):
    assert db.execute("SELECT has_function_privilege(%s,'photo.create_v30_metadata_task(uuid,uuid,uuid,jsonb,bigint)','EXECUTE')",(role,)).fetchone()==(role=='photo_scheduler',)


@pytest.mark.parametrize('reserved',[None,0,-1,1,9007199254740992])
def test_metadata_task_missing_proof_or_invalid_quota_refused(db,reserved):
    with db.transaction():
        tenant=uuid4()
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        assert db.execute('SELECT photo.create_v30_metadata_task(%s,%s,%s,%s::jsonb,%s)',
            (tenant,uuid4(),uuid4(),'{}',reserved)).fetchone()==(False,)
