"""Prove the V21 capability replacement against real JSONB worker registrations."""
from pathlib import Path
from uuid import uuid4, uuid5

import pytest
from psycopg import errors
from psycopg.types.json import Jsonb

from test_database import tenant
from test_task_leases import queued_task, claim, commit, manifest
from test_gateway_outputs import setup as setup_output, begin as begin_output, complete as complete_output
from test_gateway_inputs import setup as setup_input, read as read_input
from test_admission import ready_task, reserve


ROOT = Path(__file__).resolve().parents[2]
SCHEMA_SHA = '341984c53d787175fb5a9eec4e5d55ab74e9cd293075add8965d6064aba2e815'


def seal_v2_test_closure(db, item, project):
    """Install SQL-level transport evidence for worker/dispatch gate tests."""
    params = item['command']['params']
    def artifact(digest):
        return db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,"
                          "media_type,object_version,verified_at) VALUES(%s,'test',%s,%s,2,"
                          "'application/json','v1',clock_timestamp()) RETURNING id",
                          (item['tenant'], str(uuid4()), digest)).fetchone()[0]
    chunk, manifest_ref_id = artifact('b' * 64), artifact('c' * 64)
    manifest_ref = {'artifact_id': str(manifest_ref_id), 'sha256': 'c' * 64}
    command = {**item['command'], 'inputs': [manifest_ref]}
    db.execute("UPDATE photo.tasks SET inputs=%s,dispatch_command=CASE WHEN dispatch_command IS NULL "
               "THEN NULL ELSE %s END,execution_profile=execution_profile || %s WHERE id=%s",
               (Jsonb([manifest_ref]), Jsonb(command),
                Jsonb({'renderer_version': 'v2.2', 'contracts_sha': SCHEMA_SHA,
                       'v2_input_manifest': manifest_ref, 'v2_entry_count': 1,
                       'v2_entries_sha': 'a' * 64}), item['task']))
    for artifact_id, digest, contract, ordinal in (
        (params['artifact_id'], params['sha256'], 'Params', 0),
        (chunk, 'b' * 64, 'InputClosureChunk', 1_000_000),
        (manifest_ref_id, 'c' * 64, 'InputClosureManifest', 1_000_001),
    ):
        db.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,"
                   "sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,%s,%s)",
                   (item['tenant'], project, item['task'], artifact_id, digest, contract, ordinal))
    item['command'] = command


def test_capabilities_evidence_handles_array_qualified_operations(db):
    function = (ROOT / 'backend/db/v2-scaffolds/00196_capabilities.sql').read_text()
    db.execute(function)
    item = queued_task(db)
    db.execute("UPDATE photo.resource_nodes SET enabled=true,reported_at=clock_timestamp(),"
               "doctor_report_sha256=%s WHERE node_id=%s", ('a' * 64, item['node']))
    db.execute("UPDATE photo.worker_registrations SET certificate_sha256=%s WHERE worker_id=%s",
               ('f' * 64, item['worker']))
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(item['tenant']),))
    db.execute('SET LOCAL ROLE photo_api')
    rows = db.execute("SELECT operation,available_backends,evidence FROM photo.tenant_capabilities_evidence() "
                      "WHERE operation='asset.verify'").fetchall()
    assert len(rows) == 1
    assert rows[0][1] == ['cpu']
    assert rows[0][2] == [{'backend': 'cpu', 'doctor_report_sha256': 'a' * 64,
                            'model_lock_id': None, 'worker_certificate_sha256': 'f' * 64}]


def test_v21_reference_schema_scaffold_is_valid_postgresql(db):
    schema = (ROOT / 'backend/db/v2-scaffolds/00196_schema.sql').read_text()
    db.execute(schema)
    names = {row[0] for row in db.execute(
        "SELECT tablename FROM pg_tables WHERE schemaname='photo' AND "
        "tablename IN ('v2_workflows','v2_documents','v2_document_streams',"
        "'v2_document_refs','v2_artifact_links','model_qualifications',"
        "'v2_document_approvals','v2_task_inputs','v2_review_batches')").fetchall()}
    assert len(names) == 9
    assert db.execute("SELECT count(*) FROM pg_constraint WHERE conrelid='photo.v2_documents'::regclass "
                      "AND contype='f'").fetchone()[0] >= 3
    for name in names:
        assert db.execute("SELECT relrowsecurity AND relforcerowsecurity FROM pg_class "
                          "WHERE oid=%s::regclass", (f'photo.{name}',)).fetchone()[0]
        assert not db.execute("SELECT has_table_privilege('photo_api',%s,'INSERT')",
                              (f'photo.{name}',)).fetchone()[0]
        assert not db.execute("SELECT has_table_privilege('photo_worker',%s,'DELETE')",
                              (f'photo.{name}',)).fetchone()[0]


def test_v21_immutable_documents_and_stream_revision_guards(db):
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_schema.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_guards.sql').read_text())
    tid, _ = tenant(db, str(uuid4()) + '@example.test')
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'V21') RETURNING id",
                         (tid,)).fetchone()[0]
    stream = db.execute("INSERT INTO photo.v2_document_streams(tenant_id,project_id,stream_type) "
                        "VALUES(%s,%s,'shot_list') RETURNING id", (tid, project)).fetchone()[0]
    artifact = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) "
                          "VALUES(%s,'test',%s,%s,2,'application/json') RETURNING id",
                          (tid, str(uuid4()), 'a' * 64)).fetchone()[0]
    document = db.execute("INSERT INTO photo.v2_documents(tenant_id,project_id,stream_id,version_no,"
                          "document_type,artifact_id,sha256) VALUES(%s,%s,%s,1,'ShotList',%s,%s) RETURNING id",
                          (tid, project, stream, artifact, 'a' * 64)).fetchone()[0]
    with pytest.raises(errors.ObjectNotInPrerequisiteState), db.transaction():
        db.execute("UPDATE photo.v2_documents SET sha256=%s WHERE id=%s", ('b' * 64, document))
    db.execute("UPDATE photo.v2_document_streams SET revision=999 WHERE id=%s", (stream,))
    assert db.execute("SELECT revision FROM photo.v2_document_streams WHERE id=%s", (stream,)).fetchone()[0] == 2


