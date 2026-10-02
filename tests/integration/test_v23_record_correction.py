"""RecordCorrection admission is atomic, replayable and consent fenced."""

from uuid import uuid4

import pytest
from psycopg import errors
from psycopg.types.json import Jsonb

from test_database import tenant


def _fixture(db):
    tid, actor = tenant(db, str(uuid4()) + '@example.test')
    auth_session = db.execute(
        "INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) "
        "VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') RETURNING id",
        (actor, uuid4().bytes)).fetchone()[0]
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) "
                         "VALUES(%s,'Correction') RETURNING id", (tid,)).fetchone()[0]
    collection = db.execute("INSERT INTO photo.collections(tenant_id,project_id,name) "
                            "VALUES(%s,%s,'Session') RETURNING id",
                            (tid, project)).fetchone()[0]
    def artifact(digest, role='source'):
        return db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,"
                          "object_version,sha256,bytes,media_type,output_role,verified_at) "
                          "VALUES(%s,'test',%s,'v1',%s,64,'application/json',%s,now()) RETURNING id",
                          (tid, str(uuid4()), digest, role)).fetchone()[0]
    original = artifact('a' * 64)
    asset = db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,"
                       "metadata,upload_state,provenance) "
                       "VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",
                       (tid, project, original)).fetchone()[0]
    version = db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,"
                         "master_artifact_id,content_hash,metadata,review_state) "
                         "VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",
                         (tid, asset, original, 'a' * 64)).fetchone()[0]
    db.execute("INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,"
               "asset_version_id,position) VALUES(%s,%s,%s,%s,1)",
               (tid, collection, asset, version))
    shoot = db.execute("INSERT INTO photo.shoot_sessions(tenant_id,project_id,"
                       "collection_id,name,state,policy,consent_epoch) "
                       "VALUES(%s,%s,%s,'Test','open','{}',1) RETURNING id",
                       (tid, project, collection)).fetchone()[0]
    stream = db.execute("INSERT INTO photo.v2_document_streams(tenant_id,project_id,"
                        "asset_version_id,stream_type) VALUES(%s,%s,%s,'draft') RETURNING id",
                        (tid, project, version)).fetchone()[0]
    base, approved = uuid4(), uuid4()
    base_artifact, approved_artifact, resolved = (
        artifact(c * 64, 'document') for c in 'bcd')
    for document, number, aid, digest in (
        (base, 1, base_artifact, 'b' * 64),
        (approved, 2, approved_artifact, 'c' * 64)):
        db.execute("INSERT INTO photo.v2_documents(id,tenant_id,project_id,stream_id,"
                   "version_no,document_type,artifact_id,sha256) "
                   "VALUES(%s,%s,%s,%s,%s,'EditingRecipeV3',%s,%s)",
                   (document, tid, project, stream, number, aid, digest))
    db.execute("UPDATE photo.v2_document_streams SET current_document_id=%s,"
               "approved_document_id=%s WHERE id=%s", (approved, approved, stream))
    db.execute("INSERT INTO photo.v2_document_approvals(tenant_id,project_id,"
               "document_id,actor_id,recipe_sha,resolved_artifact_id) "
               "VALUES(%s,%s,%s,%s,%s,%s)",
               (tid, project, approved, actor, 'c' * 64, resolved))
    consent = db.execute("INSERT INTO photo.consent_grants(tenant_id,project_id,"
                         "purpose,status,epoch,policy_version,actor_id) "
                         "VALUES(%s,%s,'learning_photographer','granted',1,'v1',%s) "
                         "RETURNING id", (tid, project, actor)).fetchone()[0]
    request = dict(client_event_id=str(uuid4()), session_id=str(shoot),
                   asset_version_id=str(version), base_document_id=str(base),
                   approved_document_id=str(approved), reason='exposure', learn=True,
                   person_id=None, scene_group_id=None, consent_id=str(consent),
                   consent_epoch='1')
    return tid, actor, auth_session, project, request, consent


def _record(db, tid, actor, session, request, paths=('/ev',)):
    return db.execute("SELECT photo.record_v2_correction(%s,%s,%s,%s,%s,%s,%s)",
                      (tid, actor, session, Jsonb(request), Jsonb(list(paths)),
                       'b' * 64, 'c' * 64)).fetchone()[0]


def test_record_correction_replay_and_live_consent(db):
    tid, actor, session, project, request, consent = _fixture(db)
    assert not db.execute("SELECT has_table_privilege('photo_api',"
                          "'photo.correction_events','INSERT')").fetchone()[0]
    assert db.execute("SELECT has_function_privilege('photo_api',"
                      "'photo.record_v2_correction(uuid,uuid,uuid,jsonb,jsonb,text,text)',"
                      "'EXECUTE')").fetchone()[0]
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tid),))
    db.execute('SET LOCAL ROLE photo_api')
    first = _record(db, tid, actor, session, request)
    replay = _record(db, tid, actor, session, request)
    assert first == replay
    assert first['project_id'] == str(project)
    assert first['approved_by'] == str(actor)
    assert first['changed_paths'] == ['/ev']
    assert first['consent_epoch'] == '1'
    changed = dict(request, reason='color')
    with pytest.raises(errors.SerializationFailure), db.transaction():
        _record(db, tid, actor, session, changed)
    db.execute('RESET ROLE')
    db.execute("UPDATE photo.consent_grants SET status='revoked',epoch=2 "
               "WHERE id=%s", (consent,))
    db.execute('SET LOCAL ROLE photo_api')
    assert _record(db, tid, actor, session, request) == first
    fresh = dict(request, client_event_id=str(uuid4()))
    with pytest.raises(errors.CheckViolation), db.transaction():
        _record(db, tid, actor, session, fresh)


def test_record_correction_rejects_unapproved_and_wrong_session(db):
    tid, actor, session, project, request, _ = _fixture(db)
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tid),))
    db.execute('SET LOCAL ROLE photo_api')
    wrong_shoot = dict(request, session_id=str(uuid4()))
    with pytest.raises(errors.CheckViolation), db.transaction():
        _record(db, tid, actor, session, wrong_shoot)
    with pytest.raises(errors.InvalidParameterValue), db.transaction():
        _record(db, tid, actor, session, request, ('/crop',))
    assert db.execute("SELECT count(*) FROM photo.correction_events "
                      "WHERE tenant_id=%s", (tid,)).fetchone()[0] == 0
