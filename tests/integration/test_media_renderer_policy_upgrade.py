"""02544→02545 exact authority/ACL/OID upgrade; no photographic PASS claim.

Actual old/current rendering is covered by authored worker adapter scenarios.
This database scenario checks installed bodies, privileges and refusal paths,
not an independently qualified physical derivative publication fixture.
"""
from pathlib import Path
from uuid import uuid4
import pytest
from psycopg import sql, Error
from psycopg.types.json import Jsonb
from test_v27_native_source_authority import seed_native_source_authority

ROOT=Path(__file__).resolve().parents[2]
NAMES=(
 'pin_v26_render_derivative_document','pin_v29_perspective_preview_document',
 'pin_v29_multi_preview_document','pin_v28_restoration_preview_document',
 'pin_v25_lens_preview_document','v28_open_eyes_base_preview_ready',
 'v28_relight_base_preview_ready','v28_local_portrait_base_preview_ready',
 'review_after_media_live','v28_local_portrait_review_preview_ready',
 'v28_relight_viewed_pair_ready')
ROLES=('photo_api','photo_scheduler','photo_worker','photo_publisher','photo_portal',
       'photo_auth','photo_gateway','photo_maintenance')


def inventory(db):
    return {row[0]:row[1:] for row in db.execute("""
      SELECT proname,oid,proowner,proacl::text,prosecdef,proconfig,prosrc
      FROM pg_proc WHERE pronamespace='photo'::regnamespace AND proname=ANY(%s)
    """,(list(NAMES),)).fetchall()}


@pytest.mark.migration_head(2544)
def test_media_policy_upgrade_preserves_exact_authority_and_public_function_identity(db):
    _,case,_=seed_native_source_authority(db,'mask.person')
    tenant,actor,snapshot=[case[key] for key in ('Tenant','Actor','Snapshot')]
    old_run=uuid4()
    def insert_run(run):
        db.execute("""INSERT INTO photo.runs(id,tenant_id,kind,input_snapshot_id,
          config_snapshot,config_hash,created_by,status,graph_sealed,failure_policy)
          VALUES(%s,%s,'v2',%s,%s,%s,%s,'draft',false,'fail_fast')""",
          (run,tenant,snapshot,Jsonb(dict(project_id=case['Project'])),'e'*64,actor))
    insert_run(old_run)
    db.execute('SET CONSTRAINTS ALL IMMEDIATE')
    before=inventory(db);assert set(before)==set(NAMES)
    # Exercise a deployment's permissive default function grants. Private
    # branches must remove these explicit ACLs in addition to PUBLIC.
    db.execute('ALTER DEFAULT PRIVILEGES GRANT EXECUTE ON FUNCTIONS TO photo_api,photo_scheduler')
    db.execute((ROOT/'backend/db/migrations/02545_media_renderer_policy_revision.sql').read_text().split('-- +goose Down')[0])
    after=inventory(db)
    new_run=uuid4();insert_run(new_run)
    assert db.execute('SELECT photo.media_renderer_policy_for_run(%s,%s)',(tenant,old_run)).fetchone()==('legacy_v1',)
    assert db.execute('SELECT photo.media_renderer_policy_for_run(%s,%s)',(tenant,new_run)).fetchone()==('declared_encoding_alpha8_icc_v2',)
    assert db.execute('SELECT photo.media_renderer_policy_for_run(%s,%s)',(tenant,uuid4())).fetchone()==(None,)
    db.execute('SAVEPOINT immutable_media_policy')
    with pytest.raises(Error,match='Immutable media renderer policy'):
        db.execute("UPDATE photo.runs SET media_renderer_policy='declared_encoding_alpha8_icc_v2' WHERE id=%s",(old_run,))
    db.execute('ROLLBACK TO SAVEPOINT immutable_media_policy')
    anchor='"quality":88,"schema_version":1'
    replacement='"quality":88,"renderer_policy":"declared_encoding_alpha8_icc_v2","schema_version":1'
    for name in NAMES:
        # OID, owner, ACL, SECURITY DEFINER and search_path must not change.
        assert after[name][:-1]==before[name][:-1]
        old=db.execute("SELECT prosrc FROM pg_proc WHERE pronamespace='photo'::regnamespace AND proname=%s",(name+'_media_v1',)).fetchone()[0]
        new=db.execute("SELECT prosrc FROM pg_proc WHERE pronamespace='photo'::regnamespace AND proname=%s",(name+'_media_v2',)).fetchone()[0]
        assert old==before[name][-1] and old.count(anchor)==1
        assert new==old.replace(anchor,replacement)
        assert name+'_media_v1' in after[name][-1] and name+'_media_v2' in after[name][-1]
        for role in ROLES:
            for suffix in ('_media_v1','_media_v2'):
                assert db.execute("SELECT has_function_privilege(%s,oid,'EXECUTE') FROM pg_proc WHERE pronamespace='photo'::regnamespace AND proname=%s",(role,name+suffix)).fetchone()==(False,)
    assert db.execute('SELECT photo.media_renderer_policy_ready()').fetchone()==(True,)
    db.execute('SAVEPOINT media_reapply')
    with pytest.raises(Exception,match='Unexpected media profile gate|already exist'):
        db.execute((ROOT/'backend/db/migrations/02545_media_renderer_policy_revision.sql').read_text().split('-- +goose Down')[0])
    db.execute('ROLLBACK TO SAVEPOINT media_reapply')
    assert inventory(db)==after