def test_v21_scope_guards_reject_legacy_run_and_stream_reparent(db):
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_run_kind.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_schema.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_guards.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_project_guards.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_review_guards.sql').read_text())
    tid, user = tenant(db, str(uuid4()) + '@example.test')
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'One') RETURNING id",
                         (tid,)).fetchone()[0]
    other = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Two') RETURNING id",
                       (tid,)).fetchone()[0]
    snapshot = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) "
                          "VALUES(%s,'input','user',%s,%s,0) RETURNING id",
                          (tid, user, 'a' * 64)).fetchone()[0]
    run = db.execute("INSERT INTO photo.runs(tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,"
                     "created_by) VALUES(%s,'apply',%s,%s,%s,%s) RETURNING id",
                     (tid, snapshot, '{}', 'b' * 64, user)).fetchone()[0]
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,"
                   "workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'preview','v2',%s)",
                   (tid, project, run, snapshot, 'c' * 64))
    stream = db.execute("INSERT INTO photo.v2_document_streams(tenant_id,project_id,stream_type) "
                        "VALUES(%s,%s,'shot_list') RETURNING id", (tid, project)).fetchone()[0]
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("UPDATE photo.v2_document_streams SET project_id=%s WHERE id=%s", (other, stream))

    v2_snapshot = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) "
                             "VALUES(%s,'v2','user',%s,%s,0) RETURNING id",
                             (tid, user, 'd' * 64)).fetchone()[0]
    v2_run = db.execute("INSERT INTO photo.runs(tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,"
                        "created_by,failure_policy) VALUES(%s,'v2',%s,%s,%s,%s,'fail_fast') RETURNING id",
                        (tid, v2_snapshot, Jsonb({'project_id': str(project)}), 'e' * 64, user)).fetchone()[0]
    workflow = db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,"
                          "workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'preview','v2',%s) "
                          "RETURNING id", (tid, project, v2_run, v2_snapshot, 'f' * 64)).fetchone()[0]
    assert workflow is not None
    decisions = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,"
                           "media_type,object_version,verified_at) VALUES(%s,'test',%s,%s,2,"
                           "'application/json','v1',clock_timestamp()) RETURNING id",
                           (tid, str(uuid4()), 'f' * 64)).fetchone()[0]
    db.execute("INSERT INTO photo.v2_review_batches(tenant_id,project_id,workflow_id,decisions_artifact_id,"
               "actor_id,action) VALUES(%s,%s,%s,%s,%s,'decide')",
               (tid, project, workflow, decisions, user))
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("INSERT INTO photo.v2_review_batches(tenant_id,project_id,workflow_id,child_run_id,"
                   "decisions_artifact_id,actor_id,action) VALUES(%s,%s,%s,%s,%s,%s,'apply')",
                   (tid, project, workflow, run, decisions, user))
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("UPDATE photo.runs SET status='completed_with_errors' WHERE id=%s", (v2_run,))


def test_v21_document_closure_rejects_wrong_hash_and_unverified_bytes(db):
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_schema.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_artifact_guards.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_draft_publication.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_gc_refs.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_draft_refs.sql').read_text())
    tid, _ = tenant(db, str(uuid4()) + '@example.test')
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'V21') RETURNING id",
                         (tid,)).fetchone()[0]
    stream = db.execute("INSERT INTO photo.v2_document_streams(tenant_id,project_id,stream_type) "
                        "VALUES(%s,%s,'shot_list') RETURNING id", (tid, project)).fetchone()[0]
    artifact = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) "
                          "VALUES(%s,'test',%s,%s,2,'application/json') RETURNING id",
                          (tid, str(uuid4()), 'a' * 64)).fetchone()[0]
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("INSERT INTO photo.v2_documents(tenant_id,project_id,stream_id,version_no,"
                   "document_type,artifact_id,sha256) VALUES(%s,%s,%s,1,'ShotList',%s,%s)",
                   (tid, project, stream, artifact, 'a' * 64))
    artifact = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,"
                          "media_type,object_version,verified_at) VALUES(%s,'test',%s,%s,2,"
                          "'application/json','v1',clock_timestamp()) RETURNING id",
                          (tid, str(uuid4()), 'a' * 64)).fetchone()[0]
    assert not db.execute("SELECT photo.v2_artifact_has_references(%s,%s)",
                          (tid, artifact)).fetchone()[0]
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("INSERT INTO photo.v2_documents(tenant_id,project_id,stream_id,version_no,"
                   "document_type,artifact_id,sha256) VALUES(%s,%s,%s,1,'ShotList',%s,%s)",
                   (tid, project, stream, artifact, 'b' * 64))
    document = db.execute("INSERT INTO photo.v2_documents(tenant_id,project_id,stream_id,version_no,"
                          "document_type,artifact_id,sha256) VALUES(%s,%s,%s,1,'ShotList',%s,%s) RETURNING id",
                          (tid, project, stream, artifact, 'a' * 64)).fetchone()[0]
    assert db.execute("SELECT photo.v2_artifact_has_references(%s,%s)",
                      (tid, artifact)).fetchone()[0]
    ref = {'artifact_id': str(artifact), 'sha256': 'a' * 64, 'purpose': 'e' * 64, 'ordinal': 0}
    with pytest.raises(errors.InvalidParameterValue), db.transaction():
        db.execute("SELECT photo.register_v2_document_refs(%s,%s,%s,%s)",
                   (tid, project, document, Jsonb([{**ref, 'ordinal': 1}])))
    assert db.execute("SELECT photo.register_v2_document_refs(%s,%s,%s,%s)",
                      (tid, project, document, Jsonb([ref]))).fetchone()[0] == 1
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_gc_integration.sql').read_text())
    assert db.execute("SELECT photo.artifact_has_references(%s,%s)",
                      (tid, artifact)).fetchone()[0]
    db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' "
               "WHERE id=%s", (artifact,))
    assert db.execute("SELECT count(*) FROM photo.claim_artifact_gc()").fetchone()[0] == 0
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("INSERT INTO photo.v2_document_refs(tenant_id,project_id,document_id,artifact_id,"
                   "sha256,purpose,ordinal) VALUES(%s,%s,%s,%s,%s,'source',0)",
                   (tid, project, document, artifact, 'c' * 64))


