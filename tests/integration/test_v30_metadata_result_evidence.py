"""Authored metadata evidence ACL/refusal cases; not execution evidence."""
from uuid import uuid4
import pytest


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_gateway','photo_portal','photo_scheduler'])
@pytest.mark.parametrize('signature',[
 'photo.v30_metadata_result_artifact(uuid,uuid,integer,bigint,uuid,uuid)',
 'photo.v30_metadata_result_outputs(uuid,uuid,integer,bigint,uuid)'])
def test_metadata_result_read_acl(db,role,signature):
    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()==(role=='photo_scheduler',)


def test_metadata_result_missing_scope(db):
    with db.transaction():
        tenant=uuid4()
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        args=(tenant,uuid4(),1,1,uuid4())
        assert db.execute('SELECT * FROM photo.v30_metadata_result_outputs(%s,%s,%s,%s,%s)',args).fetchall()==[]
        assert db.execute('SELECT * FROM photo.v30_metadata_result_artifact(%s,%s,%s,%s,%s,%s)',args+(uuid4(),)).fetchall()==[]
