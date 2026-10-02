"""Learning profiles can only be created through scoped trusted admission."""

from uuid import uuid4

import pytest
from psycopg import errors

from test_database import tenant


def test_profile_creation_scope_and_role(db):
    tid, actor = tenant(db, str(uuid4()) + '@example.test')
    auth_session = db.execute(
        "INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) "
        "VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') RETURNING id",
        (actor, uuid4().bytes)).fetchone()[0]
    p1 = db.execute("INSERT INTO photo.projects(tenant_id,name) "
                    "VALUES(%s,'First') RETURNING id", (tid,)).fetchone()[0]
    p2 = db.execute("INSERT INTO photo.projects(tenant_id,name) "
                    "VALUES(%s,'Second') RETURNING id", (tid,)).fetchone()[0]
    person = db.execute("INSERT INTO photo.project_persons(tenant_id,project_id,"
                        "label,state,consent_epoch) VALUES(%s,%s,'Subject','active',1) "
                        "RETURNING id", (tid, p1)).fetchone()[0]
    call = "SELECT photo.create_v23_profile(%s,%s,%s,%s,%s,%s,%s)"
    assert not db.execute("SELECT has_table_privilege('photo_api',"
                          "'photo.learning_profiles','INSERT')").fetchone()[0]
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tid),))
    db.execute('SET LOCAL ROLE photo_api')
    photographer = db.execute(call, (tid, p1, 'Personal style', 'photographer',
                                     None, actor, auth_session)).fetchone()[0]
    subject = db.execute(call, (tid, p1, 'Subject style', 'person', person,
                                actor, auth_session)).fetchone()[0]
    rows = db.execute("SELECT id,project_id,person_id,revision,active_version_id "
                      "FROM photo.learning_profiles WHERE tenant_id=%s "
                      "ORDER BY name", (tid,)).fetchall()
    assert {row[0] for row in rows} == {photographer, subject}
    assert all(row[1] == p1 and row[3] == 1 and row[4] is None for row in rows)
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute(call, (tid, p2, 'Cross-project', 'person', person,
                          actor, auth_session))
    with pytest.raises(errors.InvalidParameterValue), db.transaction():
        db.execute(call, (tid, p1, 'Mode mismatch', 'photographer', person,
                          actor, auth_session))
    with pytest.raises(errors.InsufficientPrivilege), db.transaction():
        db.execute("INSERT INTO photo.learning_profiles(tenant_id,project_id,name,"
                   "mode,created_by) VALUES(%s,%s,'Bypass','photographer',%s)",
                   (tid, p1, actor))
