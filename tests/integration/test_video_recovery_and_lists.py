"""02707: return of stuck upload completions and the delivery list for the studio UI."""
from uuid import uuid4
from psycopg.types.json import Jsonb
from test_video_tables_schema import ctx
from test_video_upload_functions import PART, SHA, begin, give_quota, upload_all
from test_video_viewer_functions import delivery


def test_functions_security(db):
    for sig, role, other in (('recover_video_upload_completions(integer)', 'photo_maintenance', 'photo_api'),
                             ('list_video_deliveries(uuid,uuid)', 'photo_api', 'photo_portal')):
        row = db.execute("SELECT prosecdef,pg_get_userbyid(proowner),has_function_privilege('public',oid,'EXECUTE'),has_function_privilege(%s,oid,'EXECUTE'),"
                         "has_function_privilege(%s,oid,'EXECUTE') FROM pg_proc WHERE oid=%s::regprocedure", (role, other, f'photo.{sig}')).fetchone()
        assert row == (True, 'photo_fn_owner', False, True, False), sig


def test_expired_completion_returns_to_uploading_and_can_finish_again(db):
    tid, user, pid = ctx(db); give_quota(db, tid)
    size = PART + 5
    uid = begin(db, tid, pid, user, size)
    parts = upload_all(db, tid, uid, size)
    tok = db.execute('SELECT photo.complete_video_upload(%s,%s,%s)', (tid, uid, Jsonb(parts))).fetchone()[0]
    assert db.execute('SELECT photo.recover_video_upload_completions(10)').fetchone()[0] == 0  # lease still valid
    db.execute("UPDATE photo.video_upload_sessions SET completion_lease_until=clock_timestamp()-interval '1 second' WHERE id=%s", (uid,))
    assert db.execute('SELECT photo.recover_video_upload_completions(10)').fetchone()[0] == 1
    assert db.execute('SELECT state,completion_token FROM photo.video_upload_sessions WHERE id=%s', (uid,)).fetchone() == ('uploading', None)
    assert db.execute("SELECT photo.finish_video_upload_completion(%s,%s,%s,'bucket-x','v1',%s,'video/mp4')", (tid, uid, tok, SHA)).fetchone()[0] is None  # late finisher
    tok2 = db.execute('SELECT photo.complete_video_upload(%s,%s,%s)', (tid, uid, Jsonb(parts))).fetchone()[0]
    assert tok2 != tok
    assert db.execute("SELECT photo.finish_video_upload_completion(%s,%s,%s,'bucket-x','v1',%s,'video/mp4')", (tid, uid, tok2, SHA)).fetchone()[0]


def test_expired_session_is_not_recovered(db):
    tid, user, pid = ctx(db); give_quota(db, tid)
    uid = begin(db, tid, pid, user, PART + 5)
    db.execute('SELECT photo.complete_video_upload(%s,%s,%s)', (tid, uid, Jsonb(upload_all(db, tid, uid, PART + 5)))).fetchone()
    db.execute("UPDATE photo.video_upload_sessions SET completion_lease_until=clock_timestamp()-interval '1 second',expires_at=clock_timestamp()-interval '1 second' WHERE id=%s", (uid,))
    assert db.execute('SELECT photo.recover_video_upload_completions(10)').fetchone()[0] == 0
    assert db.execute('SELECT photo.expire_video_uploads(10)').fetchone()[0] == 1


def test_list_deliveries_with_items_and_shares(db):
    tid, user, cid, rid = delivery(db)
    pid = db.execute('SELECT project_id FROM photo.collections WHERE id=%s', (cid,)).fetchone()[0]
    sid = uuid4()
    db.execute("SELECT photo.create_video_share(%s,%s,%s,%s,'Client',true,clock_timestamp()+interval '1 day','{}'::jsonb,%s)", (tid, cid, sid, b'x' * 32, user))
    out = db.execute('SELECT photo.list_video_deliveries(%s,%s)', (tid, pid)).fetchone()[0]
    assert len(out) == 1 and out[0]['state'] == 'published' and out[0]['items'][0]['title'] == 'Full'
    assert [(s['id'], s['label'], s['download'], s['revoked']) for s in out[0]['shares']] == [(str(sid), 'Client', True, False)]
    db.execute('SELECT photo.revoke_video_share(%s,%s)', (tid, sid))
    assert db.execute('SELECT photo.list_video_deliveries(%s,%s)', (tid, pid)).fetchone()[0][0]['shares'][0]['revoked'] is True
    assert db.execute('SELECT photo.list_video_deliveries(%s,%s)', (uuid4(), pid)).fetchone()[0] == []