@pytest.mark.parametrize('drift',['grant','owner','body','missing','overload','default','check','trigger_body','reader_acl','wrapper'])
def test_media_startup_rejects_private_policy_drift(db,drift):
    name='pin_v25_lens_preview_document_media_v2'
    signature=name+'(uuid,uuid,text,text,text,text,text)'
    assert db.execute('SELECT photo.media_renderer_policy_ready()').fetchone()==(True,)
    if drift=='grant':db.execute(sql.SQL('GRANT EXECUTE ON FUNCTION photo.{} TO photo_api').format(sql.SQL(signature)))
    elif drift=='owner':db.execute(sql.SQL('ALTER FUNCTION photo.{} OWNER TO photo_api').format(sql.SQL(signature)))
    elif drift=='missing':db.execute(sql.SQL('ALTER FUNCTION photo.{} RENAME TO media_policy_missing_branch').format(sql.SQL(signature)))
    elif drift=='overload':
        db.execute(sql.SQL('ALTER FUNCTION photo.{} RENAME TO media_policy_missing_branch').format(sql.SQL(signature)))
        db.execute(sql.SQL("CREATE FUNCTION photo.{}() RETURNS uuid LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS 'SELECT NULL::uuid'").format(sql.Identifier(name)))
    elif drift=='default':db.execute("ALTER TABLE photo.runs ALTER COLUMN media_renderer_policy SET DEFAULT 'legacy_v1'")
    elif drift=='check':db.execute('ALTER TABLE photo.runs DROP CONSTRAINT runs_media_renderer_policy')
    elif drift=='trigger_body':
        db.execute("CREATE OR REPLACE FUNCTION photo.guard_media_renderer_policy() RETURNS trigger LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,photo AS $$ BEGIN RETURN NEW;END $$")
    elif drift=='reader_acl':db.execute('GRANT EXECUTE ON FUNCTION photo.media_renderer_policy_for_run(uuid,uuid) TO photo_api')
    elif drift=='wrapper':
        sig='photo.pin_v25_lens_preview_document(uuid,uuid,text,text,text,text,text)'
        definition=db.execute('SELECT pg_get_functiondef(%s::regprocedure)',(sig,)).fetchone()[0]
        db.execute(definition.replace("photo.media_renderer_policy_for_run(p_tenant,p_run)='legacy_v1'",'true'))
    else:
        definition=db.execute('SELECT pg_get_functiondef(%s::regprocedure)',('photo.'+signature,)).fetchone()[0]
        db.execute(definition.replace('declared_encoding_alpha8_icc_v2','uninstalled_policy'))
    assert db.execute('SELECT photo.media_renderer_policy_ready()').fetchone()==(False,)


def test_media_policy_fresh_head_private_branches_and_missing_scope_refuse(db):
    for name in NAMES:
        row=db.execute("""SELECT oid,proargtypes::oid[],prorettype::regtype::text
          FROM pg_proc WHERE pronamespace='photo'::regnamespace AND proname=%s""",(name,)).fetchone()
        assert row is not None
        types=[db.execute('SELECT %s::oid::regtype::text',(oid,)).fetchone()[0] for oid in row[1]]
        # No actor/tenant/source/receipt is supplied. Compatibility dispatch must
        # never manufacture a successful pin or readiness result.
        args=sql.SQL(',').join(sql.SQL('NULL::{}').format(sql.SQL(kind)) for kind in types)
        db.execute('SAVEPOINT missing_media_authority')
        try:
            value=db.execute(sql.SQL('SELECT photo.{}({})').format(sql.Identifier(name),args)).fetchone()[0]
            assert value in (None,False)
        except Error:
            # UUID pin dispatch requires a currently active tenant before it
            # may read frozen policy; missing scope is an explicit SQL refusal.
            assert row[2]=='uuid'
        finally:
            db.execute('ROLLBACK TO SAVEPOINT missing_media_authority')


