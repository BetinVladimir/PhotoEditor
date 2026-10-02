"""Catalog provenance and session authority for original lens EXIF inputs."""
from uuid import uuid4, uuid5
import json
from test_database import tenant


def test_lens_original_is_exact_version_project_and_live_session(db):
    tid,actor=tenant(db,str(uuid4())+'@example.test')
    project=db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Lens') RETURNING id",(tid,)).fetchone()[0]
    session=db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') RETURNING id",(actor,uuid4().bytes)).fetchone()[0]
    def artifact(sha,media,role=None):
        return db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,'test',%s,%s,100,%s,'v1',clock_timestamp(),%s) RETURNING id",(tid,str(uuid4()),sha,media,role)).fetchone()[0]
    original=artifact('a'*64,'image/jpeg')
    source=artifact('b'*64,'application/json','working_image')
    asset=db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tid,project,original)).fetchone()[0]
    version=db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",(tid,asset,source,'b'*64)).fetchone()[0]
    other=artifact('b'*64,'application/json','working_image')
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES(%s,'v2','user',%s,%s,1,clock_timestamp()+interval '1 hour') RETURNING id",(tid,actor,'d'*64)).fetchone()[0]
    db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,0,%s,%s,%s,'{}')",(tid,snapshot,version,source,'b'*64))
    db.execute('UPDATE photo.snapshots SET expires_at=NULL WHERE id=%s',(snapshot,))
    preflight='SELECT photo.v25_lens_snapshot_ready(%s,%s,%s,%s,%s)'
    selection=(tid,project,snapshot,actor,session)
    db.execute('SET LOCAL ROLE photo_api')
    query='SELECT artifact_id,sha256 FROM photo.v25_lens_original_source(%s,%s,%s,%s,%s,%s,%s)'
    args=(tid,project,version,source,'b'*64,actor,session)
    assert db.execute(query,args).fetchall()==[(original,'a'*64)]
    profile_query='SELECT bucket,object_version,bytes FROM photo.v25_lens_profile_object(%s,%s,%s,%s,%s,%s)'
    profile_args=(tid,project,actor,session,source,'b'*64)
    assert db.execute(profile_query,profile_args).fetchall()==[('test','v1',100)]
    assert db.execute(profile_query,profile_args[:-1]+('c'*64,)).fetchall()==[]
    for role in ['photo_worker','photo_gateway','photo_scheduler']:
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v25_lens_profile_object(uuid,uuid,uuid,uuid,uuid,text)','EXECUTE')",(role,)).fetchone()[0]

    assert db.execute(preflight,selection).fetchone()[0]
    selection_query='SELECT ordinal,asset_version_id,source_id,source_sha,original_id,original_sha,item_count FROM photo.v25_lens_selection_sources(%s,%s,%s,%s,%s)'
    assert db.execute(selection_query,selection).fetchall()==[(0,version,source,'b'*64,original,'a'*64,1)]
    assert db.execute(selection_query,(tid,uuid4(),snapshot,actor,session)).fetchall()==[]
    for role in ['photo_worker','photo_gateway','photo_scheduler']:
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v25_lens_selection_sources(uuid,uuid,uuid,uuid,uuid)','EXECUTE')",(role,)).fetchone()[0]
    assert not db.execute(preflight,(tid,uuid4(),snapshot,actor,session)).fetchone()[0]
    for changed in [(tid,uuid4(),version,source,'b'*64,actor,session),(tid,project,version,other,'b'*64,actor,session),(tid,project,version,source,'c'*64,actor,session)]:
        assert db.execute(query,changed).fetchall()==[]
    db.execute('RESET ROLE')
    # Synthetic admission plan tests quota and identity only, not lens quality.
    db.execute("INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)",(tid,))
    run=uuid4()
    binding={'asset_version_id':str(version),'source':{'artifact_id':str(source),'sha256':'b'*64},'canvas_hash':'f'*64}
    plan={'params_sha':'b'*64,'task_id':str(uuid5(uuid5(run,'workflow'),'optical.lens:0')),'params':{'base':binding},'original':{'binding':binding,'original':{'artifact_id':str(original),'sha256':'a'*64}},'profile':{'backend':'cpu','renderer_version':'v2.2','resource_estimate':{'output_bytes':1024}}}
    config=json.dumps({'project_id':str(project),'input_snapshot_id':str(snapshot),'v25_lens':{'auth_session_id':str(session),'nodes':[plan]}},sort_keys=True,separators=(',',':'))
    begin='SELECT photo.begin_v25_lens_draft(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)'
    admission=(tid,project,snapshot,actor,session,run,uuid5(run,'workflow'),config,'e'*64,1024)
    assert not db.execute("SELECT has_function_privilege('photo_api','photo.begin_v25_lens_draft(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,bigint)','EXECUTE')").fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_scheduler','photo.v25_lens_plan_matches_snapshot(uuid,uuid,uuid,jsonb)','EXECUTE')").fetchone()[0]
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute(begin,admission).fetchone()[0]
    assert db.execute(begin,admission).fetchone()[0]
    assert not db.execute(begin,admission[:-1]+(1025,)).fetchone()[0]
    for corrupt in (config.replace(str(version),str(uuid4())),config.replace(str(source),str(other)),config.replace(str(original),str(other)),config.replace('a'*64,'c'*64),config.replace(str(uuid5(uuid5(run,'workflow'),'optical.lens:0')),str(uuid4()))):
        trial=uuid4()
        corrupt=corrupt.replace(str(uuid5(uuid5(run,'workflow'),'optical.lens:0')),str(uuid5(uuid5(trial,'workflow'),'optical.lens:0')))
        assert not db.execute(begin,(tid,project,snapshot,actor,session,trial,uuid5(trial,'workflow'),corrupt,'e'*64,1024)).fetchone()[0]
    assert not db.execute(begin,admission[:6]+(uuid4(),)+admission[7:]).fetchone()[0]
    second=uuid4()
    too_large=config.replace('1024','10000000').replace(str(uuid5(uuid5(run,'workflow'),'optical.lens:0')),str(uuid5(uuid5(second,'workflow'),'optical.lens:0')))
    assert not db.execute(begin,(tid,project,snapshot,actor,session,second,uuid5(second,'workflow'),too_large,'e'*64,10000000)).fetchone()[0]
    db.execute('RESET ROLE')
    assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()[0]==1024
    assert db.execute('SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=%s AND holder_id=%s',(tid,run)).fetchone()[0]==1
    # Synthetic blocked shell isolates the activation authority guard; it has
    # no input closure and cannot activate a real operation.
    from psycopg.types.json import Jsonb
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,'optical.lens:0','optical.lens','1','cpu','[]',%s,%s,%s)",(uuid5(uuid5(run,'workflow'),'optical.lens:0'),tid,run,source,'d'*64,Jsonb(plan['profile'])))
    def activation():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        ready=db.execute('SELECT photo.v25_lens_activation_ready(%s,%s)',(tid,run)).fetchone()[0]
        db.execute('RESET ROLE')
        return ready
    assert activation()
    import pytest
    from psycopg import errors
    assert not db.execute("SELECT has_function_privilege('photo_scheduler','photo.adopt_v2_plan_v25_core(uuid,uuid,integer,bigint,uuid,jsonb,text)','EXECUTE')").fetchone()[0]
    adopt='SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,%s,%s)'
    adopt_args=(tid,uuid5(uuid5(run,'workflow'),'optical.lens:0'),uuid4(),Jsonb([]),'e'*64)
    db.execute('SET LOCAL ROLE photo_scheduler')
    # Live authority reaches the existing inventory gate. After revoke, the
    # same malformed inventory is refused before the old core is invoked.
    with pytest.raises(errors.InvalidParameterValue,match='V2_ADOPTION_INVENTORY_INVALID'), db.transaction():
        db.execute(adopt,adopt_args)
    db.execute('RESET ROLE')
    task=uuid5(uuid5(run,'workflow'),'optical.lens:0')
    db.execute("UPDATE photo.tasks SET node_key='optical.lens:1' WHERE id=%s",(task,))
    assert not activation()
    db.execute("UPDATE photo.tasks SET node_key='optical.lens:0',execution_profile=execution_profile||'{\"backend\":\"cuda\"}'::jsonb WHERE id=%s",(task,))
    assert not activation()
    db.execute('UPDATE photo.tasks SET execution_profile=%s WHERE id=%s',(Jsonb(plan['profile']),task))
    assert activation()
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert not db.execute('SELECT photo.seal_v2_graph(%s,%s)',(tid,run)).fetchone()[0]
    db.execute('RESET ROLE')
    for statement,reset in [
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s","UPDATE photo.artifacts SET expires_at=NULL WHERE id=%s"),
        ("UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s","UPDATE photo.artifacts SET verified_at=clock_timestamp() WHERE id=%s"),
    ]:
        db.execute(statement,(original,))
        assert not activation()
        db.execute('SET LOCAL ROLE photo_api')
        assert db.execute(query,args).fetchall()==[]
        assert not db.execute(preflight,selection).fetchone()[0]
        assert db.execute(selection_query,selection).fetchall()==[]
        db.execute('RESET ROLE')
        db.execute(reset,(original,))
    db.execute("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s",(session,))
    assert not activation()
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert not db.execute(adopt,adopt_args).fetchone()[0]
    db.execute('RESET ROLE')
    db.execute('SET LOCAL ROLE photo_api')
    assert db.execute(query,args).fetchall()==[]
    assert not db.execute(preflight,selection).fetchone()[0]
    assert db.execute(selection_query,selection).fetchall()==[]
    assert db.execute(profile_query,profile_args).fetchall()==[]
    db.execute('RESET ROLE')
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert not db.execute(begin,admission).fetchone()[0]
    db.execute('RESET ROLE')