def test_v21_task_input_closure_is_bound_to_workflow_project(db):
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_run_kind.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_schema.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_project_guards.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_artifact_guards.sql').read_text())
    tid, user = tenant(db, str(uuid4()) + '@example.test')
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'One') RETURNING id",
                         (tid,)).fetchone()[0]
    other = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Two') RETURNING id",
                       (tid,)).fetchone()[0]
    artifact = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,"
                          "media_type,object_version,verified_at) VALUES(%s,'test',%s,%s,2,"
                          "'application/json','v1',clock_timestamp()) RETURNING id",
                          (tid, str(uuid4()), 'a' * 64)).fetchone()[0]
    snapshot = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) "
                          "VALUES(%s,'v2','user',%s,%s,0) RETURNING id",
                          (tid, user, 'b' * 64)).fetchone()[0]
    run = db.execute("INSERT INTO photo.runs(tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,"
                     "created_by,failure_policy) VALUES(%s,'v2',%s,%s,%s,%s,'fail_fast') RETURNING id",
                     (tid, snapshot, Jsonb({'project_id': str(project)}), 'c' * 64, user)).fetchone()[0]
    db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,"
               "workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'preview','v2',%s)",
               (tid, project, run, snapshot, 'd' * 64))
    task = db.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,"
                      "resource_class,inputs,params_artifact_id,input_digest,execution_profile) "
                      "VALUES(%s,%s,'preview/0','editor.preview','1','cpu','[]',%s,%s,'{}') RETURNING id",
                      (tid, run, artifact, 'e' * 64)).fetchone()[0]
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,"
                   "contract,ordinal) VALUES(%s,%s,%s,%s,%s,'WorkingImage',0)",
                   (tid, other, task, artifact, 'a' * 64))
    db.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,"
               "contract,ordinal) VALUES(%s,%s,%s,%s,%s,'WorkingImage',0)",
               (tid, project, task, artifact, 'a' * 64))


def test_v21_approval_requires_current_draft_and_active_editor(db):
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_schema.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_project_guards.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_review_guards.sql').read_text())
    tid, user = tenant(db, str(uuid4()) + '@example.test')
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Draft') RETURNING id",
                         (tid,)).fetchone()[0]
    artifact = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,"
                          "media_type,object_version,verified_at) VALUES(%s,'test',%s,%s,2,"
                          "'application/json','v1',clock_timestamp()) RETURNING id",
                          (tid, str(uuid4()), 'a' * 64)).fetchone()[0]
    asset = db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,"
                       "upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",
                       (tid, project, artifact)).fetchone()[0]
    version = db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,"
                         "content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",
                         (tid, asset, artifact, 'a' * 64)).fetchone()[0]
    stream = db.execute("INSERT INTO photo.v2_document_streams(tenant_id,project_id,stream_type,asset_version_id) "
                        "VALUES(%s,%s,'draft',%s) RETURNING id", (tid, project, version)).fetchone()[0]
    document = db.execute("INSERT INTO photo.v2_documents(tenant_id,project_id,stream_id,version_no,"
                          "document_type,artifact_id,sha256) VALUES(%s,%s,%s,1,'EditingRecipeV3',%s,%s) RETURNING id",
                          (tid, project, stream, artifact, 'a' * 64)).fetchone()[0]
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("INSERT INTO photo.v2_document_approvals(tenant_id,project_id,document_id,actor_id,"
                   "recipe_sha,resolved_artifact_id) VALUES(%s,%s,%s,%s,%s,%s)",
                   (tid, project, document, user, 'a' * 64, artifact))
    db.execute("UPDATE photo.v2_document_streams SET current_document_id=%s WHERE id=%s", (document, stream))
    db.execute("INSERT INTO photo.v2_document_approvals(tenant_id,project_id,document_id,actor_id,"
               "recipe_sha,resolved_artifact_id) VALUES(%s,%s,%s,%s,%s,%s)",
               (tid, project, document, user, 'a' * 64, artifact))


def test_v21_tenant_purge_breaks_document_pointer_cycle(db):
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_schema.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_draft_publication.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_guards.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_project_guards.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_purge.sql').read_text())
    tid, user = tenant(db, str(uuid4()) + '@example.test')
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Purge') RETURNING id",
                         (tid,)).fetchone()[0]
    artifact = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,"
                          "media_type,object_version,verified_at) VALUES(%s,'test',%s,%s,2,"
                          "'application/json','v1',clock_timestamp()) RETURNING id",
                          (tid, str(uuid4()), 'a' * 64)).fetchone()[0]
    stream = db.execute("INSERT INTO photo.v2_document_streams(tenant_id,project_id,stream_type) "
                        "VALUES(%s,%s,'shot_list') RETURNING id", (tid, project)).fetchone()[0]
    document = db.execute("INSERT INTO photo.v2_documents(tenant_id,project_id,stream_id,version_no,"
                          "document_type,artifact_id,sha256) VALUES(%s,%s,%s,1,'ShotList',%s,%s) RETURNING id",
                          (tid, project, stream, artifact, 'a' * 64)).fetchone()[0]
    db.execute("UPDATE photo.v2_document_streams SET current_document_id=%s,approved_document_id=%s "
               "WHERE id=%s", (document, document, stream))
    db.execute("UPDATE photo.tenants SET status='deleting' WHERE id=%s", (tid,))
    job = db.execute("INSERT INTO photo.deletion_jobs(tenant_id,resource_type,resource_id,requested_by) "
                     "VALUES(%s,'tenant',%s,%s) RETURNING id", (tid, tid, user)).fetchone()[0]
    token = db.execute("SELECT cleanup_token FROM photo.claim_tenant_deletion() WHERE job_id=%s",
                       (job,)).fetchone()[0]
    assert db.execute("SELECT photo.finish_tenant_deletion(%s,%s)", (job, token)).fetchone()[0]
    assert db.execute("SELECT count(*) FROM photo.v2_documents WHERE tenant_id=%s", (tid,)).fetchone()[0] == 0
    assert db.execute("SELECT count(*) FROM photo.v2_document_streams WHERE tenant_id=%s", (tid,)).fetchone()[0] == 0


def test_v21_editor_authority_rechecks_session_and_project_acl(db):
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_authority.sql').read_text())
    tid, owner = tenant(db, str(uuid4()) + '@example.test')
    project = db.execute("INSERT INTO photo.projects(tenant_id,name,visibility) "
                         "VALUES(%s,'Restricted','restricted') RETURNING id", (tid,)).fetchone()[0]
    editor = db.execute("INSERT INTO photo.users(email_normalized) VALUES(%s) RETURNING id",
                        (str(uuid4()) + '@example.test',)).fetchone()[0]
    db.execute("INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES(%s,%s,'editor')", (tid, editor))
    def session(actor):
        return db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) "
                          "VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') "
                          "RETURNING id", (actor, uuid4().bytes)).fetchone()[0]
    owner_session, editor_session = session(owner), session(editor)
    check = "SELECT photo.v2_editor_authorized(%s,%s,%s,%s)"
    assert db.execute(check, (tid, project, owner, owner_session)).fetchone()[0]
    assert not db.execute(check, (tid, project, editor, editor_session)).fetchone()[0]
    db.execute("INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) "
               "VALUES(%s,%s,%s,'editor')", (tid, project, editor))
    assert db.execute(check, (tid, project, editor, editor_session)).fetchone()[0]
    db.execute("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s", (editor_session,))
    assert not db.execute(check, (tid, project, editor, editor_session)).fetchone()[0]


