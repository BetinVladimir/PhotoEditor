"""V22 schema slice installs after the complete V21 foundation."""
import hashlib
import json
from pathlib import Path
from uuid import UUID, uuid4, uuid5

import pytest
from psycopg import errors
from psycopg.types.json import Jsonb

from test_database import tenant


ROOT = Path(__file__).resolve().parents[2]
NAMES = ('shoot_sessions', 'session_analyses', 'scene_groups', 'scene_members',
         'project_persons', 'person_observations', 'consent_grants',
         'consent_events', 'workflow_consent_links')


def test_v22_tables_constraints_and_privilege_boundary(db):
    db.execute((ROOT / 'backend/db/v2-scaffolds/00196_assembled.sql').read_text()
               .split('-- +goose Down')[0])
    db.execute((ROOT / 'backend/db/v2-scaffolds/00197_assembled.sql').read_text()
               .split('-- +goose Down')[0])
    for name in NAMES:
        table = f'photo.{name}'
        assert db.execute('SELECT relrowsecurity AND relforcerowsecurity FROM pg_class '
                          'WHERE oid=%s::regclass', (table,)).fetchone()[0]
        assert db.execute('SELECT count(*) FROM pg_constraint WHERE conrelid=%s::regclass '
                          "AND contype='f'", (table,)).fetchone()[0] >= 2
        assert db.execute("SELECT has_table_privilege('photo_api',%s,'SELECT')",
                          (table,)).fetchone()[0]
        assert not db.execute("SELECT has_table_privilege('photo_api',%s,'INSERT')",
                              (table,)).fetchone()[0]
        assert not db.execute("SELECT has_table_privilege('photo_worker',%s,'DELETE')",
                              (table,)).fetchone()[0]
        assert db.execute("SELECT has_table_privilege('photo_fn_owner',%s,'DELETE')",
                          (table,)).fetchone()[0]
    rows = db.execute("SELECT operation,resource_class,v2_enabled FROM photo.operation_registry "
                      "WHERE operation IN ('session.describe','session.plan') ORDER BY operation").fetchall()
    assert rows == [('session.describe', 'gpu', False), ('session.plan', 'cpu', False)]
    tenant_id, actor = tenant(db, str(uuid4()) + '@example.test')
    db.execute("INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)", (tenant_id,))
    own = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Own') RETURNING id",
                     (tenant_id,)).fetchone()[0]
    other = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Other') RETURNING id",
                       (tenant_id,)).fetchone()[0]
    collection = db.execute("INSERT INTO photo.collections(tenant_id,project_id,name) "
                            "VALUES(%s,%s,'Set') RETURNING id", (tenant_id, own)).fetchone()[0]
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("INSERT INTO photo.shoot_sessions(tenant_id,project_id,collection_id,"
                   "name,state,policy,consent_epoch) VALUES(%s,%s,%s,'Cross','open',%s,1)",
                   (tenant_id, other, collection, Jsonb({})))
    session = db.execute("INSERT INTO photo.shoot_sessions(tenant_id,project_id,collection_id,"
                         "name,state,policy,consent_epoch) VALUES(%s,%s,%s,'Same','open',%s,1) "
                         "RETURNING id", (tenant_id, own, collection, Jsonb({}))).fetchone()[0]
    db.execute("UPDATE photo.shoot_sessions SET name='Renamed',revision=999 WHERE id=%s",
               (session,))
    assert db.execute("SELECT revision FROM photo.shoot_sessions WHERE id=%s",
                      (session,)).fetchone()[0] == 2
    session_token = db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,"
        "client_type,expires_at) VALUES(%s,%s,gen_random_uuid(),'web',"
        "clock_timestamp()+interval '1 hour') RETURNING id",
        (actor, uuid4().bytes)).fetchone()[0]
    def artifact(digest):
        return db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,"
            "media_type,object_version,verified_at) VALUES(%s,'test',%s,%s,2,"
            "'application/json','v1',clock_timestamp()) RETURNING id",
            (tenant_id, str(uuid4()), digest)).fetchone()[0]
    calibration = artifact('a'*64)
    report = artifact('b'*64)
    policy = Jsonb(dict(schema_version=1,time_gap_seconds=60,neighbor_count=4,
        protect_manual_picks=True,camera_offsets=[],
        calibration=dict(artifact_id=str(calibration),sha256='a'*64)))
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("SELECT photo.create_v22_session(%s,%s,%s,'Qualified',%s,NULL,%s,%s,%s)",
                   (tenant_id, own, collection, policy, 'c'*64, actor, session_token))
    db.execute("INSERT INTO photo.model_qualifications(tenant_id,project_id,operation,"
        "operation_version,model_lock_sha,backend,device_profile_sha,report_artifact_id,status) "
        "VALUES(%s,%s,'session.describe','1',%s,'cuda',%s,%s,'passed')",
        (tenant_id, own, 'c'*64, 'd'*64, report))
    assert db.execute("SELECT photo.v22_session_policy_ready(%s,%s,%s,%s)",
        (tenant_id, own, Jsonb(dict(schema_version=1,time_gap_seconds=1,
         neighbor_count=4,protect_manual_picks=True,camera_offsets=[],
         calibration=dict(artifact_id=str(calibration),sha256='a'*64))),
         'c'*64)).fetchone()[0] is False
    created = db.execute("SELECT photo.create_v22_session(%s,%s,%s,'Qualified',%s,NULL,%s,%s,%s)",
        (tenant_id, own, collection, policy, 'c'*64, actor, session_token)).fetchone()[0]
    assert created['revision'] == '1' and created['state'] == 'open'
    assert db.execute("SELECT count(*) FROM photo.consent_grants WHERE tenant_id=%s "
        "AND project_id=%s AND purpose='learning_photographer' AND person_id IS NULL "
        "AND status='revoked' AND epoch=1", (tenant_id, own)).fetchone()[0] == 1
    updated = db.execute("SELECT photo.update_v22_session(%s,%s,%s,1,'Closed','closed',"
        "%s,NULL,%s,%s,%s)",
        (tenant_id, own, created['id'], policy, 'c'*64, actor, session_token)).fetchone()[0]
    assert updated['revision'] == '2' and updated['state'] == 'closed'
    with pytest.raises(errors.SerializationFailure), db.transaction():
        db.execute("SELECT photo.update_v22_session(%s,%s,%s,1,'Stale','open',"
            "%s,NULL,%s,%s,%s)",
            (tenant_id, own, created['id'], policy, 'c'*64, actor, session_token))
    person = db.execute("SELECT photo.create_v22_person(%s,%s,'Subject',%s,%s)",
                        (tenant_id, own, actor, session_token)).fetchone()[0]
    assert person['revision'] == '1' and person['state'] == 'active'
    grants = db.execute("SELECT purpose,status,epoch FROM photo.consent_grants "
        "WHERE tenant_id=%s AND person_id=%s ORDER BY purpose",
        (tenant_id, person['id'])).fetchall()
    assert grants == [('identity_matching', 'revoked', 1),
                      ('learning_person', 'revoked', 1)]
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("UPDATE photo.project_persons SET project_id=%s WHERE id=%s",
                   (other, person['id']))
    matching = db.execute("SELECT id FROM photo.consent_grants WHERE tenant_id=%s "
        "AND person_id=%s AND purpose='identity_matching'",
        (tenant_id, person['id'])).fetchone()[0]
    snapshot = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,"
        "principal_id,digest,item_count) VALUES(%s,'v2','user',%s,%s,1) RETURNING id",
        (tenant_id, actor, 'e'*64)).fetchone()[0]
    plan_context = artifact('c'*64)
    policy_data = db.execute("SELECT policy FROM photo.shoot_sessions WHERE id=%s",
                             (created['id'],)).fetchone()[0]
    policy_sha = hashlib.sha256(json.dumps(policy_data, sort_keys=True,
        separators=(',', ':')).encode()).hexdigest()
    frozen_config = {'project_id': str(own), 'v22_session_plan': {
        'schema_version': 1, 'item_count': 1, 'snapshot_digest': 'e'*64,
        'policy': policy_data,
        'context_sha': 'c'*64}}
    run = db.execute("INSERT INTO photo.runs(tenant_id,kind,input_snapshot_id,"
        "config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,'v2',%s,"
        "%s,%s,%s,'fail_fast') RETURNING id",
        (tenant_id, snapshot, Jsonb(frozen_config), 'f'*64,
         actor)).fetchone()[0]
    with pytest.raises(errors.ObjectNotInPrerequisiteState), db.transaction():
        db.execute("UPDATE photo.runs SET config_snapshot='{}' WHERE id=%s", (run,))
    workflow = db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,"
        "input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES"
        "(%s,%s,%s,%s,'session','v2.2',%s) RETURNING id",
        (tenant_id, own, run, snapshot, policy_sha)).fetchone()[0]
    db.execute("INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,"
        "artifact_id,token,sha256,bytes,bucket,state,object_version,completed_at) "
        "VALUES(%s,%s,%s,'v2_analysis_context',%s,gen_random_uuid(),%s,2,'test',"
        "'ready','v1',clock_timestamp())",
        (tenant_id, run, run, plan_context, 'c'*64))
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("INSERT INTO photo.workflow_consent_links(tenant_id,project_id,"
            "workflow_id,consent_id,consent_epoch) VALUES(%s,%s,%s,%s,1)",
            (tenant_id, own, workflow, matching))
    grant_result = db.execute("SELECT photo.change_v2_consent(%s,%s,%s,1,"
        "'identity_matching',%s,'grant','v2.2',%s,%s)",
        (tenant_id, own, matching, person['id'], actor, session_token)).fetchone()[0]
    assert grant_result['status'] == 'granted' and grant_result['epoch'] == '2'
    assert db.execute("SELECT has_function_privilege('photo_api',"
        "'photo.confirm_v22_observation(uuid,uuid,uuid,bigint,uuid,uuid,text,uuid,uuid,text,uuid,bigint,uuid,uuid)',"
        "'EXECUTE')").fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_worker',"
        "'photo.confirm_v22_observation(uuid,uuid,uuid,bigint,uuid,uuid,text,uuid,uuid,text,uuid,bigint,uuid,uuid)',"
        "'EXECUTE')").fetchone()[0]
    observation_args = (tenant_id, own, person['id'], 1, uuid4(), uuid4(),
                        'a'*64, uuid4(), uuid4(), 'b'*64, matching, 2,
                        actor, session_token)
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("SELECT photo.confirm_v22_observation(" + ",".join(["%s"]*14) + ")",
                   observation_args)
    with pytest.raises(errors.SerializationFailure), db.transaction():
        db.execute("SELECT photo.confirm_v22_observation(" + ",".join(["%s"]*14) + ")",
                   (*observation_args[:11], 1, *observation_args[12:]))
    original = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,"
        "sha256,bytes,media_type,object_version,verified_at) VALUES"
        "(%s,'test',%s,%s,5,'image/jpeg','v1',clock_timestamp()) RETURNING id",
        (tenant_id, str(uuid4()), '0'*64)).fetchone()[0]
    source = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,"
        "sha256,bytes,media_type,object_version,verified_at,output_role) VALUES"
        "(%s,'test',%s,%s,5,'application/json','v1',clock_timestamp(),"
        "'working_image') RETURNING id",
        (tenant_id, str(uuid4()), '1'*64)).fetchone()[0]
    asset = db.execute("INSERT INTO photo.assets(tenant_id,project_id,"
        "original_artifact_id,metadata,upload_state,provenance) VALUES"
        "(%s,%s,%s,'{}','verified','{}') RETURNING id",
        (tenant_id, own, original)).fetchone()[0]
    version = db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,"
        "master_artifact_id,content_hash,metadata,review_state) VALUES"
        "(%s,%s,%s,%s,'{}','pending') RETURNING id",
        (tenant_id, asset, source, '1'*64)).fetchone()[0]
    selection = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,collection_id,"
        "source_revision,principal_type,principal_id,digest,item_count,expires_at) "
        "VALUES(%s,'selection',%s,1,'user',%s,%s,1,clock_timestamp()+interval '1 hour') "
        "RETURNING id", (tenant_id, collection, actor, '9'*64)).fetchone()[0]
    db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,"
        "asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,0,%s,%s,%s,'{}')",
        (tenant_id, selection, version, source, '1'*64))
    preflight = db.execute("SELECT photo.preflight_v22_analysis(%s,%s,2,%s,'[]',%s,%s)",
        (tenant_id, created['id'], selection, actor, session_token)).fetchone()[0]
    assert preflight['selection_digest'] == '9'*64
    assert preflight['item_count'] == '1' and preflight['manual_references'] == []
    admission_key = 'v22-analysis-draft'
    admission_run = uuid5(tenant_id, 'V2.Admit:' + str(actor) + ':' + admission_key)
    frozen_run = admission_run
    freeze_args = (tenant_id, frozen_run, created['id'], 2, selection,
                   Jsonb([]), actor, session_token, '7'*64)
    frozen = db.execute("SELECT snapshot_id,context FROM "
        "photo.freeze_v22_analysis_snapshot(%s,%s,%s,%s,%s,%s,%s,%s,%s)",
        freeze_args).fetchone()
    assert frozen[0] == uuid5(frozen_run, 'analysis.snapshot')
    assert frozen[1]['frozen_snapshot_id'] == str(frozen[0])
    assert frozen[1]['selection_digest'] == '9'*64
    assert frozen[1]['frozen_snapshot_digest'] == '7'*64
    assert db.execute("SELECT kind,digest,item_count,expires_at FROM photo.snapshots "
        "WHERE id=%s", (frozen[0],)).fetchone() == ('v2', '7'*64, 1, None)
    assert db.execute("SELECT asset_version_id,artifact_id,sha256 FROM "
        "photo.snapshot_items WHERE snapshot_id=%s", (frozen[0],)).fetchone() == (
            version, source, '1'*64)
    assert db.execute("SELECT snapshot_id FROM photo.freeze_v22_analysis_snapshot("
        "%s,%s,%s,%s,%s,%s,%s,%s,%s)", freeze_args).fetchone()[0] == frozen[0]
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("SELECT * FROM photo.freeze_v22_analysis_snapshot("
            "%s,%s,%s,%s,%s,%s,%s,%s,%s)", freeze_args[:-1] + ('8'*64,))
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("DELETE FROM photo.snapshot_items WHERE tenant_id=%s AND snapshot_id=%s",
                   (tenant_id, frozen[0]))
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,"
            "asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,1,%s,%s,%s,'{}')",
            (tenant_id, frozen[0], version, source, '1'*64))
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("UPDATE photo.snapshots SET expires_at=clock_timestamp()+interval '1 hour' "
                   "WHERE tenant_id=%s AND id=%s", (tenant_id, frozen[0]))
    assert db.execute("SELECT has_function_privilege('photo_scheduler',"
        "'photo.freeze_v22_analysis_snapshot(uuid,uuid,uuid,bigint,uuid,jsonb,uuid,uuid,text)',"
        "'EXECUTE')").fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_api',"
        "'photo.freeze_v22_analysis_snapshot(uuid,uuid,uuid,bigint,uuid,jsonb,uuid,uuid,text)',"
        "'EXECUTE')").fetchone()[0]
    with pytest.raises(errors.SerializationFailure), db.transaction():
        db.execute("SELECT * FROM photo.freeze_v22_analysis_snapshot("
            "%s,%s,%s,%s,%s,%s,%s,%s,%s)",
            freeze_args[:3] + (1,) + freeze_args[4:])
    admission_workflow = uuid5(admission_run, 'workflow')
    admission_config = {'project_id': str(own), 'session_id': created['id'],
                        'v22_session_plan': {'schema_version': 1,
                                             'item_count': 1,
                                             'snapshot_digest': '7'*64,
                                             'policy': policy_data,
                                             'context_sha': 'c'*64}}
    begin_args = (tenant_id, admission_run, admission_workflow, own, frozen[0],
                  created['id'], selection, 2, Jsonb([]), actor, session_token,
                  policy_sha,
                  'b'*64, Jsonb(admission_config), 1024, admission_key, 'c'*64)
    begin_query = "SELECT * FROM photo.begin_v22_analysis_draft(" + ",".join(["%s"]*17) + ")"
    assert db.execute(begin_query, begin_args).fetchone() == ('created', admission_run)
    assert db.execute(begin_query, begin_args).fetchone() == ('pending', admission_run)
    assert db.execute("SELECT outcome,run_id,workflow_id FROM "
        "photo.v22_analysis_replay(%s,%s,%s,%s,%s,%s)",
        (tenant_id, actor, session_token, admission_run, admission_key, 'c'*64)).fetchone() == (
            'pending', admission_run, admission_workflow)
    assert db.execute("SELECT outcome FROM photo.v22_analysis_replay("
        "%s,%s,%s,%s,%s,%s)",
        (tenant_id, actor, session_token, admission_run, admission_key, 'd'*64)).fetchone()[0] == 'conflict'
    with pytest.raises(errors.InsufficientPrivilege), db.transaction():
        db.execute("SELECT outcome FROM photo.v22_analysis_replay("
            "%s,%s,%s,%s,%s,%s)",
            (tenant_id, actor, uuid4(), admission_run, admission_key, 'c'*64))
    assert db.execute(begin_query, begin_args[:-1] + ('d'*64,)).fetchone() == ('conflict', None)
    assert db.execute("SELECT count(*) FROM photo.tasks WHERE run_id=%s",
                      (admission_run,)).fetchone()[0] == 0
    assert db.execute("SELECT bytes,state FROM photo.quota_reservations WHERE holder_id=%s",
                      (admission_run,)).fetchone() == (1024, 'reserved')
    reserve_document = "SELECT * FROM photo.reserve_v22_analysis_document(" + ",".join(["%s"]*7) + ")"
    context_args = (tenant_id, admission_run, admission_run,
                    'v2_analysis_context', 'c'*64, 2, 'test')
    document = db.execute(reserve_document, context_args).fetchone()
    assert document[0] == 'created' and not document[3]
    assert db.execute(reserve_document, context_args).fetchone()[0] == 'resumed'
    assert db.execute(reserve_document, context_args[:4] + ('d'*64, 2, 'test')).fetchone()[0] == 'forbidden'
    assert db.execute(reserve_document, context_args[:4] + ('c'*64, 3, 'test')).fetchone()[0] == 'conflict'
    latest = db.execute(reserve_document, context_args).fetchone()
    assert db.execute("SELECT photo.complete_v22_analysis_document(" + ",".join(["%s"]*10) + ")",
        context_args[:4] + (latest[1], latest[2], 'c'*64, 2, 'test', 'v1')).fetchone()[0]
    assert db.execute(reserve_document, context_args).fetchone()[0] == 'replayed'
    describe_owner = uuid5(admission_workflow, 'session.describe:0')
    assert db.execute(reserve_document, (tenant_id, admission_run, describe_owner,
        'v2_analysis_describe_params_0', 'd'*64, 2, 'test')).fetchone()[0] == 'created'
    assert db.execute(reserve_document, (tenant_id, admission_run, uuid4(),
        'v2_analysis_describe_params_0', 'd'*64, 2, 'test')).fetchone()[0] == 'forbidden'
    describe_task = {'id': str(describe_owner), 'node_key': 'session.describe:0',
                     'params_artifact_id': str(db.execute("SELECT artifact_id FROM "
                         "photo.backend_documents WHERE tenant_id=%s AND run_id=%s "
                         "AND purpose='v2_analysis_describe_params_0'",
                         (tenant_id, admission_run)).fetchone()[0]),
                     'params_sha': 'd'*64, 'input_digest': 'e'*64,
                     'execution_profile': {'backend': 'cuda', 'renderer_version': 'v2.2',
                         'resource_estimate': {'ram_bytes': 2147483648,
                             'vram_bytes': 6442450944, 'scratch_bytes': 1073741824,
                             'output_bytes': 8388608, 'pixel_count': 0}}}
    stage_query = "SELECT photo.stage_v22_analysis_describes(%s,%s,%s)"
    assert not db.execute(stage_query, (tenant_id, admission_run,
        Jsonb([describe_task]))).fetchone()[0]
    def publish_draft_document(purpose, owner, digest, size=2):
        args = (tenant_id, admission_run, owner, purpose, digest, size, 'test')
        ticket = db.execute(reserve_document, args).fetchone()
        assert ticket[0] in ('created', 'resumed')
        assert db.execute("SELECT photo.complete_v22_analysis_document(" +
            ",".join(["%s"]*10) + ")", args[:4] +
            (ticket[1], ticket[2], digest, size, 'test', 'v1')).fetchone()[0]
    publish_draft_document('v2_analysis_describe_params_0', describe_owner, 'd'*64)
    publish_draft_document('v2_analysis_input_snapshot', admission_run, '1'*64)
    assert not db.execute(stage_query, (tenant_id, admission_run,
        Jsonb([describe_task]))).fetchone()[0]
    empty_consents = db.execute("SELECT photo.v22_analysis_consent_sources(%s,%s)",
        (tenant_id, admission_run)).fetchone()[0]
    empty_bytes = json.dumps(empty_consents, sort_keys=True,
        separators=(',', ':')).encode()
    empty_sha = hashlib.sha256(empty_bytes).hexdigest()
    pin_args = (tenant_id, admission_run, Jsonb(empty_consents), empty_sha,
                len(empty_bytes))
    assert db.execute("SELECT photo.pin_v22_analysis_consents("
        "%s,%s,%s,%s,%s,'test')", pin_args).fetchone()[0]
    assert db.execute("SELECT photo.pin_v22_analysis_consents("
        "%s,%s,%s,%s,%s,'test')", pin_args).fetchone()[0]
    publish_draft_document('v2_analysis_consents', admission_run,
                           empty_sha, len(empty_bytes))
    assert db.execute(stage_query, (tenant_id, admission_run,
        Jsonb([describe_task]))).fetchone()[0]
    assert db.execute(stage_query, (tenant_id, admission_run,
        Jsonb([describe_task]))).fetchone()[0]
    changed = dict(describe_task, input_digest='f'*64)
    assert not db.execute(stage_query, (tenant_id, admission_run,
        Jsonb([changed]))).fetchone()[0]
    assert db.execute("SELECT state,graph_sealed FROM photo.tasks t JOIN photo.runs r "
        "ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.id=%s",
        (describe_owner,)).fetchone() == ('blocked', False)
    db.execute("UPDATE photo.artifacts SET output_role='working_image',"
        "created_by_task_id=%s,produced_attempt=1,produced_fence=1 WHERE id=%s",
        (describe_owner, source))
    assert db.execute("SELECT ordinal,asset_version_id,artifact_id,sha256 FROM "
        "photo.v22_analysis_image_sources(%s,%s)",
        (tenant_id, admission_run)).fetchall() == [(0, version, source, '1'*64)]
    assert db.execute("SELECT captured_at,camera_hash FROM "
        "photo.v22_analysis_image_sources(%s,%s)",
        (tenant_id, admission_run)).fetchone() == (None, None)
    db.execute("UPDATE photo.asset_versions SET metadata=%s WHERE id=%s",
        (Jsonb({'captured_at': '2026-09-30T10:00:00Z',
                'ingest': {'sensor_alignment': {'camera_id': 'raw-' + 'a'*64}}}),
         version))
    # Live metadata must not alter a previously frozen analysis input.
    assert db.execute("SELECT captured_at,camera_hash FROM "
        "photo.v22_analysis_image_sources(%s,%s)",
        (tenant_id, admission_run)).fetchone() == (None, None)
    assert db.execute("SELECT * FROM photo.v22_analysis_recipe_sources(%s,%s)",
        (tenant_id, admission_run)).fetchall() == []
    recipe_artifact = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,"
        "object_key,sha256,bytes,media_type,object_version,verified_at,output_role) "
        "VALUES(%s,'test',%s,%s,2,'application/json','v1',clock_timestamp(),"
        "'document') RETURNING id",
        (tenant_id, str(uuid4()), '5'*64)).fetchone()[0]
    draft_stream = db.execute("INSERT INTO photo.v2_document_streams(tenant_id,"
        "project_id,asset_version_id,stream_type) VALUES(%s,%s,%s,'draft') RETURNING id",
        (tenant_id, own, version)).fetchone()[0]
    recipe_document = db.execute("INSERT INTO photo.v2_documents(tenant_id,"
        "project_id,stream_id,version_no,document_type,artifact_id,sha256) "
        "VALUES(%s,%s,%s,1,'EditingRecipeV3',%s,%s) RETURNING id",
        (tenant_id, own, draft_stream, recipe_artifact, '5'*64)).fetchone()[0]
    db.execute("UPDATE photo.v2_document_streams SET current_document_id=%s "
        "WHERE id=%s", (recipe_document, draft_stream))
    recovery = {'schema_version': 1,
                'snapshot': {'snapshot_digest': '7'*64,
                             'items': [{'ordinal': 0, 'recipe': {
                                 'artifact_id': str(recipe_artifact), 'sha256': '5'*64}}]},
                'context': {}, 'context_sha': 'c'*64,
                'policy_sha': policy_sha, 'model_lock_sha': 'e'*64,
                'reserved_bytes': 1024}
    save_recovery = "SELECT photo.save_v22_analysis_recovery(%s,%s,%s,%s,%s,%s,%s)"
    recovery_args = (tenant_id, actor, session_token, admission_run,
                     admission_key, 'c'*64, Jsonb(recovery))
    assert db.execute(save_recovery, recovery_args).fetchone()[0]
    assert db.execute(save_recovery, recovery_args).fetchone()[0]
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute(save_recovery, recovery_args[:-1] +
                   (Jsonb({**recovery, 'model_lock_sha': 'd'*64}),))
    assert db.execute("SELECT photo.read_v22_analysis_recovery("
        "%s,%s,%s,%s,%s,%s)", recovery_args[:-1]).fetchone()[0] == recovery
    assert db.execute("SELECT ordinal,asset_version_id,artifact_id,sha256 "
        "FROM photo.v22_analysis_recipe_sources(%s,%s)",
        (tenant_id, admission_run)).fetchall() == [(0, version, recipe_artifact, '5'*64)]
    selection_sources = db.execute("SELECT ordinal,asset_version_id,artifact_id,"
        "recipe_artifact_id,recipe_sha256,selection_item_count FROM "
        "photo.v22_analysis_selection_sources(%s,%s,2,%s,'[]',%s,%s)",
        (tenant_id, created['id'], selection, actor, session_token)).fetchall()
    assert selection_sources == [(0, version, source, recipe_artifact, '5'*64, 1)]
    next_recipe_artifact = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,"
        "object_key,sha256,bytes,media_type,object_version,verified_at,output_role) "
        "VALUES(%s,'test',%s,%s,2,'application/json','v1',clock_timestamp(),"
        "'document') RETURNING id",
        (tenant_id, str(uuid4()), '6'*64)).fetchone()[0]
    next_recipe_document = db.execute("INSERT INTO photo.v2_documents(tenant_id,"
        "project_id,stream_id,version_no,document_type,artifact_id,sha256) "
        "VALUES(%s,%s,%s,2,'EditingRecipeV3',%s,%s) RETURNING id",
        (tenant_id, own, draft_stream, next_recipe_artifact, '6'*64)).fetchone()[0]
    db.execute("UPDATE photo.v2_document_streams SET current_document_id=%s "
        "WHERE id=%s", (next_recipe_document, draft_stream))
    assert db.execute("SELECT artifact_id,sha256 FROM "
        "photo.v22_analysis_recipe_sources(%s,%s)",
        (tenant_id, admission_run)).fetchone() == (recipe_artifact, '5'*64)
    db.execute("UPDATE photo.v2_document_streams SET current_document_id=%s "
        "WHERE id=%s", (recipe_document, draft_stream))
    assert db.execute("SELECT id,version_no FROM photo.v2_documents "
        "WHERE tenant_id=%s AND stream_id=%s AND (version_no,id)<(%s,%s) "
        "ORDER BY version_no DESC,id DESC LIMIT 3",
        (tenant_id, draft_stream, 9223372036854775807,
         UUID('ffffffff-ffff-ffff-ffff-ffffffffffff'))).fetchall() == [
             (next_recipe_document, 2), (recipe_document, 1)]
    with pytest.raises(errors.InsufficientPrivilege), db.transaction():
        db.execute("SELECT * FROM photo.v22_analysis_selection_sources("
            "%s,%s,2,%s,'[]',%s,%s)",
            (tenant_id, created['id'], selection, uuid4(), session_token))
    db.execute("UPDATE photo.artifacts SET output_role='unexpected' WHERE id=%s",
        (recipe_artifact,))
    assert db.execute("SELECT * FROM photo.v22_analysis_recipe_sources(%s,%s)",
        (tenant_id, admission_run)).fetchall() == []
    db.execute("UPDATE photo.artifacts SET output_role='document' WHERE id=%s",
        (recipe_artifact,))
    assert db.execute("SELECT photo.v22_analysis_consent_sources(%s,%s)",
        (tenant_id, admission_run)).fetchone()[0] == {
            'schema_version': 1, 'grants': [], 'confirmed_faces': []}
    face = uuid4()
    db.execute("INSERT INTO photo.person_observations(tenant_id,project_id,"
        "person_id,asset_version_id,face_id,evidence_artifact_id,confirmed_by) "
        "VALUES(%s,%s,%s,%s,%s,%s,%s)",
        (tenant_id, own, person['id'], version, face, report, actor))
    pinned_consents = db.execute("SELECT photo.v22_analysis_consent_sources(%s,%s)",
        (tenant_id, admission_run)).fetchone()[0]
    assert pinned_consents['grants'] == [{
        'consent_id': str(matching), 'epoch': '2',
        'person_id': str(person['id']), 'purpose': 'identity_matching'}]
    assert pinned_consents['confirmed_faces'] == [{
        'asset_version_id': str(version), 'face_id': str(face),
        'person_id': str(person['id']), 'consent_id': str(matching),
        'epoch': '2'}]
    consent_bytes = json.dumps(pinned_consents, sort_keys=True,
        separators=(',', ':')).encode()
    consent_sha = hashlib.sha256(consent_bytes).hexdigest()
    # The empty set was pinned before publication; a changed grant set cannot
    # replace its ready document on a retry.
    assert not db.execute("SELECT photo.pin_v22_analysis_consents("
        "%s,%s,%s,%s,%s,'test')",
        (tenant_id, admission_run, Jsonb(pinned_consents), consent_sha,
         len(consent_bytes))).fetchone()[0]
    pin_key = 'v22-consent-pin'
    pin_run = uuid5(tenant_id, 'V2.Admit:' + str(actor) + ':' + pin_key)
    pin_workflow = uuid5(pin_run, 'workflow')
    pin_snapshot = uuid5(pin_run, 'analysis.snapshot')
    assert db.execute("SELECT snapshot_id FROM photo.freeze_v22_analysis_snapshot("
        "%s,%s,%s,%s,%s,%s,%s,%s,%s)",
        (tenant_id, pin_run, created['id'], 2, selection, Jsonb([]), actor,
         session_token, '7'*64)).fetchone()[0] == pin_snapshot
    pin_begin_args = (tenant_id, pin_run, pin_workflow, own, pin_snapshot,
        created['id'], selection, 2, Jsonb([]), actor, session_token,
        policy_sha, 'b'*64, Jsonb(admission_config), 1024, pin_key, 'f'*64)
    assert db.execute(begin_query, pin_begin_args).fetchone() == ('created', pin_run)
    assert db.execute("SELECT photo.pin_v22_analysis_consents("
        "%s,%s,%s,%s,%s,'test')",
        (tenant_id, pin_run, Jsonb(pinned_consents), consent_sha,
         len(consent_bytes))).fetchone()[0]
    assert db.execute("SELECT photo.pin_v22_analysis_consents("
        "%s,%s,%s,%s,%s,'test')",
        (tenant_id, pin_run, Jsonb(pinned_consents), consent_sha,
         len(consent_bytes))).fetchone()[0]
    assert db.execute("SELECT consent_id,consent_epoch FROM photo.workflow_consent_links "
        "WHERE tenant_id=%s AND workflow_id=%s",
        (tenant_id, pin_workflow)).fetchall() == [(matching, 2)]
    assert not db.execute("SELECT has_function_privilege('photo_api',"
        "'photo.v22_analysis_consent_sources(uuid,uuid)','EXECUTE')").fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_api',"
        "'photo.pin_v22_analysis_consents(uuid,uuid,jsonb,text,bigint,text)',"
        "'EXECUTE')").fetchone()[0]
    assert db.execute("SELECT count(*) FROM photo.workflow_consent_links "
        "WHERE tenant_id=%s AND workflow_id=%s",
        (tenant_id, admission_workflow)).fetchone()[0] == 0
    db.execute("DELETE FROM photo.person_observations WHERE tenant_id=%s "
        "AND asset_version_id=%s AND face_id=%s", (tenant_id, version, face))
    pixel = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,"
        "sha256,bytes,media_type,object_version,verified_at,output_role,"
        "created_by_task_id,produced_attempt,produced_fence) VALUES"
        "(%s,'test',%s,%s,5,'application/octet-stream','v1',clock_timestamp(),"
        "'working_rgb',%s,1,1) RETURNING id",
        (tenant_id, str(uuid4()), '3'*64, describe_owner)).fetchone()[0]
    child_args = (tenant_id, admission_run, source, '1'*64, pixel, '3'*64, 'working_rgb')
    child_query = ("SELECT artifact_id,sha256 FROM photo.v22_analysis_image_child(" +
        ",".join(["%s"]*7) + ")")
    assert db.execute(child_query, child_args).fetchone() == (pixel, '3'*64)
    assert db.execute(child_query, child_args[:-1] + ('alpha',)).fetchone() is None
    wrong_fence = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,"
        "sha256,bytes,media_type,object_version,verified_at,output_role,"
        "created_by_task_id,produced_attempt,produced_fence) VALUES"
        "(%s,'test',%s,%s,5,'application/octet-stream','v1',clock_timestamp(),"
        "'working_rgb',%s,1,2) RETURNING id",
        (tenant_id, str(uuid4()), '4'*64, describe_owner)).fetchone()[0]
    assert db.execute(child_query, (tenant_id, admission_run, source, '1'*64,
        wrong_fence, '4'*64, 'working_rgb')).fetchone() is None
    assert db.execute("SELECT has_function_privilege('photo_scheduler',"
        "'photo.v22_analysis_image_sources(uuid,uuid)','EXECUTE')").fetchone()[0]
    assert db.execute("SELECT has_function_privilege('photo_scheduler',"
        "'photo.v22_analysis_recipe_sources(uuid,uuid)','EXECUTE')").fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_api',"
        "'photo.v22_analysis_recipe_sources(uuid,uuid)','EXECUTE')").fetchone()[0]
    assert db.execute("SELECT has_function_privilege('photo_scheduler',"
        "'photo.v22_analysis_selection_sources(uuid,uuid,bigint,uuid,jsonb,uuid,uuid)',"
        "'EXECUTE')").fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_api',"
        "'photo.v22_analysis_image_child(uuid,uuid,uuid,text,uuid,text,text)',"
        "'EXECUTE')").fetchone()[0]
    plan_artifact = artifact('6'*64)
    prior_analysis = db.execute("INSERT INTO photo.session_analyses(tenant_id,project_id,"
        "session_id,workflow_id,snapshot_id,plan_artifact_id,policy_sha,model_lock_sha) "
        "VALUES(%s,%s,%s,%s,%s,%s,%s,%s) RETURNING id",
        (tenant_id, own, created['id'], workflow, snapshot, plan_artifact,
         'a'*64, 'c'*64)).fetchone()[0]
    prior_group = db.execute("INSERT INTO photo.scene_groups(tenant_id,project_id,"
        "analysis_id,ordinal,reference_version_id,confidence) VALUES"
        "(%s,%s,%s,0,%s,0.8) RETURNING id",
        (tenant_id, own, prior_analysis, version)).fetchone()[0]
    db.execute("INSERT INTO photo.scene_members(tenant_id,project_id,group_id,"
        "asset_version_id,snapshot_ordinal) VALUES(%s,%s,%s,%s,0)",
        (tenant_id, own, prior_group, version))
    choice = {'previous_analysis_id': str(prior_analysis),
              'group_id': str(prior_group), 'asset_version_id': str(version)}
    held = db.execute("SELECT photo.preflight_v22_analysis(%s,%s,2,%s,%s,%s,%s)",
        (tenant_id, created['id'], selection, Jsonb([choice]),
         actor, session_token)).fetchone()[0]
    assert held['manual_references'] == [{
        'asset_version_id': str(version), 'previous_members': [str(version)]}]
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("SELECT photo.preflight_v22_analysis(%s,%s,2,%s,%s,%s,%s)",
            (tenant_id, created['id'], selection, Jsonb([choice, choice]),
             actor, session_token))
    with pytest.raises(errors.SerializationFailure), db.transaction():
        db.execute("SELECT photo.preflight_v22_analysis(%s,%s,1,%s,'[]',%s,%s)",
            (tenant_id, created['id'], selection, actor, session_token))
    foreign_collection = db.execute("INSERT INTO photo.collections(tenant_id,project_id,name) "
        "VALUES(%s,%s,'Other set') RETURNING id", (tenant_id, other)).fetchone()[0]
    foreign_selection = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,collection_id,"
        "source_revision,principal_type,principal_id,digest,item_count,expires_at) "
        "VALUES(%s,'selection',%s,1,'user',%s,%s,1,clock_timestamp()+interval '1 hour') "
        "RETURNING id", (tenant_id, foreign_collection, actor, 'e'*64)).fetchone()[0]
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("SELECT photo.preflight_v22_analysis(%s,%s,2,%s,'[]',%s,%s)",
            (tenant_id, created['id'], foreign_selection, actor, session_token))
    with pytest.raises(errors.InvalidParameterValue), db.transaction():
        db.execute("SELECT photo.preflight_v22_analysis(%s,%s,2,%s,%s,%s,%s)",
            (tenant_id, created['id'], selection,
             Jsonb([{'group_id': str(uuid4())}]), actor, session_token))
    working = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,"
        "sha256,bytes,media_type,object_version,verified_at,output_role) VALUES"
        "(%s,'test',%s,%s,5,'application/json','v1',clock_timestamp(),"
        "'working_image') RETURNING id",
        (tenant_id, str(uuid4()), '5'*64)).fetchone()[0]
    params = artifact('2'*64)
    task = db.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,"
        "operation,operation_version,resource_class,inputs,params_artifact_id,"
        "input_digest,execution_profile) VALUES(%s,%s,'session.describe:0',"
        "'session.describe','1','gpu',%s,%s,%s,%s) RETURNING id",
        (tenant_id, run, Jsonb([{'artifact_id': str(working), 'sha256': '5'*64}]),
         params, '3'*64, Jsonb({'renderer_version': 'v2.2'}))).fetchone()[0]
    evidence = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,"
        "sha256,bytes,media_type,object_version,verified_at,output_role,"
        "created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'test',%s,%s,5,'application/json',"
        "'v1',clock_timestamp(),'faces',%s,1,1) RETURNING id",
        (tenant_id, str(uuid4()), '4'*64, task)).fetchone()[0]
    root = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,"
        "sha256,bytes,media_type,object_version,verified_at,output_role,"
        "created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'test',%s,%s,5,"
        "'application/json','v1',clock_timestamp(),'document',%s,1,1) RETURNING id",
        (tenant_id, str(uuid4()), '7'*64, task)).fetchone()[0]
    for output_id, output_sha, output_role in ((evidence, '4'*64, 'faces'),
                                                (root, '7'*64, 'document')):
        db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,"
            "worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,"
            "object_key,sha256,bytes,media_type,state,object_version) VALUES"
            "(%s,%s,1,1,%s,%s,%s,%s,%s,'test',%s,%s,5,'application/json','ready','v1')",
            (tenant_id, task, uuid4(), '8'*64, uuid4(), output_id, output_role,
             db.execute("SELECT object_key FROM photo.artifacts WHERE id=%s",
                        (output_id,)).fetchone()[0], output_sha))
    result = db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,"
        "fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",
        (tenant_id, task, Jsonb([{'artifact_id': str(root), 'sha256': '7'*64}]),
         '6'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',attempt=1,fencing_token=1,"
               "canonical_result_id=%s WHERE id=%s", (result, task))
    # Canonical adoption of the final descriptor must unseal the run and hold
    # finalization until the late-bound session.plan task is materialized.
    db.execute("UPDATE photo.runs SET status='running',graph_sealed=true "
               "WHERE id=%s", (run,))
    assert not db.execute("SELECT photo.open_v22_session_plan_barrier(%s,%s)",
                          (tenant_id, task)).fetchone()[0]
    marker = db.execute("SELECT photo.v2_adoption_event_id(%s,%s)",
                        (task, result)).fetchone()[0]
    db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) "
               "VALUES(%s,'v2-adopt',%s,clock_timestamp())", (tenant_id, marker))
    assert db.execute("SELECT photo.open_v22_session_plan_barrier(%s,%s)",
                      (tenant_id, task)).fetchone()[0]
    assert not db.execute("SELECT graph_sealed FROM photo.runs WHERE id=%s",
                          (run,)).fetchone()[0]
    assert db.execute("SELECT state,expected_count FROM photo.graph_expansions "
                      "WHERE tenant_id=%s AND run_id=%s AND kind='v2.session.plan/1'",
                      (tenant_id, run)).fetchone() == ('open', 1)
    assert db.execute("SELECT task_id,artifact_id,sha256 FROM "
                      "photo.v22_session_plan_inputs(%s,%s)",
                      (tenant_id, run)).fetchall() == [(task, root, '7'*64)]
    assert db.execute("SELECT item_count,policy_sha,context_artifact_id,context_sha "
        "FROM photo.v22_session_plan_frozen(%s,%s)",
        (tenant_id, run)).fetchone() == (1, policy_sha, plan_context, 'c'*64)
    lease = db.execute("SELECT expansion_id,lease_token,tenant_id,run_id,workflow_id "
                       "FROM photo.claim_v22_session_plan()").fetchone()
    assert lease[2:] == (tenant_id, run, workflow)
    assert db.execute("SELECT has_function_privilege('photo_scheduler',"
                      "'photo.claim_v22_session_plan()','EXECUTE')").fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_api',"
                          "'photo.claim_v22_session_plan()','EXECUTE')").fetchone()[0]
    assert db.execute("SELECT photo.claim_v22_session_plan()").fetchone() is None
    assert not db.execute("SELECT photo.renew_v22_session_plan(%s,%s)",
                          (lease[0], uuid4())).fetchone()[0]
    assert db.execute("SELECT photo.renew_v22_session_plan(%s,%s)",
                      lease[:2]).fetchone()[0]
    reserved = db.execute("SELECT outcome,artifact_id,token,ready FROM "
        "photo.reserve_v22_session_plan_document(%s,%s,%s,"
        "'v2_session_plan_params',%s,16,'test')",
        (tenant_id, run, lease[0], 'a'*64)).fetchone()
    assert reserved[0] == 'created' and not reserved[3]
    assert not db.execute("SELECT photo.complete_v22_session_plan_document("
        "%s,%s,%s,'v2_session_plan_params',%s,%s,%s,16,'test','v1')",
        (tenant_id, run, lease[0], reserved[1], uuid4(), 'a'*64)).fetchone()[0]
    assert db.execute("SELECT photo.complete_v22_session_plan_document("
        "%s,%s,%s,'v2_session_plan_params',%s,%s,%s,16,'test','v1')",
        (tenant_id, run, lease[0], reserved[1], reserved[2], 'a'*64)).fetchone()[0]
    replay = db.execute("SELECT outcome,artifact_id,token,ready FROM "
        "photo.reserve_v22_session_plan_document(%s,%s,%s,"
        "'v2_session_plan_params',%s,16,'test')",
        (tenant_id, run, lease[0], 'a'*64)).fetchone()
    assert replay == ('replayed', reserved[1], reserved[2], True)
    assert db.execute("SELECT outcome FROM photo.reserve_v22_session_plan_document("
        "%s,%s,%s,'v2_session_plan_params',%s,16,'test')",
        (tenant_id, run, lease[0], 'b'*64)).fetchone()[0] == 'conflict'
    shard_refs = Jsonb([{'artifact_id': str(root), 'sha256': '7'*64}])
    profile = Jsonb({'backend': 'cpu', 'renderer_version': 'v2.2',
                     'resource_estimate': {'output_bytes': 1024}})
    stage_args = (tenant_id, run, lease[0], lease[1], uuid5(workflow, 'session.plan:0'),
                  reserved[1], 'a'*64, 'f'*64, shard_refs, profile)
    assert not db.execute("SELECT photo.stage_v22_session_plan(" +
                          ",".join(["%s"]*10) + ")",
                          (*stage_args[:4], uuid4(), *stage_args[5:])).fetchone()[0]
    assert db.execute("SELECT photo.stage_v22_session_plan(" +
                      ",".join(["%s"]*10) + ")", stage_args).fetchone()[0]
    assert not db.execute("SELECT photo.close_v22_session_plan(%s,%s,%s,%s,%s)",
                          (tenant_id, run, lease[0], lease[1], stage_args[4])).fetchone()[0]
    assert db.execute("SELECT count(*) FROM photo.task_edges WHERE tenant_id=%s "
                      "AND run_id=%s AND dependent_id=%s",
                      (tenant_id, run, stage_args[4])).fetchone()[0] == 1
    assert db.execute("SELECT photo.stage_v22_session_plan(" +
                      ",".join(["%s"]*10) + ")", stage_args).fetchone()[0]
    assert not db.execute("SELECT photo.release_v22_session_plan(%s,%s)",
                          (lease[0], uuid4())).fetchone()[0]
    closure_entries = sorted([
        {'ref': {'artifact_id': str(reserved[1]), 'sha256': 'a'*64},
         'contract': 'Params'},
        {'ref': {'artifact_id': str(root), 'sha256': '7'*64},
         'contract': 'DescriptorShard'},
        {'ref': {'artifact_id': str(plan_context), 'sha256': 'c'*64},
         'contract': 'SessionPlanContext'},
    ], key=lambda entry: entry['ref']['artifact_id'])
    def publish_closure(purpose, digest):
        ticket = db.execute("SELECT outcome,artifact_id,token FROM "
            "photo.reserve_v2_input_document(%s,%s,%s,%s,%s,16,'test')",
            (tenant_id, run, stage_args[4], purpose, digest)).fetchone()
        assert ticket[0] == 'created'
        assert db.execute("SELECT photo.complete_v2_input_document("
            "%s,%s,%s,%s,%s,%s,%s,16,'test','v1')",
            (tenant_id, run, stage_args[4], purpose, ticket[1], ticket[2],
             digest)).fetchone()[0]
        return {'artifact_id': str(ticket[1]), 'sha256': digest}
    chunk = publish_closure('v2_input_chunk_0', '1'*64)
    manifest = publish_closure('v2_input_manifest', '2'*64)
    assert db.execute("SELECT photo.register_v2_task_input_batch(%s,%s,%s,0,%s)",
        (tenant_id, run, stage_args[4], Jsonb(closure_entries))).fetchone()[0] == 3
    assert db.execute("SELECT photo.seal_v2_task_inputs(%s,%s,%s,%s,%s,3,%s,'[]')",
        (tenant_id, run, stage_args[4], Jsonb(manifest), Jsonb([chunk]),
         '3'*64)).fetchone()[0]
    assert db.execute("SELECT photo.stage_v22_session_plan(" +
                      ",".join(["%s"]*10) + ")", stage_args).fetchone()[0]
    assert not db.execute("SELECT photo.close_v22_session_plan(%s,%s,%s,%s,%s)",
                          (tenant_id, run, lease[0], lease[1], stage_args[4])).fetchone()[0]
    db.execute("UPDATE photo.operation_registry SET v2_enabled=true "
               "WHERE operation='session.plan' AND version='1'")
    assert db.execute("SELECT photo.close_v22_session_plan(%s,%s,%s,%s,%s)",
                      (tenant_id, run, lease[0], lease[1], stage_args[4])).fetchone()[0]
    assert db.execute("SELECT graph_sealed FROM photo.runs WHERE id=%s",
                      (run,)).fetchone()[0]
    assert db.execute("SELECT state FROM photo.tasks WHERE id=%s",
                      (stage_args[4],)).fetchone()[0] == 'ready'
    assert db.execute("SELECT state FROM photo.graph_expansions WHERE id=%s",
                      (lease[0],)).fetchone()[0] == 'closed'
    assert not db.execute("SELECT photo.release_v22_session_plan(%s,%s)",
                          lease[:2]).fetchone()[0]
    assert not db.execute("SELECT photo.open_v22_session_plan_barrier(%s,%s)",
                          (tenant_id, task)).fetchone()[0]
    assert db.execute("SELECT photo.finalize_v2_run(%s,%s)",
                      (tenant_id, run)).fetchone()[0] == 'pending'
    db.execute("DELETE FROM photo.task_edges WHERE tenant_id=%s AND run_id=%s "
               "AND dependent_id=%s", (tenant_id, run, stage_args[4]))
    db.execute("DELETE FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s",
               (tenant_id, stage_args[4]))
    db.execute("DELETE FROM photo.tasks WHERE tenant_id=%s AND id=%s",
               (tenant_id, stage_args[4]))
    db.execute("DELETE FROM photo.graph_expansions WHERE tenant_id=%s AND run_id=%s",
               (tenant_id, run))
    db.execute("UPDATE photo.runs SET status='draft',graph_sealed=false WHERE id=%s",
               (run,))
    confirmed_args = (tenant_id, own, person['id'], 1, version, working,
                      '5'*64, uuid4(), evidence, '4'*64, matching, 2,
                      actor, session_token)
    confirmed = db.execute("SELECT photo.confirm_v22_observation(" +
        ",".join(["%s"]*14) + ")", confirmed_args).fetchone()[0]
    assert confirmed['person_id'] == str(person['id'])
    assert db.execute("SELECT revision FROM photo.project_persons WHERE id=%s",
                      (person['id'],)).fetchone()[0] == 2
    repeated = db.execute("SELECT photo.confirm_v22_observation(" +
        ",".join(["%s"]*14) + ")",
        (*confirmed_args[:3], 2, *confirmed_args[4:])).fetchone()[0]
    assert repeated['id'] == confirmed['id']
    assert db.execute("SELECT count(*) FROM photo.person_observations WHERE "
        "tenant_id=%s AND asset_version_id=%s", (tenant_id, version)).fetchone()[0] == 1
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("INSERT INTO photo.workflow_consent_links(tenant_id,project_id,"
            "workflow_id,consent_id,consent_epoch) VALUES(%s,%s,%s,%s,1)",
            (tenant_id, own, workflow, matching))
    db.execute("INSERT INTO photo.workflow_consent_links(tenant_id,project_id,"
        "workflow_id,consent_id,consent_epoch) VALUES(%s,%s,%s,%s,2)",
        (tenant_id, own, workflow, matching))
    revoke_result = db.execute("SELECT photo.change_v2_consent(%s,%s,%s,2,"
        "'identity_matching',%s,'revoke','v2.2',%s,%s)",
        (tenant_id, own, matching, person['id'], actor, session_token)).fetchone()[0]
    assert revoke_result['status'] == 'revoked' and revoke_result['epoch'] == '3'
    with pytest.raises(errors.SerializationFailure), db.transaction():
        db.execute("SELECT photo.confirm_v22_observation(" + ",".join(["%s"]*14) + ")",
                   (*confirmed_args[:3], 2, *confirmed_args[4:]))
    assert db.execute("SELECT cancel_requested FROM photo.runs WHERE id=%s",
                      (run,)).fetchone()[0]
    assert db.execute("SELECT cancel_requested FROM photo.runs WHERE id=%s",
                      (pin_run,)).fetchone()[0]
    assert db.execute("SELECT photo.v22_analysis_consent_sources(%s,%s)",
        (tenant_id, admission_run)).fetchone()[0] == {
            'schema_version': 1, 'grants': [], 'confirmed_faces': []}
    assert not db.execute("SELECT photo.seal_v2_graph(%s,%s)",
                          (tenant_id, run)).fetchone()[0]
    assert db.execute("SELECT has_function_privilege('photo_scheduler',"
        "'photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)','EXECUTE')").fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_scheduler',"
        "'photo.adopt_v2_plan_core(uuid,uuid,integer,bigint,uuid,jsonb,text)','EXECUTE')").fetchone()[0]
    second = db.execute("SELECT photo.create_v22_person(%s,%s,'Second',%s,%s)",
                        (tenant_id, own, actor, session_token)).fetchone()[0]
    second_grant = db.execute("SELECT id FROM photo.consent_grants WHERE tenant_id=%s "
        "AND person_id=%s AND purpose='identity_matching'",
        (tenant_id, second['id'])).fetchone()[0]
    db.execute("SELECT photo.change_v2_consent(%s,%s,%s,1,'identity_matching',%s,"
        "'grant','v2.2',%s,%s)",
        (tenant_id, own, second_grant, second['id'], actor, session_token))
    revoked = db.execute("SELECT photo.revoke_v22_person(%s,%s,%s,1,%s,%s)",
        (tenant_id, own, second['id'], actor, session_token)).fetchone()[0]
    assert revoked['state'] == 'revoked' and revoked['consent_epoch'] == '2'
    assert db.execute("SELECT status,epoch FROM photo.consent_grants WHERE id=%s",
                      (second_grant,)).fetchone() == ('revoked', 3)
    assert db.execute("SELECT action,epoch FROM photo.consent_events WHERE tenant_id=%s "
        "AND consent_id=%s ORDER BY epoch", (tenant_id, second_grant)).fetchall() == [
            ('grant', 2), ('revoke', 3)]
    assert db.execute("SELECT count(*) FROM photo.project_persons WHERE tenant_id=%s "
        "AND id=%s", (tenant_id, second['id'])).fetchone()[0] == 1
    with pytest.raises(errors.SerializationFailure), db.transaction():
        db.execute("SELECT photo.revoke_v22_person(%s,%s,%s,1,%s,%s)",
            (tenant_id, own, second['id'], actor, session_token))
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("SELECT photo.change_v2_consent(%s,%s,%s,3,'identity_matching',%s,"
            "'grant','v2.2',%s,%s)",
            (tenant_id, own, second_grant, second['id'], actor, session_token))
    replacement = db.execute("SELECT photo.create_v22_person(%s,%s,'Replacement',%s,%s)",
                             (tenant_id, own, actor, session_token)).fetchone()[0]
    replacement_grant = db.execute("SELECT id FROM photo.consent_grants WHERE tenant_id=%s "
        "AND person_id=%s AND purpose='identity_matching'",
        (tenant_id, replacement['id'])).fetchone()[0]
    db.execute("SELECT photo.change_v2_consent(%s,%s,%s,1,'identity_matching',%s,"
        "'grant','v2.2',%s,%s)",
        (tenant_id, own, replacement_grant, replacement['id'], actor, session_token))
    replacement_args = (tenant_id, own, replacement['id'], 1, version, working,
                        '5'*64, confirmed_args[7], evidence, '4'*64,
                        replacement_grant, 2, actor, session_token)
    with pytest.raises(errors.UniqueViolation), db.transaction():
        db.execute("SELECT photo.confirm_v22_observation(" + ",".join(["%s"]*14) + ")",
                   replacement_args)
    db.execute("SELECT photo.revoke_v22_person(%s,%s,%s,2,%s,%s)",
               (tenant_id, own, person['id'], actor, session_token))
    rebound = db.execute("SELECT photo.confirm_v22_observation(" +
        ",".join(["%s"]*14) + ")", replacement_args).fetchone()[0]
    assert rebound['id'] == confirmed['id']
    assert rebound['person_id'] == str(replacement['id'])
    assert db.execute("SELECT count(*) FROM photo.audit_log WHERE tenant_id=%s "
        "AND action='v2.observation.rebind' AND resource_id=%s",
        (tenant_id, confirmed['id'])).fetchone()[0] == 1
    assert db.execute("SELECT photo.v2_artifact_has_references(%s,%s)",
                      (tenant_id, plan_artifact)).fetchone()[0]
    assert db.execute("SELECT photo.v2_artifact_has_references(%s,%s)",
                      (tenant_id, evidence)).fetchone()[0]
    db.execute("UPDATE photo.snapshots SET expires_at=clock_timestamp()-interval '1 second' "
               "WHERE id=%s", (selection,))
    assert db.execute(begin_query, begin_args).fetchone() == ('pending', admission_run)
    assert db.execute("SELECT photo.read_v22_analysis_recovery("
        "%s,%s,%s,%s,%s,%s)", recovery_args[:-1]).fetchone()[0] == recovery
    db.execute("UPDATE photo.runs SET status='cancelled' WHERE id IN (%s,%s,%s)",
               (run, admission_run, pin_run))
    db.execute("UPDATE photo.tenants SET status='deleting' WHERE id=%s", (tenant_id,))
    job = db.execute("INSERT INTO photo.deletion_jobs(tenant_id,resource_type,"
        "resource_id,requested_by) VALUES(%s,'tenant',%s,%s) RETURNING id",
        (tenant_id, tenant_id, actor)).fetchone()[0]
    claim = db.execute("SELECT job_id,cleanup_token FROM photo.claim_tenant_deletion()").fetchone()
    assert claim[0] == job
    assert db.execute("SELECT photo.finish_tenant_deletion(%s,%s)",
                      (job, claim[1])).fetchone()[0]
    for table in NAMES:
        assert db.execute(f"SELECT count(*) FROM photo.{table} WHERE tenant_id=%s",
                          (tenant_id,)).fetchone()[0] == 0
