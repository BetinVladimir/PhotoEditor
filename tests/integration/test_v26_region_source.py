from uuid import uuid4
import psycopg


def test_region_source_is_narrow_and_role_scoped(database):
    signature='photo.v26_region_source(uuid,uuid,uuid,uuid,uuid)'
    with psycopg.connect(**database,autocommit=True) as db:
        for role,allowed in [('photo_api',True),('photo_scheduler',True),('photo_worker',False)]:
            assert db.execute('SELECT has_function_privilege(%s,%s,%s)',(role,signature,'EXECUTE')).fetchone()==(allowed,)
        for role in ('photo_api','photo_scheduler'):
            with db.transaction():
                db.execute('SET LOCAL ROLE '+role)
                assert db.execute('SELECT * FROM photo.v26_region_source(%s,%s,%s,%s,%s)',tuple(uuid4() for _ in range(5))).fetchone() is None


def test_region_recipe_lookup_requires_scoped_published_document(database):
    signature='photo.v26_region_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text)'
    with psycopg.connect(**database,autocommit=True) as db:
        for role,allowed in [('photo_api',True),('photo_scheduler',True),('photo_worker',False)]:
            assert db.execute('SELECT has_function_privilege(%s,%s,%s)',(role,signature,'EXECUTE')).fetchone()==(allowed,)
        for role in ('photo_api','photo_scheduler'):
            with db.transaction():
                db.execute('SET LOCAL ROLE '+role)
                args=tuple(uuid4() for _ in range(6))+('a'*64,)
                assert db.execute('SELECT * FROM photo.v26_region_recipe(%s,%s,%s,%s,%s,%s,%s)',args).fetchone() is None


def _region_scope_fixture(db):
    from test_v23_record_correction import _fixture
    from psycopg.types.json import Jsonb
    from hashlib import sha256
    import json
    from uuid import UUID
    tenant,actor,session,project,request,_=_fixture(db)
    version=UUID(request['asset_version_id'])
    asset,source=db.execute('SELECT asset_id,master_artifact_id FROM photo.asset_versions WHERE tenant_id=%s AND id=%s',(tenant,version)).fetchone()
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES(%s,'v2','user',%s,%s,1) RETURNING id",(tenant,actor,'a'*64)).fetchone()[0]
    run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES(%s,'v2','running',%s,%s,%s,%s,'fail_fast',true) RETURNING id",(tenant,snapshot,Jsonb({'project_id':str(project)}),'b'*64,actor)).fetchone()[0]
    workflow=db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'learning','v2.2',%s) RETURNING id",(tenant,project,run,snapshot,'c'*64)).fetchone()[0]
    body=json.dumps({'schema_version':1,'source':{'asset_version_id':str(version),'source':{'artifact_id':str(source),'sha256':'a'*64},'canvas_hash':'a'*64},'base_style':{'artifact_id':str(uuid4()),'sha256':'b'*64},'effective_delta':{'ev':0,'log_r':0,'log_g':0,'log_b':0,'saturation_delta':0,'retouch_delta':0},'renderer_version':'v2.2','icc_lock_sha':'c'*64,'model_lock_sha':'d'*64},sort_keys=True,separators=(',',':')).encode()
    digest=sha256(body).hexdigest()
    recipe=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,'test','recipe','recipe-v1',%s,%s,'application/json','v23_frozen_recipe',now()) RETURNING id",(tenant,digest,len(body))).fetchone()[0]
    doc=db.execute("INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,state,object_version,completed_at) VALUES(%s,%s,%s,'v23_frozen_base',%s,%s,%s,%s,'test','ready','recipe-v1',now()) RETURNING id",(tenant,run,uuid4(),recipe,uuid4(),digest,len(body))).fetchone()[0]
    args=(tenant,asset,version,actor,session,recipe,digest)
    def lookup(values=args):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_api')
            try:return db.execute('SELECT * FROM photo.v26_region_recipe(%s,%s,%s,%s,%s,%s,%s)',values).fetchone()
            finally:db.execute('RESET ROLE')
    assert lookup()[0:3]==(project,recipe,digest)
    source_row=db.execute('SELECT * FROM photo.v26_region_source(%s,%s,%s,%s,%s)',args[:5]).fetchone()
    recipe_row=lookup()
    def pin(row):return dict(ref=dict(artifact_id=str(row[1]),sha256=row[2]),bucket=row[3],object_key=row[4],object_version=row[5],bytes=row[6])
    plan=dict(schema_version=1,tenant_id=str(tenant),project_id=str(project),asset_id=str(asset),source=pin(source_row),recipe=pin(recipe_row))
    plan['request']=dict(version_id=str(version),recipe=plan['recipe']['ref'])
    plan['frozen_recipe']=dict(source=dict(asset_version_id=str(version),source=plan['source']['ref']))
    style_artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,'test','style','style-v1',%s,64,'application/json','document',now()) RETURNING id",(tenant,'b'*64)).fetchone()[0]
    style=db.execute("INSERT INTO photo.styles(tenant_id,project_id,name,created_by) VALUES(%s,%s,'Region',%s) RETURNING id",(tenant,project,actor)).fetchone()[0]
    db.execute("INSERT INTO photo.style_versions(tenant_id,style_id,version_no,config_json,config_hash,schema_version,renderer_version,approval_status,metrics,export_artifact_id) VALUES(%s,%s,1,'{}',%s,'2.0.0','photo_renderer_v2','accepted','{}',%s)",(tenant,style,'b'*64,style_artifact))
    plan['frozen_recipe']['base_style']=dict(artifact_id=str(style_artifact),sha256='b'*64)
    return tenant,actor,session,plan,style,recipe,run,args,lookup