def test_v21_draft_upload_is_quota_backed_fenced_and_replay_safe(db):
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_schema.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_authority.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_draft_publication.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_draft_scope.sql').read_text())
    tid, owner = tenant(db, str(uuid4()) + '@example.test')
    db.execute("INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)", (tid,))
    session = db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) "
                         "VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') "
                         "RETURNING id", (owner, uuid4().bytes)).fetchone()[0]
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Draft upload') RETURNING id",
                         (tid,)).fetchone()[0]
    source = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,"
                        "media_type,object_version,verified_at) VALUES(%s,'test',%s,%s,2,"
                        "'image/jpeg','v1',clock_timestamp()) RETURNING id",
                        (tid, str(uuid4()), 'a' * 64)).fetchone()[0]
    asset = db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,"
                       "upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",
                       (tid, project, source)).fetchone()[0]
    version = db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,"
                         "content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",
                         (tid, asset, source, 'a' * 64)).fetchone()[0]
    document = uuid4()
    reserve = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v2_draft_upload(%s,%s,%s,%s,%s,%s,%s,%s)"
    complete = "SELECT photo.complete_v2_draft_upload(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)"
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tid),))
    db.execute('SET LOCAL ROLE photo_api')
    created = db.execute(reserve, (tid, version, document, owner, session, 'b' * 64, 100, 'test')).fetchone()
    assert created[0] == 'created' and not created[3]
    resumed = db.execute(reserve, (tid, version, document, owner, session, 'b' * 64, 100, 'test')).fetchone()
    assert resumed[0] == 'resumed' and resumed[1] == created[1] and resumed[2] != created[2]
    args = (tid, version, document, owner, session, created[1], created[2], 'b' * 64, 100, 'test', 'v1')
    assert not db.execute(complete, args).fetchone()[0]
    args = (*args[:6], resumed[2], *args[7:])
    assert db.execute(complete, args).fetchone()[0]
    assert db.execute(complete, args).fetchone()[0]
    replayed = db.execute(reserve, (tid, version, document, owner, session, 'b' * 64, 100, 'test')).fetchone()
    assert replayed[0] == 'replayed' and replayed[3]
    assert db.execute("SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s",
                      (tid,)).fetchone() == (100, 0)
    assert db.execute("SELECT output_role,object_key FROM photo.artifacts WHERE id=%s",
                      (created[1],)).fetchone() == ('document',
                      f'tenants/{tid}/v2-drafts/{created[1]}')
    other_project = db.execute("INSERT INTO photo.projects(tenant_id,name) "
                               "VALUES(%s,'Unrelated') RETURNING id", (tid,)).fetchone()[0]
    scoped = "SELECT photo.v2_draft_artifact_scope(%s,%s,%s,%s,%s,%s,%s,%s)"
    assert db.execute(scoped, (tid, project, version, created[1], 'b' * 64,
                               'LayerStack', owner, session)).fetchone()[0]
    assert not db.execute(scoped, (tid, other_project, version, created[1],
                                   'b' * 64, 'LayerStack', owner, session)).fetchone()[0]
    # Composition parents require a dedicated admitted-result provenance path.
    # A ready project upload cannot impersonate a typed parent/report.
    for contract in ('ImageResult', 'ImageReport', 'IdentityReport', 'Calibration'):
        assert not db.execute(scoped, (tid, project, version, created[1],
                                      'b' * 64, contract, owner, session)).fetchone()[0]
    style_artifact = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,"
        "media_type,object_version,verified_at) VALUES(%s,'test',%s,%s,2,'application/json',"
        "'v1',clock_timestamp()) RETURNING id", (tid, str(uuid4()), 'd' * 64)).fetchone()[0]
    style = db.execute("INSERT INTO photo.styles(tenant_id,project_id,name,created_by) "
                       "VALUES(%s,%s,'Scoped',%s) RETURNING id", (tid, project, owner)).fetchone()[0]
    db.execute("INSERT INTO photo.style_versions(tenant_id,style_id,version_no,config_json,"
               "config_hash,schema_version,renderer_version,approval_status,metrics,export_artifact_id) "
               "VALUES(%s,%s,1,'{}',%s,'2.0.0','photo_renderer_v2','accepted','{}',%s)",
               (tid, style, 'd' * 64, style_artifact))
    assert db.execute(scoped, (tid, project, version, style_artifact, 'd' * 64,
                               'StyleV2', owner, session)).fetchone()[0]
    assert not db.execute(scoped, (tid, other_project, version, style_artifact,
                                   'd' * 64, 'StyleV2', owner, session)).fetchone()[0]
    db.execute('RESET ROLE')
    pending_doc = uuid4()
    db.execute('SET LOCAL ROLE photo_api')
    pending = db.execute(reserve, (tid, version, pending_doc, owner, session,
                                   'c' * 64, 40, 'test')).fetchone()
    db.execute('RESET ROLE')
    assert pending[0] == 'created'
    db.execute("UPDATE photo.v2_draft_uploads SET cleanup_after=clock_timestamp()-interval '1 second' "
               "WHERE tenant_id=%s AND document_id=%s", (tid, pending_doc))
    db.execute('SET LOCAL ROLE photo_maintenance')
    ticket = db.execute("SELECT tenant_id,document_id,artifact_id,bucket,cleanup_token "
                        "FROM photo.claim_v2_draft_upload_cleanup()").fetchone()
    db.execute('RESET ROLE')
    assert ticket[:4] == (tid, pending_doc, pending[1], 'test')
    db.execute('SET LOCAL ROLE photo_api')
    assert not db.execute(complete, (tid, version, pending_doc, owner, session,
        pending[1], pending[2], 'c' * 64, 40, 'test', 'v1')).fetchone()[0]
    assert db.execute(reserve, (tid, version, pending_doc, owner, session,
        'c' * 64, 40, 'test')).fetchone()[0] == 'stale'
    db.execute('RESET ROLE')
    db.execute('SET LOCAL ROLE photo_maintenance')
    finish = "SELECT photo.finish_v2_draft_upload_cleanup(%s,%s,%s)"
    assert not db.execute(finish, (tid, pending_doc, uuid4())).fetchone()[0]
    assert db.execute(finish, (tid, pending_doc, ticket[4])).fetchone()[0]
    db.execute('RESET ROLE')
    assert db.execute("SELECT state FROM photo.v2_draft_uploads WHERE tenant_id=%s AND document_id=%s",
                      (tid, pending_doc)).fetchone()[0] == 'abandoned'
    assert db.execute("SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s",
                      (tid,)).fetchone() == (100, 0)
    db.execute("UPDATE photo.v2_draft_uploads SET completed_at=clock_timestamp()-interval '25 hours' "
               "WHERE tenant_id=%s AND document_id=%s", (tid, document))
    db.execute('SET LOCAL ROLE photo_maintenance')
    assert db.execute("SELECT photo.retire_one_v2_draft_upload()").fetchone()[0]
    db.execute('RESET ROLE')
    assert db.execute("SELECT state FROM photo.v2_draft_uploads WHERE tenant_id=%s AND document_id=%s",
                      (tid, document)).fetchone()[0] == 'retired'
    db.execute("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s", (session,))
    db.execute('SET LOCAL ROLE photo_api')
    denied = db.execute(reserve, (tid, version, uuid4(), owner, session,
                                  'c' * 64, 100, 'test')).fetchone()
    assert denied[0] == 'forbidden' and denied[1] is None
    assert not db.execute(complete, args).fetchone()[0]
    db.execute('RESET ROLE')


