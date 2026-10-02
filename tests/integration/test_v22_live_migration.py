"""V22 is installed by the normal forward-only migration chain."""

from datetime import datetime, timedelta, timezone
from uuid import uuid4

import pytest
from psycopg import errors
from psycopg.types.json import Jsonb

from test_database import tenant


def test_v22_live_migration_installs_session_and_consent_boundary(db):
    for table in ('shoot_sessions', 'session_analyses', 'scene_groups',
                  'scene_members', 'project_persons', 'person_observations',
                  'consent_grants', 'consent_events', 'workflow_consent_links'):
        assert db.execute("SELECT to_regclass(%s) IS NOT NULL",
                          (f'photo.{table}',)).fetchone()[0]
        assert db.execute("SELECT has_table_privilege('photo_api',%s,'SELECT')",
                          (f'photo.{table}',)).fetchone()[0]
        assert not db.execute("SELECT has_table_privilege('photo_api',%s,'INSERT')",
                              (f'photo.{table}',)).fetchone()[0]
    assert db.execute("SELECT to_regprocedure('photo.begin_v22_analysis_draft("
                      "uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,jsonb,uuid,uuid,"
                      "text,text,jsonb,bigint,text,text)') IS NOT NULL").fetchone()[0]
    assert db.execute("SELECT has_function_privilege('photo_api',"
                      "'photo.create_v22_session(uuid,uuid,uuid,text,jsonb,jsonb,text,uuid,uuid)',"
                      "'EXECUTE')").fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_worker',"
                          "'photo.create_v22_session(uuid,uuid,uuid,text,jsonb,jsonb,text,uuid,uuid)',"
                          "'EXECUTE')").fetchone()[0]
    signature = ('photo.materialize_v22_scene_plan('
                 'uuid,uuid,uuid,text,jsonb,text)')
    assert db.execute("SELECT to_regprocedure(%s) IS NOT NULL", (signature,)).fetchone()[0]
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",
                      (signature,)).fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')",
                          (signature,)).fetchone()[0]
    for function in ('v22_pending_scene_plan_materializations()',
                     'v22_scene_plan_materialization_source(uuid,uuid)',
                     'v22_scene_input_snapshot_source(uuid,uuid)',
                     'v22_scene_snapshot_members(uuid,uuid)',
                     'v22_scene_correction_sources(uuid,uuid)'):
        name = 'photo.' + function
        assert db.execute("SELECT to_regprocedure(%s) IS NOT NULL",
                          (name,)).fetchone()[0]
        assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",
                          (name,)).fetchone()[0]
        assert not db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')",
                              (name,)).fetchone()[0]
    decision = 'photo.v22_analysis_decisions'
    assert db.execute("SELECT to_regclass(%s) IS NOT NULL", (decision,)).fetchone()[0]
    assert not db.execute("SELECT has_table_privilege('photo_api',%s,'INSERT')",
                          (decision,)).fetchone()[0]
    finish = ('photo.finish_v22_analysis_apply(uuid,uuid,uuid,uuid,bigint,text,'
              'uuid,text,text,uuid[],uuid)')
    assert db.execute("SELECT to_regprocedure(%s) IS NOT NULL", (finish,)).fetchone()[0]
    assert db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')",
                      (finish,)).fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_worker',%s,'EXECUTE')",
                          (finish,)).fetchone()[0]
    apply_source = 'photo.v22_analysis_apply_input_source(uuid,uuid,uuid,uuid)'
    assert db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')",
                      (apply_source,)).fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_worker',%s,'EXECUTE')",
                          (apply_source,)).fetchone()[0]
    apply_replay = ('photo.v22_analysis_apply_replay('
                    'uuid,uuid,uuid,uuid,text,text)')
    assert db.execute("SELECT to_regprocedure(%s) IS NOT NULL",
                      (apply_replay,)).fetchone()[0]
    assert db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')",
                      (apply_replay,)).fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_worker',%s,'EXECUTE')",
                          (apply_replay,)).fetchone()[0]
    assert not db.execute("SELECT has_table_privilege('photo_api',%s,'SELECT')",
                          (decision,)).fetchone()[0]
    assert db.execute("SELECT has_table_privilege('photo_api',"
                      "'photo.apply_builds','INSERT')").fetchone()[0]
    assert not db.execute("SELECT has_table_privilege('photo_api',"
                          "'photo.apply_builds','SELECT')").fetchone()[0]
    assert not db.execute("SELECT has_table_privilege('photo_api',"
                          "'photo.apply_builds','UPDATE')").fetchone()[0]


def test_v22_recovery_pins_model_lock_once_and_purges_with_workflow(db):
    tenant_id, actor = tenant(db, str(uuid4()) + '@example.test')
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'P') "
                         "RETURNING id", (tenant_id,)).fetchone()[0]
    snapshot = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,"
                          "principal_id,digest,item_count) VALUES(%s,'v2','user',%s,%s,1) "
                          "RETURNING id", (tenant_id, actor, 'a'*64)).fetchone()[0]
    run = db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,"
                     "config_snapshot,config_hash,created_by,failure_policy) VALUES"
                     "(%s,'v2','draft',%s,%s,%s,%s,'fail_fast') RETURNING id",
                     (tenant_id, snapshot, Jsonb({'project_id': str(project)}),
                      'b'*64, actor)).fetchone()[0]
    workflow = db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,"
                          "input_snapshot_id,workflow_type,renderer_version,policy_sha) "
                          "VALUES(%s,%s,%s,%s,'session','v2.2',%s) RETURNING id",
                          (tenant_id, project, run, snapshot, 'c'*64)).fetchone()[0]
    idem = db.execute("INSERT INTO photo.idempotency_records(tenant_id,principal_type,"
                      "principal_id,route,key,request_hash,expires_at,resource_id) "
                      "VALUES(%s,'user',%s,'V2.Admit','pin',%s,%s,%s) RETURNING id",
                      (tenant_id, actor, 'd'*64,
                       datetime.now(timezone.utc)+timedelta(days=1), run)).fetchone()[0]
    db.execute("UPDATE photo.idempotency_records SET response_body=%s WHERE id=%s",
               (Jsonb({'model_lock_sha': 'e'*64}), idem))
    assert db.execute("SELECT model_lock_sha FROM photo.v22_analysis_locks "
                      "WHERE workflow_id=%s", (workflow,)).fetchone()[0] == 'e'*64
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("UPDATE photo.idempotency_records SET response_body=%s WHERE id=%s",
                   (Jsonb({'model_lock_sha': 'f'*64}), idem))
    db.execute("DELETE FROM photo.v2_workflows WHERE id=%s", (workflow,))
    assert db.execute("SELECT count(*) FROM photo.v22_analysis_locks "
                      "WHERE workflow_id=%s", (workflow,)).fetchone()[0] == 0