def test_published_region_recipe_metadata_and_live_authority(db):
    from psycopg.types.json import Jsonb
    tenant,actor,session,plan,style,recipe,run,args,lookup=_region_scope_fixture(db)
    def locked(values=plan):
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
            db.execute('SET LOCAL ROLE photo_scheduler')
            try:return db.execute('SELECT photo.v26_region_locked_scope(%s,%s,%s,%s)',(tenant,actor,session,Jsonb(values))).fetchone()[0]
            finally:db.execute('RESET ROLE')
    assert locked() is True
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.styles SET archived_at=now() WHERE id=%s',(style,))
        assert locked() is False
    from copy import deepcopy
    for key in ('source','recipe'):
        changed=deepcopy(plan);changed[key]['object_version']='replacement'
        assert locked(changed) is False
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.sessions SET revoked_at=now() WHERE id=%s',(session,))
        assert locked() is False

    for index in range(7):
        wrong=list(args);wrong[index]='e'*64 if index==6 else uuid4()
        assert lookup(tuple(wrong)) is None
    import pytest
    with pytest.raises(psycopg.errors.CheckViolation),db.transaction(force_rollback=True):
        db.execute("UPDATE photo.artifacts SET object_version='replacement' WHERE id=%s",(recipe,))
    for query,values in [
        ("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",(recipe,)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(recipe,)),
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,)),
        ('UPDATE photo.sessions SET revoked_at=now() WHERE id=%s',(session,)),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(query,values)
            assert lookup() is None