def test_v21_draft_first_save_and_cas_preserve_history(db):
    for name in ('00196_schema.sql','00196_draft_indexes.sql','00196_guards.sql',
                 '00196_project_guards.sql','00196_artifact_guards.sql','00196_authority.sql',
                 '00196_draft_publication.sql',
                 '00196_draft_refs.sql','00196_review_guards.sql',
                 '00196_draft_mutations.sql','00196_draft_approval.sql'):
        db.execute((ROOT / 'backend/db/v2-scaffolds' / name).read_text())
    tid, owner = tenant(db, str(uuid4()) + '@example.test')
    session = db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) "
                         "VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') "
                         "RETURNING id", (owner, uuid4().bytes)).fetchone()[0]
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Draft') RETURNING id",
                         (tid,)).fetchone()[0]
    def artifact(sha):
        return db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,"
                          "media_type,object_version,verified_at) VALUES(%s,'test',%s,%s,2,"
                          "'application/json','v1',clock_timestamp()) RETURNING id",
                          (tid, str(uuid4()), sha)).fetchone()[0]
    original, base, first, second, resolved = (artifact(char * 64) for char in 'abcde')
    asset = db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,"
                       "upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",
                       (tid, project, original)).fetchone()[0]
    version = db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,"
                         "content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",
                         (tid, asset, original, 'a' * 64)).fetchone()[0]
    base_doc, first_doc, second_doc = uuid4(), uuid4(), uuid4()
    def published(document_id, artifact_id, sha):
        db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,"
                   "asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at) "
                   "VALUES(%s,%s,%s,%s,%s,%s,2,'test','ready','v1',clock_timestamp())",
                   (tid, document_id, project, version, artifact_id, sha))
    published(base_doc, base, 'b' * 64)
    published(first_doc, first, 'c' * 64)
    published(second_doc, second, 'd' * 64)
    closure = Jsonb([{'artifact_id': str(original), 'sha256': 'a' * 64,
                      'purpose': 'f' * 64, 'ordinal': 0}])
    call = "SELECT photo.replace_v2_draft(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)"
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tid),))
    db.execute('SET LOCAL ROLE photo_api')
    saved = db.execute(call, (tid, version, 0, first_doc, first, 'c' * 64, owner, session,
                              closure, base_doc, base, 'b' * 64, closure)).fetchone()[0]
    assert saved['revision'] == '2' and saved['document_id'] == str(first_doc)
    updated = db.execute(call, (tid, version, 2, second_doc, second, 'd' * 64, owner, session,
                                closure, None, None, None, Jsonb([]))).fetchone()[0]
    assert updated['id'] == saved['id'] and updated['revision'] == '3'
    with pytest.raises(errors.SerializationFailure), db.transaction():
        db.execute(call, (tid, version, 2, uuid4(), second, 'd' * 64, owner, session,
                          closure, None, None, None, Jsonb([])))
    approve = "SELECT photo.approve_v2_draft(%s,%s,%s,%s,%s,%s,%s,%s)"
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute(approve, (tid, version, 3, second_doc, owner, session,
                            resolved, 'e' * 64))
    db.execute('RESET ROLE')
    db.execute("UPDATE photo.artifacts SET output_role='document' WHERE tenant_id=%s AND id=%s",
               (tid, resolved))
    published(uuid4(), resolved, 'e' * 64)
    db.execute('SET LOCAL ROLE photo_api')
    decision = db.execute(approve, (tid, version, 3, second_doc, owner, session,
                                    resolved, 'e' * 64)).fetchone()[0]
    assert decision['revision'] == '4' and decision['document_id'] == str(second_doc)
    replay = db.execute(approve, (tid, version, 3, second_doc, owner, session,
                                  resolved, 'e' * 64)).fetchone()[0]
    assert replay == decision
    assert db.execute("SELECT count(*) FROM photo.audit_log WHERE tenant_id=%s "
                      "AND actor_id=%s AND action='v2_draft.approved' "
                      "AND resource_id=%s", (tid, owner, second_doc)).fetchone()[0] == 1
    with pytest.raises(errors.SerializationFailure), db.transaction():
        db.execute(approve, (tid, version, 4, first_doc, owner, session,
                             resolved, 'e' * 64))
    db.execute('RESET ROLE')
    with pytest.raises(errors.UniqueViolation), db.transaction():
        db.execute("INSERT INTO photo.v2_document_streams(tenant_id,project_id,asset_version_id,stream_type) "
                   "VALUES(%s,%s,%s,'draft')", (tid, project, version))
    assert db.execute("SELECT version_no FROM photo.v2_documents WHERE stream_id=%s ORDER BY version_no",
                      (saved['id'],)).fetchall() == [(1,), (2,), (3,)]
    assert db.execute("SELECT count(*) FROM photo.v2_document_refs WHERE document_id IN (%s,%s,%s)",
                      (base_doc, first_doc, second_doc)).fetchone()[0] == 3
    layer_artifact, third_artifact = artifact('f' * 64), artifact('1' * 64)
    layer_doc, third_doc = uuid4(), uuid4()
    published(layer_doc, layer_artifact, 'f' * 64)
    published(third_doc, third_artifact, '1' * 64)
    layer_closure = Jsonb([{'artifact_id': str(layer_artifact), 'sha256': 'f' * 64,
                           'purpose': 'e' * 64, 'ordinal': 0}])
    layer_refs = Jsonb([{'artifact_id': str(original), 'sha256': 'a' * 64,
                        'purpose': 'd' * 64, 'ordinal': 0}])
    layered_call = "SELECT photo.replace_v2_draft(" + ",".join(["%s"] * 17) + ")"
    db.execute('SET LOCAL ROLE photo_api')
    layered = db.execute(layered_call, (tid, version, 4, third_doc, third_artifact,
        '1' * 64, owner, session, layer_closure, None, None, None, Jsonb([]),
        layer_doc, layer_artifact, 'f' * 64, layer_refs)).fetchone()[0]
    db.execute('RESET ROLE')
    assert layered['revision'] == '5' and layered['document_id'] == str(third_doc)
    assert db.execute("SELECT version_no,document_type FROM photo.v2_documents "
                      "WHERE stream_id=%s ORDER BY version_no", (saved['id'],)).fetchall() == [
                          (1, 'EditingRecipeV3'), (2, 'EditingRecipeV3'),
                          (3, 'EditingRecipeV3'), (4, 'LayerStack'),
                          (5, 'EditingRecipeV3')]
    bad_recipe_doc, bad_recipe_artifact = uuid4(), artifact('2' * 64)
    published(bad_recipe_doc, bad_recipe_artifact, '2' * 64)
    db.execute('SET LOCAL ROLE photo_api')
    with pytest.raises(errors.InvalidParameterValue), db.transaction():
        db.execute(layered_call, (tid, version, 5, bad_recipe_doc, bad_recipe_artifact,
            '2' * 64, owner, session, closure, None, None, None, Jsonb([]),
            uuid4(), layer_artifact, 'f' * 64, layer_refs))
    db.execute('RESET ROLE')
    db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,version_no,master_artifact_id,"
               "content_hash,metadata,review_state) VALUES(%s,%s,2,%s,%s,'{}','accepted')",
               (tid, asset, original, 'a' * 64))
    db.execute('SET LOCAL ROLE photo_api')
    assert db.execute("SELECT outcome FROM photo.reserve_v2_draft_upload("
                      "%s,%s,%s,%s,%s,%s,%s,%s)",
                      (tid, version, uuid4(), owner, session, '3' * 64, 2, 'test')).fetchone()[0] == 'conflict'
    with pytest.raises(errors.SerializationFailure), db.transaction():
        db.execute(call, (tid, version, 5, uuid4(), third_artifact, '1' * 64,
                          owner, session, closure, None, None, None, Jsonb([])))
    with pytest.raises(errors.SerializationFailure), db.transaction():
        db.execute(approve, (tid, version, 5, third_doc, owner, session,
                             resolved, 'e' * 64))
    db.execute('RESET ROLE')


