from uuid import uuid4


def test_full_render_registration_schema_and_unscoped_denial(db):
    import json
    from pathlib import Path
    registry=json.loads((Path(__file__).resolve().parents[2]/'docs/implementation-v2/contracts/operations.json').read_text())
    operation=next(item for item in registry['operations'] if item['id']=='render.compose')
    assert operation['params']=='FrozenRenderRecipe' and operation['output']=='ImageResult'
    policies=db.execute("SELECT role,media_types,max_bytes,max_count FROM photo.operation_output_policies WHERE operation='render.compose' AND version='1'").fetchall()
    assert {role for role,*_ in policies}==set(operation['role_policies'])
    for role,media,bytecap,count in policies:
        policy=operation['role_policies'][role]
        assert (media,bytecap,count)==(policy['media_types'],policy['max_bytes'],policy['max_count'])
    assert db.execute("SELECT resource_class,timeout_seconds,v2_enabled FROM photo.operation_registry WHERE operation='render.compose' AND version='1'").fetchone()==('gpu',operation['timeout_seconds'],False)
    for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway','photo_maintenance'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v26_render_background_binding_ready(uuid,uuid,jsonb)','EXECUTE')",(role,)).fetchone()[0]
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v26_render_recipe_scope_before_background(uuid,uuid,uuid,uuid,text,uuid,uuid)','EXECUTE')",(role,)).fetchone()[0]
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
    for private in ('photo.v26_render_adoption_inputs_ready(uuid,uuid)',
                    'photo.adopt_v2_plan_pre_render_qualification(uuid,uuid,integer,bigint,uuid,jsonb,text)',
                    'photo.v26_render_live_qualification_ready(uuid,uuid)',
                    'photo.admit_v26_render_recipe_before_live_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'):
        for role in ('photo_worker','photo_api','photo_scheduler','photo_gateway','photo_maintenance'):
            assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,private)).fetchone()[0]
    cache='photo.publish_v26_render_cache(uuid,uuid,integer,bigint,uuid,text,text,jsonb)'
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(cache,)).fetchone()[0]
    for role in ('photo_api','photo_worker','photo_gateway','photo_maintenance'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,cache)).fetchone()[0]
    lookup='photo.lookup_v26_render_cache(uuid,uuid,integer,bigint,text,text)'
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(lookup,)).fetchone()[0]
    for role in ('photo_api','photo_worker','photo_gateway','photo_maintenance'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,lookup)).fetchone()[0]
    manifest='photo.v26_render_cache_artifacts(uuid,uuid,integer,bigint,text,text)'
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(manifest,)).fetchone()[0]
    for role in ('photo_api','photo_worker','photo_gateway','photo_maintenance'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,manifest)).fetchone()[0]
    for cache_worker in ('photo.v26_render_worker_cache_context(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',
                         'photo.lookup_v26_render_worker_cache(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text,text)'):
        assert db.execute("SELECT has_function_privilege('photo_worker',%s,'EXECUTE')",(cache_worker,)).fetchone()[0]
        for role in ('photo_api','photo_scheduler','photo_gateway','photo_maintenance'):
            assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,cache_worker)).fetchone()[0]
    worker_manifest='photo.v26_render_worker_cache_artifacts(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text,text)'
    assert db.execute("SELECT has_function_privilege('photo_worker',%s,'EXECUTE')",(worker_manifest,)).fetchone()[0]
    for role in ('photo_api','photo_scheduler','photo_gateway','photo_maintenance'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,worker_manifest)).fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_gateway','photo.gateway_input_before_render_cache(uuid,integer,bigint,uuid,text,uuid)','EXECUTE')").fetchone()[0]
    quarantine='photo.quarantine_v26_render_worker_cache(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text,text,uuid)'
    assert db.execute("SELECT has_function_privilege('photo_worker',%s,'EXECUTE')",(quarantine,)).fetchone()[0]
    for role in ('photo_api','photo_scheduler','photo_gateway','photo_maintenance'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,quarantine)).fetchone()[0]
    worker_pin='photo.v26_render_worker_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'
    assert db.execute("SELECT has_function_privilege('photo_worker',%s,'EXECUTE')",(worker_pin,)).fetchone()[0]
    for role in ('photo_api','photo_scheduler','photo_gateway','photo_maintenance'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,worker_pin)).fetchone()[0]
    qualification='photo.v26_render_adoption_qualification(uuid,uuid,integer,bigint,uuid)'
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(qualification,)).fetchone()[0]
    for role in ('photo_api','photo_worker','photo_gateway','photo_maintenance'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,qualification)).fetchone()[0]
    assert db.execute('SELECT photo.v26_render_adoption_qualification(%s,%s,1,1,%s)',(uuid4(),uuid4(),uuid4())).fetchone()==(None,)
    replay='photo.v26_render_adoption_inputs(uuid,uuid,integer,bigint,uuid)'
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(replay,)).fetchone()[0]
    for role in ('photo_api','photo_worker','photo_gateway','photo_maintenance'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,replay)).fetchone()[0]
    assert db.execute('SELECT * FROM photo.v26_render_adoption_inputs(%s,%s,1,1,%s)',(uuid4(),uuid4(),uuid4())).fetchone() is None
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