def test_preview_policy_is_producer_bound_before_and_after_child_save(db):
    # Relational policy scope, not a substitute for full derivative adoption.
    _,case,_=seed_native_source_authority(db,'mask.person')
    tenant,actor,snapshot=[case[key] for key in ('Tenant','Actor','Snapshot')]
    producer,parent=db.execute("SELECT id,run_id FROM photo.tasks WHERE tenant_id=%s AND node_key='source:0'",(tenant,)).fetchone()
    child=uuid4()
    expected=db.execute('SELECT media_renderer_policy FROM photo.runs WHERE id=%s',(parent,)).fetchone()[0]
    assert db.execute('SELECT photo.media_renderer_policy_for_preview(%s,%s,%s)',(tenant,child,producer)).fetchone()[0]==expected
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by,status,graph_sealed,failure_policy) VALUES(%s,%s,'preview',%s,%s,%s,%s,'draft',false,'fail_fast')",(child,tenant,snapshot,Jsonb(dict(source_task_id=str(producer))),'e'*64,actor))
    assert db.execute('SELECT photo.media_renderer_policy_for_preview(%s,%s,%s)',(tenant,child,producer)).fetchone()[0]==expected
    for scope in [(tenant,child,uuid4()),(uuid4(),child,producer)]:
        db.execute('SAVEPOINT refused_preview_policy')
        with pytest.raises(Error,match='Media preview policy'):
            db.execute('SELECT photo.media_renderer_policy_for_preview(%s,%s,%s)',scope)
        db.execute('ROLLBACK TO SAVEPOINT refused_preview_policy')
    wrong=uuid4()
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by,status,graph_sealed,failure_policy) VALUES(%s,%s,'preview',%s,%s,%s,%s,'draft',false,'fail_fast')",(wrong,tenant,snapshot,Jsonb(dict(source_task_id=str(uuid4()))),'f'*64,actor))
    db.execute('SAVEPOINT wrong_saved_intent')
    with pytest.raises(Error,match='Media preview policy intent unavailable'):
        db.execute('SELECT photo.media_renderer_policy_for_preview(%s,%s,%s)',(tenant,wrong,producer))
    db.execute('ROLLBACK TO SAVEPOINT wrong_saved_intent')


def seed_queued_retry_policy_command(db):
    digest='a'*64
    user,epoch=db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)",(f'{uuid4()}@test.example',)).fetchone()
    tenant=db.execute('SELECT id FROM photo.tenants WHERE owner_user_id=%s',(user,)).fetchone()[0]
    project=db.execute("INSERT INTO photo.projects(tenant_id,name,visibility) VALUES(%s,'Retry','workspace') RETURNING id",(tenant,)).fetchone()[0]
    artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role) VALUES(%s,'photo',%s,'v1',%s,10,'application/json','working_image') RETURNING id",(tenant,f'masters/{uuid4()}',digest)).fetchone()[0]
    asset=db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tenant,project,artifact)).fetchone()[0]
    version=db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",(tenant,asset,artifact,digest)).fetchone()[0]
    collection=db.execute("INSERT INTO photo.collections(tenant_id,project_id,name) VALUES(%s,%s,'Retry') RETURNING id",(tenant,project)).fetchone()[0]
    item=db.execute("INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position,moderation_state) VALUES(%s,%s,%s,%s,0,'accepted') RETURNING id",(tenant,collection,asset,version)).fetchone()[0]
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count) VALUES(%s,'selection',%s,1,'user',%s,%s,1) RETURNING id",(tenant,collection,user,digest)).fetchone()[0]
    db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,0,%s,%s,%s,%s,'{}')",(tenant,snapshot,item,version,artifact,digest))
    run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'bulk','completed',%s,'{}',%s,%s,true) RETURNING id",(tenant,snapshot,digest,user)).fetchone()[0]
    variants=['thumbnail','preview']
    bulk=db.execute("INSERT INTO photo.bulk_operations(tenant_id,collection_id,snapshot_id,action,params,expected_next_revision,status,created_by,run_id) VALUES(%s,%s,%s,'retry_media',%s,1,'completed',%s,%s) RETURNING id",(tenant,collection,snapshot,Jsonb({'variants':variants}),user,run)).fetchone()[0]
    db.execute("INSERT INTO photo.bulk_item_results(tenant_id,bulk_operation_id,snapshot_ordinal,status,result) VALUES(%s,%s,0,'succeeded','{}')",(tenant,bulk))
    event=uuid4();payload={'schema_version':1,'event_id':str(event),'bulk_id':str(bulk),'item_id':str(item),'asset_version_id':str(version),'variants':variants}
    db.execute("INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(%s,%s,'photo.commands.v1','media.retry',%s)",(tenant,event,Jsonb(payload)))
    assert db.execute('SELECT photo.accept_media_retry_command(%s,%s,%s,%s,%s)',(event,bulk,item,version,Jsonb(variants))).fetchone()[0]
    assert db.execute('SELECT photo.accept_media_retry_command(%s,%s,%s,%s,%s)',(event,bulk,item,version,Jsonb(variants))).fetchone()[0]
    return tenant,event,version,variants


