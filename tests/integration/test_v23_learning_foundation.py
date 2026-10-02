"""V23 metadata is isolated until learning admission and worker qualification."""

from uuid import uuid4

import pytest
from psycopg import errors
from psycopg.types.json import Jsonb

from test_database import tenant


TABLES = ('correction_events', 'learning_profiles', 'learning_versions',
          'version_eligibility', 'learning_example_links', 'model_use_links',
          'profile_activations', 'consent_cleanup', 'learning_safety_reviews')


def test_v23_foundation_table_roles_and_purge_allowlist(db):
    for table in TABLES:
        name = 'photo.' + table
        assert db.execute('SELECT to_regclass(%s) IS NOT NULL',
                          (name,)).fetchone()[0]
        assert db.execute("SELECT has_table_privilege('photo_api',%s,'SELECT')",
                          (name,)).fetchone()[0]
        for verb in ('INSERT', 'UPDATE', 'DELETE'):
            assert not db.execute('SELECT has_table_privilege('
                                  "'photo_api',%s,%s)", (name, verb)).fetchone()[0]
        assert db.execute("SELECT has_table_privilege('photo_fn_owner',%s,'DELETE')",
                          (name,)).fetchone()[0]
    body = db.execute("SELECT pg_get_functiondef('photo.finish_tenant_deletion(" 
                      "uuid,uuid)'::regprocedure)").fetchone()[0]
    assert all("'" + table + "'" in body for table in TABLES)


def test_v23_consent_cleanup_artifact_blocks_gc_until_row_is_removed(db):
    tid, actor = tenant(db, str(uuid4()) + '@example.test')
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) "
                         "VALUES(%s,'Learning') RETURNING id", (tid,)).fetchone()[0]
    artifact = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,"
                          "object_version,sha256,bytes,media_type,verified_at) "
                          "VALUES(%s,'test',%s,'v1',%s,64,'application/json',now()) "
                          "RETURNING id", (tid, str(uuid4()), 'a'*64)).fetchone()[0]
    consent = db.execute("INSERT INTO photo.consent_grants(tenant_id,project_id,"
                         "purpose,status,epoch,policy_version,actor_id) "
                         "VALUES(%s,%s,'learning_photographer','granted',1,'v1',%s) "
                         "RETURNING id", (tid, project, actor)).fetchone()[0]
    assert not db.execute("SELECT photo.v2_artifact_has_references(%s,%s)",
                          (tid, artifact)).fetchone()[0]
    row = db.execute("INSERT INTO photo.consent_cleanup(tenant_id,project_id,"
                     "consent_id,artifact_id,state,attempts,next_attempt_at) "
                     "VALUES(%s,%s,%s,%s,'pending',0,now()) RETURNING id",
                     (tid, project, consent, artifact)).fetchone()[0]
    assert db.execute("SELECT photo.v2_artifact_has_references(%s,%s)",
                      (tid, artifact)).fetchone()[0]
    db.execute("DELETE FROM photo.consent_cleanup WHERE id=%s", (row,))
    assert not db.execute("SELECT photo.v2_artifact_has_references(%s,%s)",
                          (tid, artifact)).fetchone()[0]


def test_v23_learning_version_pins_all_three_artifacts(db):
    tid, actor = tenant(db, str(uuid4()) + '@example.test')
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) "
                         "VALUES(%s,'Learning') RETURNING id", (tid,)).fetchone()[0]
    snapshot = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,"
                          "principal_id,digest,item_count) VALUES(%s,'v2','user',"
                          "%s,%s,1) RETURNING id", (tid, actor, 'a'*64)).fetchone()[0]
    run = db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,"
                     "config_snapshot,config_hash,created_by,failure_policy) "
                     "VALUES(%s,'v2','draft',%s,%s,%s,%s,'fail_fast') RETURNING id",
                     (tid, snapshot, Jsonb({'project_id': str(project)}),
                      'b'*64, actor)).fetchone()[0]
    workflow = db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,"
                          "run_id,input_snapshot_id,workflow_type,renderer_version,"
                          "policy_sha) VALUES(%s,%s,%s,%s,'learning','v2.2',%s) "
                          "RETURNING id", (tid, project, run, snapshot,
                                           'c'*64)).fetchone()[0]
    profile = db.execute("INSERT INTO photo.learning_profiles(tenant_id,project_id,"
                         "name,mode,created_by) VALUES(%s,%s,'My model',"
                         "'photographer',%s) RETURNING id",
                         (tid, project, actor)).fetchone()[0]
    artifacts = []
    for index in range(3):
        artifacts.append(db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,"
            "object_key,object_version,sha256,bytes,media_type,verified_at) "
            "VALUES(%s,'test',%s,'v1',%s,64,'application/json',now()) RETURNING id",
            (tid, str(uuid4()), f'{index+1:064x}')).fetchone()[0])
    version = db.execute("INSERT INTO photo.learning_versions(tenant_id,project_id,"
                         "profile_id,version_no,workflow_id,training_artifact_id,"
                         "model_artifact_id,evaluation_artifact_id) "
                         "VALUES(%s,%s,%s,1,%s,%s,%s,%s) RETURNING id",
                         (tid, project, profile, workflow,
                          *artifacts)).fetchone()[0]
    with pytest.raises(errors.ObjectNotInPrerequisiteState), db.transaction():
        db.execute("UPDATE photo.learning_versions SET version_no=2 WHERE id=%s",
                   (version,))
    for artifact in artifacts:
        assert db.execute("SELECT photo.v2_artifact_has_references(%s,%s)",
                          (tid, artifact)).fetchone()[0]
    db.execute("DELETE FROM photo.learning_versions WHERE id=%s", (version,))
    for artifact in artifacts:
        assert not db.execute("SELECT photo.v2_artifact_has_references(%s,%s)",
                              (tid, artifact)).fetchone()[0]


def test_v23_profile_is_removed_by_confirmed_tenant_purge(db):
    tid, actor = tenant(db, str(uuid4()) + '@example.test')
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) "
                         "VALUES(%s,'Purge learning') RETURNING id", (tid,)).fetchone()[0]
    profile = db.execute("INSERT INTO photo.learning_profiles(tenant_id,project_id,"
                         "name,mode,created_by) VALUES(%s,%s,'Disposable',"
                         "'photographer',%s) RETURNING id",
                         (tid, project, actor)).fetchone()[0]
    db.execute("UPDATE photo.tenants SET status='deleting' WHERE id=%s", (tid,))
    job = db.execute("INSERT INTO photo.deletion_jobs(tenant_id,resource_type,"
                     "resource_id,requested_by) VALUES(%s,'tenant',%s,%s) "
                     "RETURNING id", (tid, tid, actor)).fetchone()[0]
    claim = db.execute("SELECT job_id,cleanup_token FROM photo.claim_tenant_deletion() "
                       "WHERE job_id=%s", (job,)).fetchone()
    assert claim is not None
    assert db.execute("SELECT photo.finish_tenant_deletion(%s,%s)",
                      claim).fetchone()[0]
    assert db.execute("SELECT count(*) FROM photo.learning_profiles WHERE id=%s",
                      (profile,)).fetchone()[0] == 0