def test_region_style_locks_hold_until_scope_transaction_finishes(database):
    import pytest
    from psycopg.types.json import Jsonb
    # Commit fixture so genuinely independent connections see the same rows.
    with psycopg.connect(**database,autocommit=True) as setup:
        with setup.transaction():
            tenant,actor,session,plan,style,recipe,run,_,_=_region_scope_fixture(setup)
    with psycopg.connect(**database,autocommit=True) as reader, psycopg.connect(**database,autocommit=True) as writer:
        with reader.transaction():
            reader.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
            reader.execute('SET LOCAL ROLE photo_scheduler')
            assert reader.execute('SELECT photo.v26_region_locked_scope(%s,%s,%s,%s)',(tenant,actor,session,Jsonb(plan))).fetchone()==(True,)
            for query,values in (
                ('UPDATE photo.styles SET archived_at=now() WHERE id=%s',(style,)),
                ('UPDATE photo.style_versions SET export_artifact_id=NULL WHERE tenant_id=%s AND style_id=%s',(tenant,style)),
                ("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",(plan['frozen_recipe']['base_style']['artifact_id'],)),
            ):
                with pytest.raises(psycopg.errors.LockNotAvailable):
                    with writer.transaction():
                        writer.execute("SET LOCAL lock_timeout='150ms'")
                        writer.execute(query,values)
        # Releasing the reader permits archive; subsequent authority is denied.
        with writer.transaction():
            writer.execute('UPDATE photo.styles SET archived_at=now() WHERE id=%s',(style,))
            # The opposite order also waits; an uncommitted archive cannot be
            # skipped by the scope reader and checked against stale authority.
            with pytest.raises(psycopg.errors.LockNotAvailable):
                with reader.transaction():
                    reader.execute("SET LOCAL lock_timeout='150ms'")
                    reader.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
                    reader.execute('SET LOCAL ROLE photo_scheduler')
                    reader.execute('SELECT photo.v26_region_locked_scope(%s,%s,%s,%s)',(tenant,actor,session,Jsonb(plan)))
        with reader.transaction():
            reader.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
            reader.execute('SET LOCAL ROLE photo_scheduler')
            assert reader.execute('SELECT photo.v26_region_locked_scope(%s,%s,%s,%s)',(tenant,actor,session,Jsonb(plan))).fetchone()==(False,)


def test_region_snapshot_requires_exact_single_source(db):
    from psycopg.types.json import Jsonb
    tenant,actor,session,plan,_,_,_,_,_=_region_scope_fixture(db)
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES(%s,'v2','user',%s,%s,1,now()+interval '1 hour') RETURNING id",(tenant,actor,'a'*64)).fetchone()[0]
    def check(snapshot_id=snapshot,principal=actor):
        return db.execute('SELECT photo.v26_region_snapshot_scope(%s,%s,%s,%s)',(tenant,snapshot_id,principal,Jsonb(plan))).fetchone()[0]
    assert check() is False
    db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,0,%s,%s,%s,'{}')",(tenant,snapshot,plan['request']['version_id'],plan['source']['ref']['artifact_id'],plan['source']['ref']['sha256']))
    assert check() is False  # draft snapshots are mutable and cannot authorize commit
    db.execute('UPDATE photo.snapshots SET expires_at=NULL WHERE id=%s',(snapshot,))
    assert check() is True
    signature='photo.v26_region_admission_scope(uuid,uuid,uuid,uuid,jsonb)'
    for role in ('photo_api','photo_scheduler','photo_worker'):
        assert db.execute('SELECT has_function_privilege(%s,%s,%s)',(role,signature,'EXECUTE')).fetchone()==(role=='photo_api',)
    def admit_scope():
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
            db.execute('SET LOCAL ROLE photo_api')
            try:return db.execute('SELECT photo.v26_region_admission_scope(%s,%s,%s,%s,%s)',(tenant,snapshot,actor,session,Jsonb(plan))).fetchone()[0]
            finally:db.execute('RESET ROLE')
    assert admit_scope() is False  # disabled operation cannot create a draft
    # This toggle only exercises authority, not renderer qualification/activation.
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='region.preview' AND version='1'")
        assert admit_scope() is True
        db.execute('UPDATE photo.sessions SET revoked_at=now() WHERE id=%s',(session,))
        assert admit_scope() is False
    assert check(principal=uuid4()) is False
    assert check(snapshot_id=uuid4()) is False
    from copy import deepcopy
    for section,key in [('request','version_id'),('source','ref')]:
        changed=deepcopy(plan)
        changed[section][key]=str(uuid4()) if key=='version_id' else dict(artifact_id=str(uuid4()),sha256='e'*64)
        assert db.execute('SELECT photo.v26_region_snapshot_scope(%s,%s,%s,%s)',(tenant,snapshot,actor,Jsonb(changed))).fetchone()==(False,)
    import pytest
    with pytest.raises(psycopg.errors.CheckViolation),db.transaction(force_rollback=True):
        db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,1,%s,%s,%s,'{}')",(tenant,snapshot,plan['request']['version_id'],plan['source']['ref']['artifact_id'],plan['source']['ref']['sha256']))
