"""An adopted session plan is published once as reviewable relational rows."""

from uuid import uuid4

import pytest
from psycopg import errors
from psycopg.types.json import Jsonb

from test_database import tenant


def test_adopted_scene_plan_materializes_once_and_rejects_rebinding(db):
    tid, actor = tenant(db, str(uuid4()) + '@example.test')
    db.execute("INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)", (tid,))
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) "
                         "VALUES(%s,'Session') RETURNING id", (tid,)).fetchone()[0]
    collection = db.execute("INSERT INTO photo.collections(tenant_id,project_id,name) "
                            "VALUES(%s,%s,'Input') RETURNING id",
                            (tid, project)).fetchone()[0]
    session = db.execute("INSERT INTO photo.shoot_sessions(tenant_id,project_id,"
                         "collection_id,name,state,policy,consent_epoch) "
                         "VALUES(%s,%s,%s,'Shoot','open',%s,1) RETURNING id",
                         (tid, project, collection, Jsonb({}))).fetchone()[0]

    def artifact(sha, *, task=None, role=None):
        return db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,"
                          "object_version,sha256,bytes,media_type,verified_at,"
                          "created_by_task_id,produced_attempt,produced_fence,output_role) "
                          "VALUES(%s,'test',%s,'v1',%s,64,'application/json',"
                          "clock_timestamp(),%s,%s,%s,%s) RETURNING id",
                          (tid, str(uuid4()), sha, task, 1 if task else None,
                           1 if task else None, role)).fetchone()[0]

    working = artifact('a'*64)
    asset = db.execute("INSERT INTO photo.assets(tenant_id,project_id,"
                       "original_artifact_id,metadata,upload_state,provenance) "
                       "VALUES(%s,%s,%s,%s,'verified',%s) RETURNING id",
                       (tid, project, working, Jsonb({}), Jsonb({}))).fetchone()[0]
    version = db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,"
                         "master_artifact_id,content_hash,metadata,review_state) "
                         "VALUES(%s,%s,%s,%s,%s,'accepted') RETURNING id",
                         (tid, asset, working, 'a'*64, Jsonb({}))).fetchone()[0]
    item = db.execute("INSERT INTO photo.collection_items(tenant_id,collection_id,"
                      "asset_id,asset_version_id,position) VALUES(%s,%s,%s,%s,0) "
                      "RETURNING id", (tid, collection, asset, version)).fetchone()[0]
    snapshot = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,collection_id,"
                          "source_revision,principal_type,principal_id,digest,item_count,expires_at) "
                          "VALUES(%s,'v2',%s,1,'user',%s,%s,1,"
                          "clock_timestamp()+interval '1 hour') RETURNING id",
                          (tid, collection, actor, 'b'*64)).fetchone()[0]
    db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,"
               "item_id,asset_version_id,artifact_id,sha256,payload) "
               "VALUES(%s,%s,0,%s,%s,%s,%s,%s)",
               (tid, snapshot, item, version, working, 'a'*64, Jsonb({})))
    db.execute("UPDATE photo.snapshots SET expires_at=NULL WHERE id=%s", (snapshot,))
    config = {'project_id': str(project), 'session_id': str(session)}
    run = db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,"
                     "config_snapshot,config_hash,created_by,failure_policy,graph_sealed) "
                     "VALUES(%s,'v2','waiting_review',%s,%s,%s,%s,'fail_fast',true) "
                     "RETURNING id", (tid, snapshot, Jsonb(config), 'c'*64,
                                      actor)).fetchone()[0]
    workflow = db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,"
                          "run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) "
                          "VALUES(%s,%s,%s,%s,'session','v2.2',%s) RETURNING id",
                          (tid, project, run, snapshot, 'd'*64)).fetchone()[0]
    db.execute("INSERT INTO photo.v22_analysis_locks(workflow_id,tenant_id,model_lock_sha) "
               "VALUES(%s,%s,%s)", (workflow, tid, 'e'*64))
    input_doc = artifact('6'*64)
    db.execute("INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,"
               "artifact_id,token,sha256,bytes,bucket,state,object_version,completed_at) "
               "VALUES(%s,%s,%s,'v2_analysis_input_snapshot',%s,gen_random_uuid(),"
               "%s,64,'test','ready','v1',clock_timestamp())",
               (tid, run, run, input_doc, '6'*64))
    params = artifact('f'*64)
    task = db.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,"
                      "operation_version,resource_class,state,inputs,params_artifact_id,"
                      "input_digest,attempt,fencing_token,execution_profile) "
                      "VALUES(%s,%s,'session.plan:0','session.plan','1','cpu',"
                      "'blocked',%s,%s,%s,1,1,%s) RETURNING id",
                      (tid, run, Jsonb([]), params, '1'*64,
                       Jsonb({'renderer_version': 'v2.2'}))).fetchone()[0]
    root = artifact('2'*64, task=task, role='document')
    correction = artifact('3'*64, task=task, role='correction_plan')
    db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,"
               "worker_id,certificate_sha256,idempotency_key,artifact_id,role,"
               "bucket,object_key,sha256,bytes,media_type,state,object_version) "
               "VALUES(%s,%s,1,1,%s,%s,%s,%s,'document','test',%s,%s,64,"
               "'application/json','ready','v1')",
               (tid, task, uuid4(), '4'*64, uuid4(), root, str(uuid4()), '2'*64))
    result = db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,"
                        "fencing_token,outputs,metrics,review_required,result_digest) "
                        "VALUES(%s,%s,1,1,%s,%s,true,%s) RETURNING id",
                        (tid, task, Jsonb([{'artifact_id': str(root),
                                           'sha256': '2'*64}]), Jsonb({}),
                         '5'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET canonical_result_id=%s,state='succeeded' WHERE id=%s",
               (result, task))
    marker = db.execute("SELECT photo.v2_adoption_event_id(%s,%s)",
                        (task, result)).fetchone()[0]
    db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) "
               "VALUES(%s,'v2-adopt',%s,clock_timestamp())", (tid, marker))
    db.execute("INSERT INTO photo.v2_artifact_links(tenant_id,project_id,workflow_id,"
               "artifact_id,sha256,purpose,ordinal) VALUES(%s,%s,%s,%s,%s,'group-correction',1)",
               (tid, project, workflow, correction, '3'*64))

    group = uuid4()
    plan = {'schema_version': 1, 'snapshot_id': str(snapshot),
            'snapshot_digest': 'b'*64, 'policy_sha': 'd'*64,
            'groups': [{'id': str(group), 'ordinals': [0],
                        'reference_ordinal': 0, 'confidence': 1,
                        'corrections': {'artifact_id': str(correction), 'sha256': '3'*64}}]}
    assert (tid, run) in db.execute(
        "SELECT * FROM photo.v22_pending_scene_plan_materializations()").fetchall()
    assert db.execute("SELECT artifact_id FROM photo.v22_scene_input_snapshot_source(%s,%s)",
                      (tid, run)).fetchone()[0] == input_doc
    assert db.execute("SELECT ordinal,asset_version_id FROM photo.v22_scene_snapshot_members(%s,%s)",
                      (tid, run)).fetchone() == (0, version)
    assert db.execute("SELECT artifact_id FROM photo.v22_scene_correction_sources(%s,%s)",
                      (tid, run)).fetchone()[0] == correction
    args = (tid, run, root, '2'*64, Jsonb(plan), 'e'*64)
    analysis = db.execute("SELECT photo.materialize_v22_scene_plan(%s,%s,%s,%s,%s,%s)",
                          args).fetchone()[0]
    assert analysis == db.execute("SELECT id FROM photo.session_analyses WHERE workflow_id=%s",
                                  (workflow,)).fetchone()[0]
    assert db.execute("SELECT reference_version_id FROM photo.scene_groups "
                      "WHERE analysis_id=%s", (analysis,)).fetchone()[0] == version
    assert db.execute("SELECT snapshot_ordinal,asset_version_id FROM photo.scene_members "
                      "WHERE group_id=%s", (group,)).fetchone() == (0, version)
    assert db.execute("SELECT photo.materialize_v22_scene_plan(%s,%s,%s,%s,%s,%s)",
                      args).fetchone()[0] == analysis
    assert (tid, run) not in db.execute(
        "SELECT * FROM photo.v22_pending_scene_plan_materializations()").fetchall()
    changed = dict(plan)
    changed['groups'] = [{**plan['groups'][0], 'reference_ordinal': 1}]
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("SELECT photo.materialize_v22_scene_plan(%s,%s,%s,%s,%s,%s)",
                   (tid, run, root, '2'*64, Jsonb(changed), 'e'*64))

    auth_session = db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,"
                              "family_id,client_type,expires_at) VALUES(%s,%s,%s,'web',"
                              "clock_timestamp()+interval '1 day') RETURNING id",
                              (actor, uuid4().bytes, uuid4())).fetchone()[0]
    from render_descriptor_scope_fixture import assert_render_descriptor_authority
    assert_render_descriptor_authority(db, tid, project, version, working,
                                      run, analysis, params, actor, auth_session)

    db.execute("SET LOCAL ROLE photo_api")
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tid),))
    assert db.execute("SELECT artifact_id FROM photo.v22_analysis_apply_input_source("
                      "%s,%s,%s,%s)", (tid, analysis, actor,
                                       auth_session)).fetchone()[0] == input_doc
    assert db.execute("SELECT count(*) FROM photo.v22_analysis_apply_input_source("
                      "%s,%s,%s,%s)", (tid, analysis, uuid4(),
                                       auth_session)).fetchone()[0] == 0
    assert db.execute(
        "SELECT a.project_id,w.run_id,s.digest,shoot.collection_id,plan.id "
        "FROM photo.session_analyses a "
        "JOIN photo.v2_workflows w ON w.tenant_id=a.tenant_id "
        "AND w.id=a.workflow_id "
        "JOIN photo.runs parent ON parent.tenant_id=w.tenant_id "
        "AND parent.id=w.run_id "
        "JOIN photo.snapshots s ON s.tenant_id=a.tenant_id "
        "AND s.id=a.snapshot_id "
        "JOIN photo.shoot_sessions shoot ON shoot.tenant_id=a.tenant_id "
        "AND shoot.id=a.session_id "
        "JOIN photo.collections source ON source.tenant_id=shoot.tenant_id "
        "AND source.id=shoot.collection_id "
        "JOIN photo.artifacts plan ON plan.tenant_id=a.tenant_id "
        "AND plan.id=a.plan_artifact_id "
        "WHERE a.tenant_id=%s AND a.id=%s", (tid, analysis)
    ).fetchone() == (project, run, 'b'*64, collection, root)
    assert db.execute(
        "SELECT count(*) FROM photo.snapshot_items "
        "WHERE tenant_id=%s AND snapshot_id=%s", (tid, snapshot)
    ).fetchone()[0] == 1
    assert db.execute(
        "SELECT count(*) FROM photo.v2_artifact_links link "
        "JOIN photo.artifacts artifact ON artifact.tenant_id=link.tenant_id "
        "AND artifact.id=link.artifact_id "
        "WHERE link.tenant_id=%s AND link.workflow_id=%s "
        "AND artifact.output_role='correction_plan'", (tid, workflow)
    ).fetchone()[0] == 1
    db.execute("RESET ROLE")
    child_snapshot = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,"
                                "collection_id,source_revision,principal_type,principal_id,"
                                "digest,item_count,expires_at) VALUES(%s,'selection',%s,1,"
                                "'user',%s,%s,1,clock_timestamp()+interval '1 day') "
                                "RETURNING id", (tid, collection, actor, '7'*64)).fetchone()[0]
    db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,"
               "ordinal,item_id,asset_version_id,artifact_id,sha256,payload) "
               "VALUES(%s,%s,0,%s,%s,%s,%s,%s)",
               (tid, child_snapshot, item, version, working, 'a'*64,
                Jsonb({'position': 0})))
    output = db.execute("INSERT INTO photo.collections(tenant_id,project_id,name,"
                        "source_collection_id,source_revision) VALUES(%s,%s,'Output',%s,1) "
                        "RETURNING id", (tid, project, collection)).fetchone()[0]
    child_config = {'source_collection_id': str(collection), 'scope': 'selected',
                    'source_revision': 1, 'input_snapshot_id': str(child_snapshot),
                    'input_snapshot_digest': '7'*64, 'analysis_decision_sha': '8'*64,
                    'item_overrides': [{'item_id': str(item),
                                        'asset_version_id': str(version),
                                        'input_sha256': 'a'*64,
                                        'body': {'schema_version': 1, 'revision': 1,
                                                 'input_sha256': 'a'*64,
                                                 'modules': {'global': {'saturation': 1}}}}]}
    child = db.execute("INSERT INTO photo.runs(tenant_id,kind,status,"
                       "input_snapshot_id,output_collection_id,config_snapshot,config_hash,"
                       "created_by,failure_policy) VALUES(%s,'apply','queued',%s,%s,%s,%s,"
                       "%s,'continue_items') RETURNING id",
                       (tid, child_snapshot, output, Jsonb(child_config), '9'*64,
                        actor)).fetchone()[0]
    db.execute("UPDATE photo.collections SET source_run_id=%s WHERE id=%s",
               (child, output))
    db.execute("INSERT INTO photo.apply_builds(tenant_id,run_id,"
               "source_collection_id,item_count,phase,graph_version) "
               "VALUES(%s,%s,%s,1,'resolve',4)", (tid, child, collection))
    args = (tid, analysis, actor, auth_session, 1, '2'*64, input_doc,
            '6'*64, '8'*64, [group], child)
    with pytest.raises(errors.InvalidParameterValue), db.transaction():
        db.execute("SELECT photo.finish_v22_analysis_apply(%s,%s,%s,%s,%s,%s,"
                   "%s,%s,%s,%s,%s)", (*args[:-2], [group, group], child))
    bad_snapshot = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,"
                              "collection_id,source_revision,principal_type,principal_id,"
                              "digest,item_count,expires_at) VALUES(%s,'selection',%s,1,"
                              "'user',%s,%s,1,clock_timestamp()+interval '1 day') "
                              "RETURNING id", (tid, collection, actor, '0'*64)).fetchone()[0]
    db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,"
               "item_id,asset_version_id,artifact_id,sha256,payload) "
               "VALUES(%s,%s,0,%s,%s,%s,%s,%s)",
               (tid, bad_snapshot, item, version, input_doc, '6'*64, Jsonb({})))
    bad_output = db.execute("INSERT INTO photo.collections(tenant_id,project_id,name,"
                            "source_collection_id,source_revision) VALUES(%s,%s,'Wrong',"
                            "%s,1) RETURNING id", (tid, project, collection)).fetchone()[0]
    bad_config = {**child_config, 'input_snapshot_id': str(bad_snapshot),
                  'input_snapshot_digest': '0'*64,
                  'item_overrides': [{**child_config['item_overrides'][0],
                                      'input_sha256': '6'*64,
                                      'body': {**child_config['item_overrides'][0]['body'],
                                               'input_sha256': '6'*64}}]}
    bad_child = db.execute("INSERT INTO photo.runs(tenant_id,kind,status,"
                           "input_snapshot_id,output_collection_id,config_snapshot,"
                           "config_hash,created_by,failure_policy) VALUES(%s,'apply',"
                           "'queued',%s,%s,%s,%s,%s,'continue_items') RETURNING id",
                           (tid, bad_snapshot, bad_output, Jsonb(bad_config), '9'*64,
                            actor)).fetchone()[0]
    db.execute("UPDATE photo.collections SET source_run_id=%s WHERE id=%s",
               (bad_child, bad_output))
    db.execute("INSERT INTO photo.apply_builds(tenant_id,run_id,"
               "source_collection_id,item_count,phase,graph_version) "
               "VALUES(%s,%s,%s,1,'resolve',4)", (tid, bad_child, collection))
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("SELECT photo.finish_v22_analysis_apply(%s,%s,%s,%s,%s,%s,"
                   "%s,%s,%s,%s,%s)", (*args[:-1], bad_child))
    assert db.execute("SELECT status FROM photo.runs WHERE id=%s",
                      (run,)).fetchone()[0] == 'waiting_review'
    assert db.execute("SELECT count(*) FROM photo.v22_analysis_decisions "
                      "WHERE analysis_id=%s", (analysis,)).fetchone()[0] == 0
    assert db.execute("SELECT photo.finish_v22_analysis_apply(%s,%s,%s,%s,%s,%s,"
                      "%s,%s,%s,%s,%s)", args).fetchone()[0] is True
    assert db.execute("SELECT status,revision FROM photo.runs WHERE id=%s",
                      (run,)).fetchone() == ('completed', 2)
    assert db.execute("SELECT child_run_id,approved_groups FROM "
                      "photo.v22_analysis_decisions WHERE analysis_id=%s",
                      (analysis,)).fetchone() == (child, [group])
    with pytest.raises(errors.SerializationFailure), db.transaction():
        db.execute("SELECT photo.finish_v22_analysis_apply(%s,%s,%s,%s,%s,%s,"
                   "%s,%s,%s,%s,%s)", args)
    db.execute("DELETE FROM photo.scene_members WHERE group_id=%s", (group,))
    db.execute("DELETE FROM photo.scene_groups WHERE id=%s", (group,))
    db.execute("DELETE FROM photo.session_analyses WHERE id=%s", (analysis,))
    assert db.execute("SELECT count(*) FROM photo.v22_analysis_decisions "
                      "WHERE analysis_id=%s", (analysis,)).fetchone()[0] == 0