def test_background_recipe_registration_and_live_expiry(db):
    import json
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    from layer_original_fixture import seed_original_composition
    case=seed_original_composition(db);binding=json.loads(case['Params'])['base']
    tid,project,actor,session=case['Tenant'],case['Project'],case['Actor'],case['Session']
    def artifact(body,media,role):
        digest=sha256(body).hexdigest()
        ident=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,'test',%s,%s,%s,%s,'v1',now(),%s) RETURNING id",(tid,str(uuid4()),digest,len(body),media,role)).fetchone()[0]
        return dict(artifact_id=str(ident),sha256=digest)
    config={'extensions':{'background':{'mode':'asset','asset_id':'studio'}}}
    style_ref=artifact(canonical_bytes(config),'application/json','style_candidate')
    source_ref=artifact(b'controlled imported file','image/png','style_asset')
    style=db.execute("INSERT INTO photo.styles(tenant_id,project_id,name,created_by) VALUES(%s,%s,'Background',%s) RETURNING id",(tid,project,actor)).fetchone()[0]
    version=db.execute("INSERT INTO photo.style_versions(tenant_id,style_id,version_no,config_json,config_hash,schema_version,renderer_version,approval_status,metrics,export_artifact_id) VALUES(%s,%s,1,%s,%s,'2.0.0','photo_renderer_v2','accepted','{}',%s) RETURNING id",(tid,style,json.dumps(config),style_ref['sha256'],style_ref['artifact_id'])).fetchone()[0]
    db.execute("INSERT INTO photo.style_asset_bindings(tenant_id,style_version_id,portable_asset_id,artifact_id,sha256) VALUES(%s,%s,'studio',%s,%s)",(tid,version,source_ref['artifact_id'],source_ref['sha256']))
    pin=dict(schema_version=1,portable_asset_id='studio',style_version_id=str(version),style=style_ref,source=source_ref,expected_bytes=24,media_type='image/png',suffix='.png',assume_profile=None,work_icc_sha='c'*64)
    pin['expected_bytes']=len(b'controlled imported file')
    recipe=dict(schema_version=3,source=binding,base_style=style_ref,session_corrections=None,learning_version_id=None,overrides=[],layer_stack=None,renderer_version='v2.2',parameter_delta=dict(ev=0,log_r=0,log_g=0,log_b=0,saturation_delta=0,retouch_delta=0))
    body=canonical_bytes(dict(schema_version=1,recipe=recipe,recipe_sha256=sha256(canonical_bytes(recipe)).hexdigest(),scalars=dict(schema_version=1,source=binding,base_style=style_ref,effective_delta=recipe['parameter_delta'],renderer_version='v2.2',icc_lock_sha='c'*64,model_lock_sha='d'*64),learning=None,background_asset=pin)).decode()
    digest=sha256(body.encode()).hexdigest();document=uuid4()
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING',(tid,))
    db.execute("SELECT set_config('app.tenant_id',%s,true)",(tid,));db.execute('SET LOCAL ROLE photo_api')
    args=(tid,binding['asset_version_id'],document,actor,session,digest,len(body.encode()),'test')
    reserved=db.execute('SELECT * FROM photo.reserve_v2_draft_upload('+','.join(['%s']*8)+')',args).fetchone()
    complete=(*args[:5],reserved[1],reserved[2],digest,args[6],args[7],'background-v1')
    assert db.execute('SELECT photo.complete_v2_draft_upload('+','.join(['%s']*11)+')',complete).fetchone()==(True,)
    registration=(*args[:5],reserved[1],digest,binding['source']['artifact_id'],binding['source']['sha256'],body)
    register='SELECT photo.register_v26_render_recipe('+','.join(['%s']*10)+')'
    assert db.execute(register,registration).fetchone()==(True,)
    assert db.execute(register,registration).fetchone()==(True,)
    scope_args=(tid,project,binding['asset_version_id'],reserved[1],digest,actor,session)
    scope='SELECT photo.v26_render_recipe_scope('+','.join(['%s']*7)+')'
    assert db.execute(scope,scope_args).fetchone()==(True,)
    for field,value in [('expected_bytes',999),('work_icc_sha','e'*64),('portable_asset_id','different')]:
        changed=json.loads(body);changed['background_asset'][field]=value
        changed_body=canonical_bytes(changed).decode();changed_sha=sha256(changed_body.encode()).hexdigest()
        changed_args=(tid,binding['asset_version_id'],uuid4(),actor,session,changed_sha,len(changed_body.encode()),'test')
        changed_reserved=db.execute('SELECT * FROM photo.reserve_v2_draft_upload('+','.join(['%s']*8)+')',changed_args).fetchone()
        completion=(*changed_args[:5],changed_reserved[1],changed_reserved[2],changed_sha,changed_args[6],changed_args[7],'invalid-background-v1')
        assert db.execute('SELECT photo.complete_v2_draft_upload('+','.join(['%s']*11)+')',completion).fetchone()==(True,)
        invalid=(*changed_args[:5],changed_reserved[1],changed_sha,binding['source']['artifact_id'],binding['source']['sha256'],changed_body)
        assert db.execute(register,invalid).fetchone()==(False,)
    db.execute('RESET ROLE')
    stored=db.execute('SELECT render_background_asset FROM photo.v2_draft_uploads WHERE tenant_id=%s AND document_id=%s',(tid,document)).fetchone()[0]
    assert stored==pin
    from psycopg.types.json import Jsonb
    run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,'v2','draft',%s,%s,%s,%s,'fail_fast') RETURNING id",(tid,case['Snapshot'],Jsonb({'project_id':str(project)}),'a'*64,actor)).fetchone()[0]
    db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'preview','v2.2',%s)",(tid,project,run,case['Snapshot'],'a'*64))
    task=db.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,'render.compose:0','render.compose','1','gpu','blocked','[]',%s,%s,'{}') RETURNING id",(tid,run,reserved[1],'b'*64)).fetchone()[0]
    query='SELECT photo.v26_render_direct_inputs_ready('+','.join(['%s']*6)+')'
    def direct(value=None):
        return db.execute(query,(tid,task,project,actor,session,Jsonb(json.loads(body) if value is None else value))).fetchone()[0]
    db.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,ordinal,contract) VALUES(%s,%s,%s,%s,%s,0,'StyleV2')",(tid,project,task,style_ref['artifact_id'],style_ref['sha256']))
    assert direct() is False
    db.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,ordinal,contract) VALUES(%s,%s,%s,%s,%s,1,'StyleBackgroundAsset')",(tid,project,task,source_ref['artifact_id'],source_ref['sha256']))
    assert direct() is True
    altered=json.loads(body);del altered['background_asset']
    assert direct(altered) is False
    altered=json.loads(body);altered['background_asset']['expected_bytes']+=1
    assert direct(altered) is False
    with db.transaction(force_rollback=True):
        db.execute("DELETE FROM photo.v2_task_inputs WHERE task_id=%s AND ordinal=1",(task,))
        db.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,ordinal,contract) VALUES(%s,%s,%s,%s,%s,1,'Artifact')",(tid,project,task,source_ref['artifact_id'],source_ref['sha256']))
        assert direct() is False
    for fn in ('v26_render_direct_inputs_ready','v26_render_direct_inputs_before_background'):
        assert db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')",(f'photo.{fn}(uuid,uuid,uuid,uuid,uuid,jsonb)',)).fetchone()==(False,)
    db.execute("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE tenant_id=%s AND id=%s",(tid,source_ref['artifact_id']))
    assert direct() is False
    db.execute('SET LOCAL ROLE photo_api')
    assert db.execute(scope,scope_args).fetchone()==(False,)
    assert db.execute(register,registration).fetchone()==(False,)
