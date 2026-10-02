from uuid import uuid4
from psycopg.types.json import Jsonb


def setup(db,permissions):
    user,tenant,project,collection,share,session=uuid4(),uuid4(),uuid4(),uuid4(),uuid4(),uuid4()
    session_hash,csrf_hash=uuid4().bytes+uuid4().bytes,uuid4().bytes+uuid4().bytes
    db.execute('INSERT INTO photo.users(id,email_normalized) VALUES(%s,%s)',(user,f'{user}@example.test'))
    db.execute("INSERT INTO photo.tenants(id,name,owner_user_id,quotas,retention) VALUES(%s,'T',%s,'{}','{}')",(tenant,user))
    db.execute("INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES(%s,%s,'owner')",(tenant,user))
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)',(tenant,))
    db.execute("INSERT INTO photo.projects(id,tenant_id,name,download_allowed) VALUES(%s,%s,'P',true)",(project,tenant))
    db.execute("INSERT INTO photo.collections(id,tenant_id,project_id,name,state) VALUES(%s,%s,%s,'C','published')",(collection,tenant,project))
    preset=db.execute("SELECT id FROM photo.export_presets WHERE tenant_id=%s AND name='Web'",(tenant,)).fetchone()[0]
    db.execute("INSERT INTO photo.share_links(id,tenant_id,collection_id,secret_hash,permissions,limits,allowed_presets,created_by) VALUES(%s,%s,%s,%s,%s,%s,%s,%s)",(share,tenant,collection,b'x'*32,Jsonb(permissions),Jsonb({'max_upload_files':10,'max_upload_bytes_total':1000000,'max_file_bytes':100000,'allowed_formats':['png']}),Jsonb([str(preset)]),user))
    db.execute("INSERT INTO photo.share_sessions(id,tenant_id,share_id,collection_id,session_hash,link_version,expires_at,locale,csrf_hash) VALUES(%s,%s,%s,%s,%s,1,clock_timestamp()+interval '1 hour','en',%s)",(session,tenant,share,collection,session_hash,csrf_hash))
    return tenant,collection,share,session_hash,csrf_hash


def test_permission_matrix_and_blind_all_snapshot(db):
    for permissions in (
      {'view':True,'download':False,'upload':False},
      {'view':False,'download':True,'upload':False},
      {'view':False,'download':False,'upload':True},
      {'view':True,'download':True,'upload':True},
    ):
        _,collection,_,session_hash,csrf_hash=setup(db,permissions)
        row=db.execute('SELECT capabilities,sections,cover,empty,revision FROM photo.get_portal_collection(%s)',(session_hash,)).fetchone()
        assert row is not None
        caps,sections,cover,empty,revision=row
        assert caps==permissions and sections==[] and cover is None and empty is True
        for permission in ('view','download','upload'):
            allowed=db.execute('SELECT count(*) FROM photo.authorize_portal_session(%s,%s,%s)',(session_hash,csrf_hash,permission)).fetchone()[0]
            assert bool(allowed) is permissions[permission]
        if permissions['download'] and not permissions['view']:
            outcome=db.execute("SELECT outcome FROM photo.create_portal_selection(%s,%s,'blind',%s,%s,%s,'all',NULL,NULL,clock_timestamp()+interval '1 hour')",(session_hash,csrf_hash,"a"*64,uuid4(),revision)).fetchone()[0]
            assert outcome=='empty'
            selected=db.execute("SELECT outcome FROM photo.create_portal_selection(%s,%s,'selected',%s,%s,%s,'selected',%s,NULL,clock_timestamp()+interval '1 hour')",(session_hash,csrf_hash,"b"*64,uuid4(),revision,[uuid4()])).fetchone()[0]
            assert selected=='unauthorized'


