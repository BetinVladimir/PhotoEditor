"""Read one frozen candidate inventory through the API-role Go adapter."""

import os
import subprocess

import psycopg
from psycopg.conninfo import make_conninfo

from conftest import ROOT
from test_v23_record_correction import _fixture, _record


def test_v23_candidate_source_go(database):
    with psycopg.connect(**database) as db:
        tid, actor, auth_session, project, request, _ = _fixture(db)
        collection = db.execute("SELECT collection_id FROM photo.shoot_sessions "
                                "WHERE id=%s", (request['session_id'],)).fetchone()[0]
        sessions = [request['session_id']]
        for index in range(5):
            sid = db.execute("INSERT INTO photo.shoot_sessions(tenant_id,project_id,"
                             "collection_id,name,state,policy,consent_epoch) "
                             "VALUES(%s,%s,%s,%s,'open','{}',1) RETURNING id",
                             (tid, project, collection, f'Go session {index}')).fetchone()[0]
            sessions.append(sid)
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tid),))
        db.execute('SET LOCAL ROLE photo_api')
        _record(db, tid, actor, auth_session, request)
        profile = db.execute("SELECT photo.create_v23_profile(%s,%s,%s,%s,%s,%s,%s)",
                             (tid, project, 'Model', 'photographer', None,
                              actor, auth_session)).fetchone()[0]
    env = os.environ.copy()
    env['PHOTO_TEST_DATABASE_URL'] = make_conninfo(**database)
    env['PHOTO_TEST_LEARNING_IDS'] = ','.join(map(str, [tid, profile, actor,
                                                         auth_session, *sessions]))
    env['GOCACHE'] = str(ROOT / 'work' / 'go-cache')
    result = subprocess.run(
        ['go', 'test', './internal/v2learning',
         '-run', '^TestIntegrationTrainingCandidateSource$', '-count=1'],
        cwd=ROOT / 'backend', env=env, capture_output=True, text=True,
        timeout=60,
    )
    assert result.returncode == 0, result.stdout + result.stderr
