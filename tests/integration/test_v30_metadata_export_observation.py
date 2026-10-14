"""SQL readiness authority refusal cases; positive lifecycle remains required."""
from uuid import uuid4
import pytest


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_gateway','photo_portal','photo_scheduler'])
def test_delivery_producer_readiness_private_acl(db,role):
    assert db.execute("SELECT has_function_privilege(%s,'photo.v30_delivery_producer_ready(uuid,uuid,uuid,text)','EXECUTE')",(role,)).fetchone()==(False,)


@pytest.mark.parametrize('policy',[None,'','unknown','strip','copyright','approved_exif'])
def test_delivery_producer_missing_authority_never_ready(db,policy):
    assert db.execute('SELECT photo.v30_delivery_producer_ready(%s,%s,%s,%s)',
        (uuid4(),uuid4(),uuid4(),policy)).fetchone()==(False,)


def test_current_observation_missing_child_is_absent(db):
    assert db.execute('SELECT photo.v30_delivery_current_export_observation(%s,%s,%s,%s::jsonb)',
        (uuid4(),uuid4(),uuid4(),'{}')).fetchone()==(None,)