def test_policy_change_revokes_every_permission_and_portal_has_no_table_access(db):
    tenant,_,share,session_hash,csrf_hash=setup(db,{'view':True,'download':True,'upload':True})
    db.execute('UPDATE photo.share_links SET policy_version=policy_version+1 WHERE tenant_id=%s AND id=%s',(tenant,share))
    for permission in ('session','view','download','upload'):
        assert db.execute('SELECT count(*) FROM photo.authorize_portal_session(%s,%s,%s)',(session_hash,csrf_hash,permission)).fetchone()[0]==0
    db.execute('SET LOCAL ROLE photo_portal')
    try:
        db.execute('SELECT * FROM photo.collection_items').fetchall()
        raise AssertionError('photo_portal read business table')
    except Exception as exc:
        assert getattr(exc,'sqlstate',None)=='42501'


def test_revocation_after_multipart_prevents_complete_and_keeps_staging_unadopted(db):
    tenant,_,share,session_hash,csrf_hash=setup(db,{'view':False,'download':False,'upload':True})
    upload,reservation=uuid4(),uuid4()
    debug=db.execute("SELECT l.limits,l.permissions,l.policy_version,s.link_version,s.revoked_at,l.revoked_at,c.state,s.csrf_hash=%s FROM photo.share_links l JOIN photo.share_sessions s ON s.tenant_id=l.tenant_id AND s.share_id=l.id JOIN photo.collections c ON c.tenant_id=l.tenant_id AND c.id=l.collection_id WHERE l.id=%s",(csrf_hash,share)).fetchone()
    assert debug[0]['allowed_formats']==['png'] and debug[1]['upload'] is True and debug[2:8]==(1,1,None,None,'published',True),debug
    checks=db.execute("SELECT jsonb_typeof(l.limits),jsonb_typeof(l.limits->'max_upload_files'),jsonb_typeof(l.limits->'max_upload_bytes_total'),jsonb_typeof(l.limits->'max_file_bytes'),jsonb_typeof(l.limits->'allowed_formats'),l.limits->'allowed_formats' ? 'png',l.collection_id=s.collection_id,l.policy_version=s.link_version,l.permissions->'upload'='true'::jsonb FROM photo.share_links l JOIN photo.share_sessions s ON s.tenant_id=l.tenant_id AND s.share_id=l.id WHERE l.id=%s",(share,)).fetchone()
    assert checks==('object','number','number','number','array',True,True,True,True),checks
    row=db.execute("SELECT outcome,id FROM photo.begin_portal_upload(%s,%s,'begin',%s,%s,%s,'staging/object','guest.png',100,'png','image/png',NULL,%s,clock_timestamp()+interval '1 hour')",(session_hash,csrf_hash,'c'*64,upload,reservation,'d'*64)).fetchone()
    assert row==('created',upload)
    assert db.execute('SELECT photo.attach_portal_multipart(%s,%s,%s,%s)',(session_hash,csrf_hash,upload,'multipart-1')).fetchone()[0] is True
    db.execute('UPDATE photo.share_links SET revoked_at=clock_timestamp(),policy_version=policy_version+1 WHERE tenant_id=%s AND id=%s',(tenant,share))
    db.execute('UPDATE photo.share_sessions SET revoked_at=clock_timestamp() WHERE session_hash=%s',(session_hash,))
    assert db.execute('SELECT count(*) FROM photo.get_portal_upload(%s,%s,false)',(session_hash,upload)).fetchone()[0]==0
    outcome=db.execute("SELECT outcome FROM photo.complete_portal_upload(%s,%s,'finish',%s,%s,100,%s,%s,%s,%s)",(session_hash,csrf_hash,'e'*64,upload,Jsonb([{'part_no':1,'etag':'etag-1'}]),uuid4(),uuid4(),uuid4())).fetchone()[0]
    assert outcome=='unauthorized'
    state,multipart,staging=db.execute('SELECT state,multipart_id,staging_artifact_id FROM photo.upload_sessions WHERE tenant_id=%s AND id=%s',(tenant,upload)).fetchone()
    assert (state,multipart,staging)==('uploading','multipart-1',None)
    assert db.execute('SELECT count(*) FROM photo.upload_parts WHERE tenant_id=%s AND upload_session_id=%s',(tenant,upload)).fetchone()[0]==0