def test_v21_output_policy_extends_only_explicit_v2_roles(db):
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_schema.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_graph_seal.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_output_protocol.sql').read_text())
    db.execute("INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,"
               "timeout_seconds,backends) VALUES('session.plan','1','cpu','cpu.session_plan',600,'[\"cpu\"]')")
    db.execute("INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) "
               "VALUES('session.plan','1','correction_plan','[\"application/json\"]',8388608,5000)")
    item = setup_output(db)
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("UPDATE photo.operation_output_policies SET max_count=129 "
                   "WHERE operation='asset.verify' AND role='verified'")
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("INSERT INTO photo.operation_output_policies(operation,version,role,media_types,"
                   "max_bytes,max_count) VALUES('session.plan','1','draft','[\"application/json\"]',"
                   "8388608,5000)")
    db.execute('SET LOCAL ROLE photo_gateway')
    assert begin_output(db, item, uuid4())[0] == 'created'


def test_v21_v2_upload_fails_closed_without_renderer_binding(db):
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_run_kind.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_schema.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_graph_seal.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_output_protocol.sql').read_text())
    item = setup_output(db)
    db.execute("UPDATE photo.runs SET kind='v2',failure_policy='fail_fast' WHERE id=%s", (item['run'],))
    db.execute('SET LOCAL ROLE photo_gateway')
    assert begin_output(db, item, uuid4())[0] == 'forbidden'


def test_v21_gateway_extension_preserves_legacy_exact_scope(db):
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_schema.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_graph_seal.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_gateway_input.sql').read_text())
    item, artifact = setup_input(db)
    unrelated = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,"
                           "sha256,bytes,media_type) VALUES(%s,'test',%s,'version-2',%s,3,'image/png') "
                           "RETURNING id", (item['tenant'], str(uuid4()), 'a' * 64)).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_gateway')
    assert read_input(db, item, artifact)[0] == artifact
    assert read_input(db, item, unrelated) is None


def test_v21_task_result_requires_verified_v2_document_root(db):
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_run_kind.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_schema.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_result_root.sql').read_text())
    item = queued_task(db)
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Result') RETURNING id",
                         (item['tenant'],)).fetchone()[0]
    snapshot = db.execute("SELECT input_snapshot_id FROM photo.runs WHERE id=%s",
                          (item['run'],)).fetchone()[0]
    db.execute("UPDATE photo.runs SET kind='v2',failure_policy='fail_fast' WHERE id=%s", (item['run'],))
    db.execute("UPDATE photo.tasks SET execution_profile=execution_profile || %s WHERE id=%s",
               (Jsonb({'renderer_version': 'v2.2'}), item['task']))
    db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,"
               "workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'preview','v2.2',%s)",
               (item['tenant'], project, item['run'], snapshot, 'a' * 64))
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,"
                   "metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s)",
                   (item['tenant'], item['task'], Jsonb([]), 'b' * 64))
    unrelated = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,"
                           "media_type,object_version,verified_at) VALUES(%s,'test',%s,%s,2,"
                           "'application/json','v1',clock_timestamp()) RETURNING id",
                           (item['tenant'], str(uuid4()), 'c' * 64)).fetchone()[0]
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,"
                   "metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s)",
                   (item['tenant'], item['task'], Jsonb([{'artifact_id': str(unrelated),
                                                          'sha256': 'c' * 64}]), 'b' * 64))


