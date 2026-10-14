"""Metadata document storage boundaries; authored, NOT RUN."""
from uuid import uuid4
import pytest

SIGNATURES=[
 'photo.reserve_v30_metadata_document(uuid,uuid,uuid,text,text,text)',
 'photo.complete_v30_metadata_document(uuid,uuid,uuid,text,text,text,uuid,text)',
]

@pytest.mark.parametrize('signature',SIGNATURES)
@pytest.mark.parametrize('role,allowed',[('photo_scheduler',True),('photo_api',False),('photo_worker',False),('photo_gateway',False),('photo_portal',False)])
def test_metadata_document_storage_acl(db,signature,role,allowed):
    assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()==(allowed,)


def test_metadata_document_reservation_missing_proof_refuses(db):
    with db.transaction():
        tenant=uuid4()
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        db.execute('SET LOCAL ROLE photo_scheduler')
        row=db.execute('SELECT * FROM photo.reserve_v30_metadata_document(%s,%s,%s,%s,%s,%s)',
            (tenant,uuid4(),uuid4(),'v30_metadata_projection','{}','test')).fetchone()
        assert row[0] in ('stale','forbidden') and row[1:]==(None,None,False)


@pytest.mark.parametrize('version',['','null','line\nversion','line\rversion','x'*1025])
def test_metadata_document_completion_invalid_version(db,version):
    with db.transaction():
        tenant=uuid4()
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.complete_v30_metadata_document(%s,%s,%s,%s,%s,%s,%s,%s)',
            (tenant,uuid4(),uuid4(),'task_params','{}','test',uuid4(),version)).fetchone()==(False,)
