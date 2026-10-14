"""Delivery original provenance boundary; authored, NOT RUN."""
from uuid import uuid4
import pytest

@pytest.mark.parametrize('role,allowed',[('photo_api',True),('photo_worker',False),('photo_scheduler',False),('photo_gateway',False),('photo_portal',False)])
def test_delivery_metadata_original_acl(db,role,allowed):
    assert db.execute("SELECT has_function_privilege(%s,'photo.v30_delivery_metadata_original(uuid,uuid,uuid,uuid,text,uuid,uuid)','EXECUTE')",(role,)).fetchone()==(allowed,)


def test_delivery_metadata_original_missing_scope_refuses(db):
    assert db.execute('SELECT * FROM photo.v30_delivery_metadata_original(%s,%s,%s,%s,%s,%s,%s)',(uuid4(),uuid4(),uuid4(),uuid4(),'a'*64,uuid4(),uuid4())).fetchall()==[]


def test_delivery_original_positive_and_live_authority(db):
    from test_database import tenant
    with db.transaction():
        tid,actor=tenant(db,str(uuid4())+'@example.test')
        project=db.execute("INSERT INTO photo.projects(tenant_id,name,download_allowed) VALUES(%s,'Delivery metadata',true) RETURNING id",(tid,)).fetchone()[0]
        session=db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') RETURNING id",(actor,uuid4().bytes)).fetchone()[0]
        def artifact(sha,media,role=None):
            return db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,'test',%s,%s,100,%s,'v1',clock_timestamp(),%s) RETURNING id",(tid,str(uuid4()),sha,media,role)).fetchone()[0]
        original=artifact('a'*64,'image/jpeg')
        source=artifact('b'*64,'application/json','working_image')
        asset=db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tid,project,original)).fetchone()[0]
        version=db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",(tid,asset,source,'b'*64)).fetchone()[0]
        query='SELECT artifact_id,sha256,bytes,bucket,object_version,media_type FROM photo.v30_delivery_metadata_original(%s,%s,%s,%s,%s,%s,%s)'
        args=(tid,project,version,source,'b'*64,actor,session)
        def read():
            db.execute('SET LOCAL ROLE photo_api')
            try:return db.execute(query,args).fetchall()
            finally:db.execute('RESET ROLE')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tid),))
        expected=[(original,'a'*64,100,'test','v1','image/jpeg')]
        assert read()==expected
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(uuid4()),))
        assert read()==[]
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tid),))
        assert read()==expected
        for statement,reset,identity in [
            ('UPDATE photo.projects SET download_allowed=false WHERE id=%s','UPDATE photo.projects SET download_allowed=true WHERE id=%s',project),
            ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",'UPDATE photo.artifacts SET expires_at=NULL WHERE id=%s',original),
            ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s','UPDATE photo.artifacts SET verified_at=clock_timestamp() WHERE id=%s',original),
            ("UPDATE photo.artifacts SET object_version='null' WHERE id=%s","UPDATE photo.artifacts SET object_version='v1' WHERE id=%s",original),
            ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s','UPDATE photo.sessions SET revoked_at=NULL WHERE id=%s',session),
        ]:
            db.execute(statement,(identity,));assert read()==[]
            db.execute(reset,(identity,));assert read()==expected