def test_v21_worker_renderer_gate_rejects_legacy_worker(db):
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_run_kind.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_schema.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_input_closure_gate.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_graph_seal.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_worker_renderer_gate.sql').read_text())
    item = queued_task(db)
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'V2 worker') RETURNING id",
                         (item['tenant'],)).fetchone()[0]
    snapshot = db.execute("SELECT input_snapshot_id FROM photo.runs WHERE id=%s",
                          (item['run'],)).fetchone()[0]
    db.execute("UPDATE photo.runs SET kind='v2',failure_policy='fail_fast',"
               "config_snapshot=%s WHERE id=%s", (Jsonb({'project_id': str(project)}), item['run']))
    db.execute("UPDATE photo.tasks SET execution_profile=execution_profile || %s WHERE id=%s",
               (Jsonb({'renderer_version': 'v2.2'}), item['task']))
    db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,"
               "workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'preview','v2.2',%s)",
               (item['tenant'], project, item['run'], snapshot, 'a' * 64))
    db.execute('SET LOCAL ROLE photo_worker')
    assert claim(db, item)[0] == 'unauthorized'
    db.execute('RESET ROLE')
    db.execute("UPDATE photo.worker_registrations SET v2_renderer_versions='[\"v2.2\"]' "
               "WHERE worker_id=%s", (item['worker'],))
    db.execute('SET LOCAL ROLE photo_worker')
    assert claim(db, item)[0] == 'unauthorized'
    db.execute('RESET ROLE')
    seal_v2_test_closure(db, item, project)
    db.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='asset.verify' AND version='1'")
    db.execute('SET LOCAL ROLE photo_worker')
    assert claim(db, item)[0] == 'claimed'


def test_v21_dispatch_selects_only_renderer_qualified_worker(db):
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_run_kind.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_schema.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_input_closure_gate.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_graph_seal.sql').read_text())
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_worker_renderer_gate.sql').read_text())
    item = ready_task(db)
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Dispatch') RETURNING id",
                         (item['tenant'],)).fetchone()[0]
    snapshot = db.execute("SELECT input_snapshot_id FROM photo.runs WHERE id=%s",
                          (item['run'],)).fetchone()[0]
    db.execute("UPDATE photo.runs SET kind='v2',failure_policy='fail_fast' WHERE id=%s", (item['run'],))
    db.execute("UPDATE photo.tasks SET execution_profile=execution_profile || %s WHERE id=%s",
               (Jsonb({'renderer_version': 'v2.2'}), item['task']))
    db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,"
               "workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'preview','v2.2',%s)",
               (item['tenant'], project, item['run'], snapshot, 'a' * 64))
    db.execute("UPDATE photo.worker_registrations SET v2_renderer_versions='[\"v2.2\"]' "
               "WHERE worker_id=%s", (item['worker'],))
    db.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='asset.verify' AND version='1'")
    db.execute('SET LOCAL ROLE photo_scheduler')
    with pytest.raises(errors.CheckViolation, match='V2_INPUT_CLOSURE_UNSEALED'), db.transaction():
        reserve(db, item)
    db.execute('RESET ROLE')
    assert db.execute("SELECT state FROM photo.tasks WHERE id=%s", (item['task'],)).fetchone()[0] == 'ready'
    assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s",
                      (item['tenant'],)).fetchone()[0] == 0
    db.execute("UPDATE photo.worker_registrations SET v2_renderer_versions='[]' "
               "WHERE worker_id=%s", (item['worker'],))
    seal_v2_test_closure(db, item, project)
    db.execute('SET LOCAL ROLE photo_scheduler')
    with pytest.raises(errors.SerializationFailure), db.transaction():
        reserve(db, item)
    db.execute('RESET ROLE')
    db.execute("UPDATE photo.worker_registrations SET v2_renderer_versions='[\"v2.2\"]' "
               "WHERE worker_id=%s", (item['worker'],))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert reserve(db, item)[0] == 'queued'


