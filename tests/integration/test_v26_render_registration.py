from uuid import uuid4


def test_full_render_registration_schema_and_unscoped_denial(db):
    signature='photo.register_v26_render_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text)'
    row=db.execute('SELECT prosecdef,pg_get_userbyid(proowner),proconfig FROM pg_proc WHERE oid=%s::regprocedure',(signature,)).fetchone()
    assert row[0] and row[1]=='photo_fn_owner' and 'search_path=pg_catalog, photo' in row[2]
    assert db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')",(signature,)).fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_worker',%s,'EXECUTE')",(signature,)).fetchone()[0]
    stage='photo.stage_v26_render_recipe_task(uuid,uuid,uuid,integer,uuid,text,text,jsonb)'
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(stage,)).fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_worker',%s,'EXECUTE')",(stage,)).fetchone()[0]
    assert db.execute("SELECT photo.stage_v26_render_recipe_task(%s,%s,%s,0,%s,%s,%s,'{}'::jsonb)",(uuid4(),uuid4(),uuid4(),uuid4(),'a'*64,'b'*64)).fetchone()==(False,)
    begin='photo.begin_v26_render_draft(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,bigint)'
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(begin,)).fetchone()[0]
    for role in ('photo_api','photo_worker'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,begin)).fetchone()[0]
    admission='photo.admit_v26_render_recipe(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'
    assert db.execute("SELECT has_function_privilege('photo_worker',%s,'EXECUTE')",(admission,)).fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')",(admission,)).fetchone()[0]
    assert db.execute('SELECT photo.admit_v26_render_recipe(%s,%s,%s,1,1,%s,%s,%s,%s,%s)',(uuid4(),uuid4(),uuid4(),uuid4(),uuid4(),'a'*64,'{}','b'*64)).fetchone()==(False,)
    recovery='photo.v26_render_draft_state(uuid,uuid,uuid,uuid)'
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(recovery,)).fetchone()[0]
    for role in ('photo_api','photo_worker'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,recovery)).fetchone()[0]
    columns=db.execute("SELECT column_name FROM information_schema.columns WHERE table_schema='photo' AND table_name='v2_draft_uploads'").fetchall()
    assert {'render_source_id','render_source_sha','render_registered_at'}<=set(c[0] for c in columns)
    args=(uuid4(),uuid4(),uuid4(),uuid4(),uuid4(),uuid4(),'a'*64,uuid4(),'b'*64,'{}')
    assert db.execute('SELECT photo.register_v26_render_recipe(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()==(False,)


def test_full_render_registration_ready_upload_replay_and_revocation(db):
    import json
    from hashlib import sha256
    from layer_original_fixture import seed_original_composition
    case=seed_original_composition(db);binding=json.loads(case['Params'])['base']
    version=binding['asset_version_id'];source=binding['source']
    body=json.dumps({'schema_version':1,'recipe':{'schema_version':3,'source':binding},
        'recipe_sha256':'c'*64,'scalars':{'source':binding},'learning':None},sort_keys=True,separators=(',',':'))
    digest=sha256(body.encode()).hexdigest();document=uuid4()
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING',(case['Tenant'],))
    db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],));db.execute('SET LOCAL ROLE photo_api')
    args=(case['Tenant'],version,document,case['Actor'],case['Session'],digest,len(body.encode()),'test')
    reserved=db.execute('SELECT * FROM photo.reserve_v2_draft_upload(%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()
    assert reserved[0]=='created'
    register='SELECT photo.register_v26_render_recipe('+','.join(['%s']*10)+')'
    registration=(*args[:5],reserved[1],digest,source['artifact_id'],source['sha256'],body)
    assert db.execute(register,registration).fetchone()==(False,)
    completion=(*args[:5],reserved[1],reserved[2],digest,args[6],args[7],'registered-v1')
    assert db.execute('SELECT photo.complete_v2_draft_upload('+','.join(['%s']*11)+')',completion).fetchone()==(True,)
    db.execute('RESET ROLE')
    project=db.execute('SELECT project_id FROM photo.v2_draft_uploads WHERE tenant_id=%s AND document_id=%s',(case['Tenant'],document)).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_api')
    scope='SELECT photo.v26_render_recipe_scope(%s,%s,%s,%s,%s,%s,%s)'
    scope_args=(case['Tenant'],project,version,reserved[1],digest,case['Actor'],case['Session'])
    assert db.execute(scope,scope_args).fetchone()==(False,)
    assert db.execute(register,registration).fetchone()==(True,)
    assert db.execute(scope,scope_args).fetchone()==(True,)
    assert db.execute('SELECT photo.v26_render_model_scope(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',(case['Tenant'],project,version,reserved[1],digest,uuid4(),'e'*64,uuid4(),case['Actor'],case['Session'])).fetchone()==(False,)
    assert db.execute('SELECT photo.v26_render_descriptor_scope('+','.join(['%s']*12)+')',(case['Tenant'],project,version,reserved[1],digest,uuid4(),'e'*64,uuid4(),0,'f'*64,case['Actor'],case['Session'])).fetchone()==(False,)
    wrong=list(scope_args);wrong[2]=uuid4()
    assert db.execute(scope,wrong).fetchone()==(False,)
    assert db.execute(register,registration).fetchone()==(True,)
    for index,replacement in ((0,uuid4()),(1,uuid4()),(4,'d'*64),(5,uuid4()),(6,uuid4())):
        wrong=list(scope_args);wrong[index]=replacement
        assert db.execute(scope,wrong).fetchone()==(False,)
    for artifact in (reserved[1],source['artifact_id']):
        with db.transaction(force_rollback=True):
            db.execute('RESET ROLE')
            db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND id=%s",(case['Tenant'],artifact))
            db.execute('SET LOCAL ROLE photo_api')
            assert db.execute(scope,scope_args).fetchone()==(False,)
        assert db.execute(scope,scope_args).fetchone()==(True,)
    changed=list(registration);changed[-1]=body+' '
    assert db.execute(register,changed).fetchone()==(False,)
    db.execute('RESET ROLE')
    row=db.execute('SELECT render_source_id,render_source_sha,render_registered_at IS NOT NULL FROM photo.v2_draft_uploads WHERE tenant_id=%s AND document_id=%s',(case['Tenant'],document)).fetchone()
    assert str(row[0])==source['artifact_id'] and row[1:]==(source['sha256'],True)
    assert db.execute('SELECT render_authored_sha FROM photo.v2_draft_uploads WHERE tenant_id=%s AND document_id=%s',(case['Tenant'],document)).fetchone()==('c'*64,)
    db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(case['Session'],))
    db.execute('SET LOCAL ROLE photo_api')
    assert db.execute(register,registration).fetchone()==(False,)
    assert db.execute(scope,scope_args).fetchone()==(False,)