@pytest.mark.migration_head(2544)
def test_queued_retry_without_run_retains_legacy_policy_through_upgrade_and_begin(db):
    tenant,event,version,variants=seed_queued_retry_policy_command(db)
    assert db.execute('SELECT run_id,status FROM photo.media_retry_jobs WHERE event_id=%s',(event,)).fetchone()==(None,'queued')
    db.execute('SET CONSTRAINTS ALL IMMEDIATE')
    db.execute((ROOT/'backend/db/migrations/02545_media_renderer_policy_revision.sql').read_text().split('-- +goose Down')[0])
    claimed=db.execute('SELECT event_id,materialize_token FROM photo.claim_media_retry_job()').fetchone()
    assert claimed[0]==event
    token=claimed[1]
    assert db.execute('SELECT photo.media_renderer_policy_for_retry(%s,%s,%s)',(tenant,event,token)).fetchone()[0]=='legacy_v1'
    run=uuid4();config=dict(event_id=str(event),asset_version_id=str(version),variants=variants,srgb_sha256='b'*64)
    for _ in range(2):
        assert db.execute('SELECT photo.begin_media_retry_plan(%s,%s,%s,%s,%s)',(event,token,run,'c'*64,Jsonb(config))).fetchone()[0]
        assert db.execute('SELECT media_renderer_policy FROM photo.runs WHERE id=%s',(run,)).fetchone()[0]=='legacy_v1'
    db.execute('SAVEPOINT immutable_retry_policy')
    with pytest.raises(Error,match='Immutable media renderer policy'):
        db.execute("UPDATE photo.media_retry_jobs SET media_renderer_policy='declared_encoding_alpha8_icc_v2' WHERE event_id=%s",(event,))
    db.execute('ROLLBACK TO SAVEPOINT immutable_retry_policy')
    # A genuinely new durable command uses current policy independently of
    # the old command's resumed run. Uses real intake/outbox admission again.
    new_tenant,new_event,new_version,new_variants=seed_queued_retry_policy_command(db)
    assert new_tenant!=tenant
    assert db.execute('SELECT media_renderer_policy,run_id FROM photo.media_retry_jobs WHERE event_id=%s',(new_event,)).fetchone()==('declared_encoding_alpha8_icc_v2',None)
    new_claim=db.execute('SELECT event_id,materialize_token FROM photo.claim_media_retry_job()').fetchone()
    assert new_claim[0]==new_event
    assert db.execute('SELECT photo.media_renderer_policy_for_retry(%s,%s,%s)',(new_tenant,new_event,new_claim[1])).fetchone()[0]=='declared_encoding_alpha8_icc_v2'
    new_run=uuid4();new_config=dict(event_id=str(new_event),asset_version_id=str(new_version),variants=new_variants,srgb_sha256='b'*64)
    assert db.execute('SELECT photo.begin_media_retry_plan(%s,%s,%s,%s,%s)',(new_event,new_claim[1],new_run,'d'*64,Jsonb(new_config))).fetchone()[0]
    assert db.execute('SELECT media_renderer_policy FROM photo.runs WHERE id=%s',(new_run,)).fetchone()[0]=='declared_encoding_alpha8_icc_v2'
    for args in [(tenant,event,uuid4()),(new_tenant,event,token)]:
        db.execute('SAVEPOINT refused_retry_scope')
        with pytest.raises(Error,match='Media retry policy scope unavailable'):
            db.execute('SELECT photo.media_renderer_policy_for_retry(%s,%s,%s)',args)
        db.execute('ROLLBACK TO SAVEPOINT refused_retry_scope')