def test_v21_adoption_is_fenced_atomic_and_replay_safe(db):
    for name in ('00196_run_kind.sql', '00196_schema.sql', '00196_artifact_guards.sql',
                 '00196_result_root.sql', '00196_adoption.sql',
                 '00196_adoption_reads.sql', '00196_finalize.sql'):
        db.execute((ROOT / 'backend/db/v2-scaffolds' / name).read_text())
    item = setup_output(db)
    db.execute("INSERT INTO photo.operation_output_policies(operation,version,role,media_types,"
               "max_bytes,max_count) VALUES('asset.verify','1','document','[\"application/json\"]',100,1)")
    db.execute('SET LOCAL ROLE photo_gateway')
    ticket = begin_output(db, item, uuid4(), role='document')
    assert ticket[0] == 'created'
    assert complete_output(db, item, ticket[1])[0] == 'committed'
    db.execute('RESET ROLE')
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Adoption') RETURNING id",
                         (item['tenant'],)).fetchone()[0]
    snapshot = db.execute("SELECT input_snapshot_id FROM photo.runs WHERE id=%s",
                          (item['run'],)).fetchone()[0]
    db.execute("UPDATE photo.runs SET kind='v2',failure_policy='fail_fast' WHERE id=%s", (item['run'],))
    db.execute("UPDATE photo.tasks SET execution_profile=execution_profile || %s WHERE id=%s",
               (Jsonb({'renderer_version': 'v2.2'}), item['task']))
    db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,"
               "workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'preview','v2.2',%s)",
               (item['tenant'], project, item['run'], snapshot, 'd' * 64))
    db.execute('SET LOCAL ROLE photo_worker')
    result = commit(db, item, 1, manifest([{'artifact_id': str(ticket[2]), 'sha256': 'a' * 64}]))
    assert result[0] == 'committed'
    db.execute('RESET ROLE')
    assert db.execute('SELECT photo.v2_adoption_event_id(%s,%s)',
                      (item['task'], result[1])).fetchone()[0] == uuid5(
                          item['task'], 'adopt:' + str(result[1]))
    inventory = [{'artifact_id': str(ticket[2]), 'sha256': 'a' * 64,
                  'purpose': 'e' * 64, 'ordinal': 0}]
    creator = db.execute('SELECT created_by FROM photo.runs WHERE id=%s', (item['run'],)).fetchone()[0]
    db.execute("INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,"
               "holder_type,holder_id,bytes,files,expires_at) VALUES(%s,'user',%s,'run',%s,10,0,"
               "clock_timestamp()+interval '1 hour')", (item['tenant'], creator, item['run']))
    db.execute("UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+10 WHERE tenant_id=%s",
               (item['tenant'],))
    unrelated = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,"
                           "media_type,object_version,verified_at) VALUES(%s,'test',%s,%s,2,"
                           "'application/json','v1',clock_timestamp()) RETURNING id",
                           (item['tenant'], str(uuid4()), 'f' * 64)).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_scheduler')
    evidence = db.execute('SELECT * FROM photo.v2_adoption_artifact(%s,%s,1,1,%s)',
                          (item['tenant'], item['task'], ticket[2])).fetchone()
    assert evidence[3] == 'a' * 64 and evidence[6:] == ('document', 'output')
    assert db.execute('SELECT * FROM photo.v2_adoption_artifact(%s,%s,1,1,%s)',
                      (item['tenant'], item['task'], unrelated)).fetchone() is None
    assert db.execute('SELECT artifact_id FROM photo.v2_adoption_staged_outputs(%s,%s,1,1)',
                      (item['tenant'], item['task'])).fetchone()[0] == ticket[2]
    assert (item['tenant'], item['run'], item['task']) in db.execute(
        'SELECT * FROM photo.v2_pending_adoptions()').fetchall()
    context = db.execute('SELECT * FROM photo.v2_adoption_context(%s,%s)',
                         (item['tenant'], item['task'])).fetchone()
    assert context[2] == result[1] and context[5] == ticket[2]
    db.execute('RESET ROLE')
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute('SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,%s,%s)',
                   (item['tenant'], item['task'], result[1],
                    Jsonb(inventory + [{'artifact_id': str(unrelated), 'sha256': 'f' * 64,
                                        'purpose': 'f' * 64, 'ordinal': 1}]), '0' * 64))
    assert db.execute("SELECT count(*) FROM photo.v2_artifact_links WHERE tenant_id=%s",
                      (item['tenant'],)).fetchone()[0] == 0
    db.execute('SET LOCAL ROLE photo_scheduler')
    args = (item['tenant'], item['task'], result[1], Jsonb(inventory), '0' * 64)
    assert db.execute('SELECT photo.finalize_v2_run(%s,%s)',
                      (item['tenant'], item['run'])).fetchone()[0] == 'pending'
    assert not db.execute('SELECT photo.adopt_v2_plan(%s,%s,1,2,%s,%s,%s)', args).fetchone()[0]
    db.execute('RESET ROLE')
    db.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s', (item['run'],))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert not db.execute('SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,%s,%s)', args).fetchone()[0]
    db.execute('RESET ROLE')
    db.execute('UPDATE photo.runs SET cancel_requested=false WHERE id=%s', (item['run'],))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,%s,%s)', args).fetchone()[0]
    assert (item['tenant'], item['run']) in db.execute(
        'SELECT * FROM photo.v2_pending_finalizations()').fetchall()
    assert not db.execute('SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,%s,%s)', args).fetchone()[0]
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute('SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,%s,%s)', args[:-1] + ('1' * 64,))
    assert db.execute('SELECT photo.finalize_v2_run(%s,%s)',
                      (item['tenant'], item['run'])).fetchone()[0] == 'completed'
    assert db.execute('SELECT photo.finalize_v2_run(%s,%s)',
                      (item['tenant'], item['run'])).fetchone()[0] == 'completed'
    db.execute('RESET ROLE')
    assert db.execute("SELECT count(*) FROM photo.v2_artifact_links WHERE tenant_id=%s",
                      (item['tenant'],)).fetchone()[0] == 1
    assert db.execute("SELECT count(*) FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt'",
                      (item['tenant'],)).fetchone()[0] == 1
    assert db.execute("SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s",
                      (item['tenant'],)).fetchone()[0] == 0
    assert db.execute("SELECT state FROM photo.quota_reservations WHERE holder_id=%s",
                      (item['run'],)).fetchone()[0] == 'consumed'


def test_v21_assembled_migration_applies_as_one_transaction(db):
    assembled = (ROOT / 'backend/db/v2-scaffolds/00196_assembled.sql').read_text()
    db.execute(assembled.split('-- +goose Down')[0])
    assert db.execute("SELECT to_regclass('photo.v2_task_inputs') IS NOT NULL").fetchone()[0]
    assert db.execute("SELECT to_regprocedure('photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)') "
                      "IS NOT NULL").fetchone()[0]
    assert db.execute("SELECT to_regprocedure('photo.finalize_v2_run(uuid,uuid)') "
                      "IS NOT NULL").fetchone()[0]


def test_v21_invalid_adoption_drains_to_failed_without_marker(db):
    for name in ('00196_run_kind.sql', '00196_schema.sql', '00196_adoption.sql',
                 '00196_adoption_reads.sql', '00196_finalize.sql'):
        db.execute((ROOT / 'backend/db/v2-scaffolds' / name).read_text())
    item = queued_task(db)
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Rejected') RETURNING id",
                         (item['tenant'],)).fetchone()[0]
    snapshot = db.execute("SELECT input_snapshot_id FROM photo.runs WHERE id=%s",
                          (item['run'],)).fetchone()[0]
    db.execute("UPDATE photo.runs SET kind='v2',failure_policy='fail_fast' WHERE id=%s", (item['run'],))
    db.execute("UPDATE photo.tasks SET execution_profile=execution_profile || %s WHERE id=%s",
               (Jsonb({'renderer_version': 'v2.2'}), item['task']))
    db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,"
               "workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'preview','v2.2',%s)",
               (item['tenant'], project, item['run'], snapshot, 'a' * 64))
    result = db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,"
                        "outputs,metrics,result_digest) VALUES(%s,%s,1,1,'[]','{}',%s) RETURNING id",
                        (item['tenant'], item['task'], 'b' * 64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',attempt=1,fencing_token=1,"
               "canonical_result_id=%s WHERE id=%s", (result, item['task']))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.reject_v2_adoption(%s,%s,%s,%s)',
                      (item['tenant'], item['task'], result, 'ARTIFACT_SCHEMA_INVALID')).fetchone()[0]
    assert not db.execute('SELECT photo.reject_v2_adoption(%s,%s,%s,%s)',
                          (item['tenant'], item['task'], result, 'ARTIFACT_SCHEMA_INVALID')).fetchone()[0]
    assert (item['tenant'], item['run']) in db.execute(
        'SELECT * FROM photo.v2_pending_finalizations()').fetchall()
    assert db.execute('SELECT photo.finalize_v2_run(%s,%s)',
                      (item['tenant'], item['run'])).fetchone()[0] == 'failed'
    db.execute('RESET ROLE')
    assert db.execute("SELECT count(*) FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt'",
                      (item['tenant'],)).fetchone()[0] == 0
