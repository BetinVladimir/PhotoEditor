from hashlib import sha256
from uuid import uuid4
import json

SIGNATURE = 'photo.register_v26_render_draft_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text,uuid,bigint)'


def test_render_draft_freeze_function_scope(db):
    row = db.execute('SELECT prosecdef,pg_get_userbyid(proowner),proconfig FROM pg_proc WHERE oid=%s::regprocedure', (SIGNATURE,)).fetchone()
    assert row[0] and row[1] == 'photo_fn_owner' and 'search_path=pg_catalog, photo' in row[2]
    assert db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')", (SIGNATURE,)).fetchone() == (True,)
    for role in ('photo_worker', 'photo_scheduler', 'photo_gateway', 'photo_maintenance'):
        assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, SIGNATURE)).fetchone() == (False,)
    result_signature=SIGNATURE.replace('register_v26_render_draft_recipe(', 'register_v26_render_draft_recipe_result(')
    core_signature=SIGNATURE.replace('register_v26_render_draft_recipe(', 'register_v26_render_draft_recipe_core(')
    assert db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE'),has_function_privilege('photo_api',%s,'EXECUTE')",(result_signature,core_signature)).fetchone()==(True,False)
    for role in ('photo_worker','photo_scheduler','photo_gateway','photo_maintenance'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,result_signature)).fetchone()==(False,)
    row=db.execute('SELECT prosecdef,pg_get_userbyid(proowner),proconfig FROM pg_proc WHERE oid=%s::regprocedure',(result_signature,)).fetchone()
    assert row[0] and row[1]=='photo_fn_owner' and 'search_path=pg_catalog, photo' in row[2]
    args = (uuid4(),uuid4(),uuid4(),uuid4(),uuid4(),uuid4(),'a'*64,uuid4(),'b'*64,'{}',uuid4(),1)
    assert db.execute('SELECT photo.register_v26_render_draft_recipe('+','.join(['%s']*12)+')', args).fetchone() == (False,)


def _seed_registration(db, *, freeze_key=None, learning=None):
    from layer_original_fixture import seed_original_composition
    from photo_worker.contracts.base import canonical_bytes
    case = seed_original_composition(db)
    binding = json.loads(case['Params'])['base']
    tid, project, actor, session = case['Tenant'], case['Project'], case['Actor'], case['Session']
    authored_doc=dict(schema_version=3,source=binding)
    if learning is not None: authored_doc["learning_version_id"]=learning["version_id"]
    authored = canonical_bytes(authored_doc)
    authored_sha = sha256(authored).hexdigest()
    artifact = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,'test',%s,%s,%s,'application/json','v1',now(),'document') RETURNING id", (tid,str(uuid4()),authored_sha,len(authored))).fetchone()[0]
    stream = db.execute("INSERT INTO photo.v2_document_streams(tenant_id,project_id,asset_version_id,stream_type) VALUES(%s,%s,%s,'draft') RETURNING id", (tid,project,binding['asset_version_id'])).fetchone()[0]
    draft = db.execute("INSERT INTO photo.v2_documents(tenant_id,project_id,stream_id,version_no,document_type,artifact_id,sha256) VALUES(%s,%s,%s,1,'EditingRecipeV3',%s,%s) RETURNING id", (tid,project,stream,artifact,authored_sha)).fetchone()[0]
    revision = db.execute('UPDATE photo.v2_document_streams SET current_document_id=%s WHERE tenant_id=%s AND id=%s RETURNING revision', (draft,tid,stream)).fetchone()[0]
    body = canonical_bytes(dict(schema_version=1,recipe=json.loads(authored),recipe_sha256=authored_sha,scalars=dict(source=binding,model_lock_sha='a'*64) if learning is not None else dict(source=binding),learning=learning)).decode()
    digest = sha256(body.encode()).hexdigest()
    frozen_document = uuid4()
    if freeze_key is not None:
        from uuid import uuid5, NAMESPACE_OID
        asset = db.execute('SELECT asset_id FROM photo.asset_versions WHERE tenant_id=%s AND id=%s',(tid,binding['asset_version_id'])).fetchone()[0]
        frozen_document = uuid5(NAMESPACE_OID,f'v26-render-freeze/{tid}/{actor}/{asset}/{freeze_key}')
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING', (tid,))
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (tid,))
    db.execute('SET LOCAL ROLE photo_api')
    upload = (tid,binding['asset_version_id'],frozen_document,actor,session,digest,len(body),'test')
    reserved = db.execute('SELECT * FROM photo.reserve_v2_draft_upload('+','.join(['%s']*8)+')', upload).fetchone()
    complete = (*upload[:5],reserved[1],reserved[2],digest,len(body),'test','frozen-v1')
    assert db.execute('SELECT photo.complete_v2_draft_upload('+','.join(['%s']*11)+')', complete).fetchone() == (True,)
    registration = (*upload[:5],reserved[1],digest,binding['source']['artifact_id'],binding['source']['sha256'],body,draft,revision)
    register = 'SELECT photo.register_v26_render_draft_recipe('+','.join(['%s']*12)+')'
    return registration,register,tid,stream,artifact,frozen_document,draft,revision,authored_sha,session


