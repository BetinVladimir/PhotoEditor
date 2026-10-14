"""Metadata source reservation/completion ACL and refusal tests; NOT RUN."""
from uuid import uuid4
import pytest


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal'])
@pytest.mark.parametrize('signature',[
 'photo.reserve_v30_metadata_source(uuid,uuid,uuid,uuid,text,text,text,text[],text)',
 'photo.complete_v30_metadata_source(uuid,uuid,uuid,uuid,text,text,text,text[],text,uuid,text)'])
def test_source_publication_api_only(db,role,signature):
    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()==(role=='photo_api',)


def test_source_publication_missing_authority_refused(db):
    tenant,run,actor,session=[uuid4() for _ in range(4)]
    args=[tenant,run,actor,session,'{}','0'*64,'copyright',[],'photos']
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        row=db.execute('SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_metadata_source(%s,%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()
        assert row==('stale',None,None,False)
        assert db.execute('SELECT photo.complete_v30_metadata_source(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',args+[uuid4(),'v1']).fetchone()==(False,)
