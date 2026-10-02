"""Training inventory pins eligible source references and live consent state."""

from uuid import UUID, uuid4

import pytest
from psycopg import errors

from test_v23_record_correction import _fixture, _record


def test_training_candidate_inventory_scoped_and_consent_fenced(db):
    tid, actor, auth_session, project, request, consent = _fixture(db)
    collection = db.execute("SELECT collection_id FROM photo.shoot_sessions "
                            "WHERE id=%s", (request['session_id'],)).fetchone()[0]
    sessions = [UUID(request['session_id'])]
    for index in range(5):
        sid = db.execute("INSERT INTO photo.shoot_sessions(tenant_id,project_id,"
                         "collection_id,name,state,policy,consent_epoch) "
                         "VALUES(%s,%s,%s,%s,'open','{}',1) RETURNING id",
                         (tid, project, collection, f'Session {index}')).fetchone()[0]
        sessions.append(sid)
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tid),))
    db.execute('SET LOCAL ROLE photo_api')
    _record(db, tid, actor, auth_session, request)
    profile = db.execute("SELECT photo.create_v23_profile(%s,%s,%s,%s,%s,%s,%s)",
                         (tid, project, 'My model', 'photographer', None,
                          actor, auth_session)).fetchone()[0]
    call = "SELECT * FROM photo.v23_training_candidate_rows(%s,%s,%s,%s,%s,%s)"
    rows = db.execute(call, (tid, profile, 1, sessions, actor, auth_session)).fetchall()
    assert len(rows) == 1
    item = rows[0]
    assert str(item[1]) == request['session_id']
    assert item[3] == 'a' * 64 and item[4] == 'a' * 64
    assert item[11] is not None  # exact human approval
    assert item[13] == consent and item[14] == 1
    assert item[15] == ['/ev']
    assert item[16:20] == (True, True, True, True)
    with pytest.raises(errors.SerializationFailure), db.transaction():
        db.execute(call, (tid, profile, 2, sessions, actor, auth_session))
    wrong = list(sessions)
    wrong[-1] = uuid4()
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute(call, (tid, profile, 1, wrong, actor, auth_session))
    db.execute('RESET ROLE')
    db.execute("UPDATE photo.consent_grants SET status='revoked',epoch=2 "
               "WHERE id=%s", (consent,))
    db.execute('SET LOCAL ROLE photo_api')
    rows = db.execute(call, (tid, profile, 1, sessions, actor, auth_session)).fetchall()
    assert len(rows) == 1 and rows[0][16] is False