def test_render_draft_freeze_exact_revision_document_and_authored_bytes(db):
    registration,register,tid,stream,artifact,frozen_document,draft,revision,authored_sha,session = _seed_registration(db)
    for index,value in ((10,uuid4()),(11,revision+1),(11,0),(11,None),(10,frozen_document),(4,uuid4())):
        changed = list(registration); changed[index] = value
        assert db.execute(register,changed).fetchone() == (False,)
    db.execute('RESET ROLE')
    assert db.execute('SELECT render_registered_at,render_draft_document_id,render_draft_revision FROM photo.v2_draft_uploads WHERE tenant_id=%s AND document_id=%s',(tid,frozen_document)).fetchone() == (None,None,None)
    db.execute('SET LOCAL ROLE photo_api')
    assert db.execute(register,registration).fetchone() == (True,)
    assert db.execute(register,registration).fetchone() == (True,)
    db.execute('RESET ROLE')
    row = db.execute('SELECT render_draft_document_id,render_draft_revision,render_authored_sha FROM photo.v2_draft_uploads WHERE tenant_id=%s AND document_id=%s',(tid,frozen_document)).fetchone()
    assert row == (draft,revision,authored_sha)
    for mutation,params in (
        ('UPDATE photo.v2_document_streams SET revision=revision+1 WHERE tenant_id=%s AND id=%s',(tid,stream)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND id=%s",(tid,artifact)),
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(session,)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(mutation,params);db.execute('SET LOCAL ROLE photo_api')
            assert db.execute(register,registration).fetchone() == (False,)
        db.execute('SET LOCAL ROLE photo_api')
        assert db.execute(register,registration).fetchone() == (True,)
        db.execute('RESET ROLE')


def test_concurrent_draft_revision_change_is_seen_after_lock_wait(database):
    import psycopg
    from concurrent.futures import ThreadPoolExecutor
    from threading import Event
    from time import monotonic, sleep
    with psycopg.connect(**database) as seed:
        registration,register,tid,stream,*_ = _seed_registration(seed)
        seed.execute('RESET ROLE')
    ready = Event(); state = {}
    def freeze():
        with psycopg.connect(**database) as connection:
            connection.execute("SET LOCAL statement_timeout='5s'")
            connection.execute("SELECT set_config('app.tenant_id',%s,true)",(tid,))
            connection.execute('SET LOCAL ROLE photo_api')
            state['pid'] = connection.execute('SELECT pg_backend_pid()').fetchone()[0]
            ready.set()
            return connection.execute(register,registration).fetchone()
    with psycopg.connect(**database) as writer, psycopg.connect(**database,autocommit=True) as observer:
        writer_pid = writer.execute('SELECT pg_backend_pid()').fetchone()[0]
        writer.execute('SELECT id FROM photo.v2_document_streams WHERE tenant_id=%s AND id=%s FOR UPDATE',(tid,stream))
        with ThreadPoolExecutor(max_workers=1) as executor:
            pending = executor.submit(freeze)
            try:
                assert ready.wait(2), 'freeze connection did not start'
                deadline = monotonic()+2; blocked = False
                while monotonic()<deadline:
                    blocked = observer.execute('SELECT %s=ANY(pg_blocking_pids(%s))',(writer_pid,state['pid'])).fetchone()[0]
                    if blocked: break
                    sleep(.01)
                assert blocked, 'freeze did not wait on the current stream lock'
                writer.execute('UPDATE photo.v2_document_streams SET revision=revision+1 WHERE tenant_id=%s AND id=%s',(tid,stream))
                writer.commit()
                assert pending.result(timeout=5) == (False,)
            finally:
                writer.rollback()
        assert observer.execute('SELECT render_registered_at,render_draft_document_id FROM photo.v2_draft_uploads WHERE tenant_id=%s AND document_id=%s',(tid,registration[2])).fetchone() == (None,None)


def test_frozen_draft_replay_scope_without_upload_table_grants(db):
    signature = 'photo.v26_render_frozen_draft_scope(uuid,uuid,uuid,uuid,bigint,uuid,text,uuid,uuid,text,uuid,uuid)'
    assert db.execute("SELECT has_table_privilege('photo_api','photo.v2_draft_uploads','SELECT')").fetchone() == (False,)
    assert db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')",(signature,)).fetchone() == (True,)
    for role in ('photo_worker','photo_scheduler','photo_gateway','photo_maintenance'):
        assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone() == (False,)
    row = db.execute('SELECT prosecdef,pg_get_userbyid(proowner),proconfig FROM pg_proc WHERE oid=%s::regprocedure',(signature,)).fetchone()
    assert row[0] and row[1]=='photo_fn_owner' and 'search_path=pg_catalog, photo' in row[2]
    args,register,tid,stream,artifact,document,draft,revision,*_ = _seed_registration(db)
    assert db.execute(register,args).fetchone() == (True,)
    db.execute('RESET ROLE')
    asset = db.execute('SELECT asset_id FROM photo.asset_versions WHERE tenant_id=%s AND id=%s',(tid,args[1])).fetchone()[0]
    scope_args = (tid,asset,document,draft,revision,args[5],args[6],args[1],args[7],args[8],args[3],args[4])
    scope = 'SELECT photo.v26_render_frozen_draft_scope('+','.join(['%s']*12)+')'
    db.execute('SET LOCAL ROLE photo_api')
    assert db.execute(scope,scope_args).fetchone() == (True,)
    for index,replacement in ((1,uuid4()),(2,uuid4()),(3,uuid4()),(4,revision+1),(5,uuid4()),(6,'f'*64),(7,uuid4()),(8,uuid4()),(9,'f'*64),(11,uuid4())):
        changed=list(scope_args);changed[index]=replacement
        assert db.execute(scope,changed).fetchone() == (False,)
    db.execute('RESET ROLE')
    db.execute('UPDATE photo.v2_document_streams SET revision=revision+1 WHERE tenant_id=%s AND id=%s',(tid,stream))
    db.execute('SET LOCAL ROLE photo_api')
    # Completed idempotent replies describe the original immutable snapshot.
    assert db.execute(scope,scope_args).fetchone() == (True,)
    db.execute('RESET ROLE')
    db.execute('UPDATE photo.v2_draft_uploads SET render_learning_checked=false WHERE tenant_id=%s AND document_id=%s',(tid,document))
    db.execute('SET LOCAL ROLE photo_api')
    assert db.execute(scope,scope_args).fetchone()==(False,)
    db.execute('RESET ROLE')
    from psycopg.types.json import Jsonb
    learning=dict(version_id=str(uuid4()),model=dict(artifact_id=str(uuid4()),sha256='1'*64),descriptor=dict(artifact_id=str(uuid4()),sha256='2'*64),analysis_id=str(uuid4()),ordinal=0)
    db.execute('UPDATE photo.v2_draft_uploads SET render_learning_checked=true,render_learning=%s,render_learning_model_lock=%s WHERE tenant_id=%s AND document_id=%s',(Jsonb(learning),'a'*64,tid,document))
    db.execute('SET LOCAL ROLE photo_api')
    assert db.execute(scope,scope_args).fetchone()==(False,)



def test_learning_denial_rolls_back_core_registration_even_for_sql_caller(db):
    learning=dict(version_id=str(uuid4()),model=dict(artifact_id=str(uuid4()),sha256='1'*64),descriptor=dict(artifact_id=str(uuid4()),sha256='2'*64),analysis_id=str(uuid4()),ordinal=0)
    args,register,tid,stream,artifact,document,*_=_seed_registration(db,learning=learning)
    result=register.replace('register_v26_render_draft_recipe(', 'register_v26_render_draft_recipe_result(')
    assert db.execute(result,args).fetchone()==('learning_expired',)
    assert db.execute(register,args).fetchone()==(False,)
    db.execute('RESET ROLE')
    assert db.execute('SELECT render_registered_at,render_source_id,render_authored_sha,render_draft_document_id,render_learning_checked,render_learning FROM photo.v2_draft_uploads WHERE tenant_id=%s AND document_id=%s',(tid,document)).fetchone()==(None,None,None,None,False,None)
