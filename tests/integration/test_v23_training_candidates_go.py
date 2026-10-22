"""Read one frozen candidate inventory through the API-role Go adapter."""

import json
import os
import subprocess

import psycopg
from psycopg.conninfo import make_conninfo

from conftest import ROOT
from test_v23_record_correction import _fixture, _record




def _assert_exact_go_root_executed(result, package, root):
    """A successful exit alone may hide a skipped or unmatched Go selector."""
    from hashlib import sha256
    from pathlib import Path
    from uuid import uuid4
    events=[]
    invalid_json=False
    for line in result.stdout.splitlines():
        if not line.startswith('{'):continue
        try:event=json.loads(line)
        except (ValueError,TypeError):
            invalid_json=True
            continue
        if not isinstance(event,dict):
            invalid_json=True
            continue
        events.append({key:event[key] for key in ('Action','Package','Test','Elapsed') if key in event})
    evidence=Path(__file__).resolve().parents[2]/'work/specialized-three-go-evidence'
    evidence.mkdir(parents=True,exist_ok=True)
    path=evidence/(root+'-'+uuid4().hex+'.json')
    path.write_text(json.dumps(dict(selector='^'+root+'$',package=package,
        exit_code=result.returncode,events=events,invalid_json=invalid_json,
        stdout_sha256=sha256(result.stdout.encode()).hexdigest(),
        stderr_sha256=sha256(result.stderr.encode()).hexdigest()),indent=2)+'\n')
    assert not invalid_json,str(path)
    assert events, 'Go JSON events required'
    assert not [event for event in events if event.get('Action') in ('skip','fail')], 'Go root/child SKIP or FAIL is not execution evidence'
    for action in ('run','pass'):
        assert sum(event.get('Action')==action and event.get('Test')==root
            and event.get('Package')==package for event in events)==1, 'exact selected Go root '+action+' required once'

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
        ['go', 'test', '-json', './internal/v2learning',
         '-run', '^TestIntegrationTrainingCandidateSource$', '-count=1'],
        cwd=ROOT / 'backend', env=env, capture_output=True, text=True,
        timeout=60,
    )
    _assert_exact_go_root_executed(result,'photoeditor/backend/internal/v2learning','TestIntegrationTrainingCandidateSource')
    assert result.returncode == 0, result.stdout + result.stderr
