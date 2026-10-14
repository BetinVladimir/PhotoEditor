"""Published metadata source authority tests; authored, NOT RUN."""
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal'])
def test_published_source_validator_private(db,role):
    assert db.execute("SELECT has_function_privilege(%s,'photo.v30_metadata_published_source_ready(uuid,uuid,jsonb,text[])','EXECUTE')",(role,)).fetchone()==(False,)


def test_no_publication_cannot_authorize_metadata_proof(db):
    tenant,parent=uuid4(),uuid4()
    proof={'metadata_source':{'artifact_id':str(uuid4()),'sha256':'a'*64}}
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        assert db.execute('SELECT photo.v30_metadata_published_source_ready(%s,%s,%s,%s)',(tenant,parent,Jsonb(proof),[])).fetchone()==(False,)
