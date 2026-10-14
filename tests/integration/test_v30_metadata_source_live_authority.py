"""Live original metadata authority refusal and ACL tests; NOT RUN."""
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal'])
def test_source_live_authority_api_only(db,role):
    signature='photo.v30_metadata_source_authorized(uuid,uuid,uuid,uuid,jsonb,text,text,jsonb,text,text,text[])'
    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()==(role=='photo_api',)


def test_source_live_authority_missing_or_foreign_scope(db):
    tenant,project,actor,session=[uuid4() for _ in range(4)]
    query='SELECT photo.v30_metadata_source_authorized(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)'
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        args=[tenant,project,actor,session,Jsonb({}),'{}','0'*64,Jsonb({}),'{}','copyright',[]]
        assert db.execute(query,args).fetchone()==(False,)
        args[0]=uuid4()
        assert db.execute(query,args).fetchone()==(False,)
