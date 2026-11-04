"""02708: two-phase withdrawal of library media (FN-D-13)."""
from uuid import uuid4
import pytest
from psycopg import errors
from test_video_media_sequence_functions import derivs, finish, media, usage
from test_video_upload_functions import SHA


def ready_media(db):
    tid, user, pid, uid, mid = media(db)
    finish(db, tid, mid, derivs(db, tid))
    return tid, user, mid


def test_security(db):
    for sig, role, other in (('withdraw_video_media(uuid,uuid)', 'photo_api', 'photo_maintenance'),
                             ('list_video_media_deletions(integer)', 'photo_maintenance', 'photo_api'),
                             ('finish_video_media_delete(uuid,uuid)', 'photo_maintenance', 'photo_api')):
        row = db.execute("SELECT prosecdef,pg_get_userbyid(proowner),has_function_privilege('public',oid,'EXECUTE'),has_function_privilege(%s,oid,'EXECUTE'),"
                         "has_function_privilege(%s,oid,'EXECUTE') FROM pg_proc WHERE oid=%s::regprocedure", (role, other, f'photo.{sig}')).fetchone()
        assert row == (True, 'photo_fn_owner', False, True, False), sig


def test_quota_is_released_only_in_the_second_phase(db):
    tid, user, mid = ready_media(db)
    before = usage(db, tid)
    assert before[1] > 0
    assert db.execute('SELECT photo.withdraw_video_media(%s,%s)', (tid, mid)).fetchone()[0]
    assert db.execute('SELECT photo.withdraw_video_media(%s,%s)', (tid, mid)).fetchone()[0]  # idempotent
    assert db.execute('SELECT state,withdrawn_at IS NOT NULL FROM photo.video_media WHERE id=%s', (mid,)).fetchone() == ('withdrawing', True)
    assert usage(db, tid) == before
    todo = [x for x in db.execute('SELECT photo.list_video_media_deletions(100)').fetchone()[0] if x['media_id'] == str(mid)]
    assert len(todo) == 1 and len(todo[0]['objects']) == 5
    assert db.execute('SELECT photo.finish_video_media_delete(%s,%s)', (tid, mid)).fetchone()[0]
    assert db.execute('SELECT photo.finish_video_media_delete(%s,%s)', (tid, mid)).fetchone()[0]  # replay: no second debit
    assert usage(db, tid)[1] == 0
    assert db.execute('SELECT state FROM photo.video_media WHERE id=%s', (mid,)).fetchone()[0] == 'deleted'
    assert not [x for x in db.execute('SELECT photo.list_video_media_deletions(100)').fetchone()[0] if x['media_id'] == str(mid)]
    with pytest.raises(errors.NoDataFound), db.transaction():
        db.execute('SELECT photo.withdraw_video_media(%s,%s)', (tid, mid))


def test_guards(db):
    tid, user, pid, uid, mid = media(db)  # still probing
    with pytest.raises(errors.ObjectNotInPrerequisiteState), db.transaction():
        db.execute('SELECT photo.withdraw_video_media(%s,%s)', (tid, mid))
    with pytest.raises(errors.NoDataFound), db.transaction():
        db.execute('SELECT photo.withdraw_video_media(%s,%s)', (uuid4(), mid))
    assert db.execute('SELECT photo.finish_video_media_delete(%s,%s)', (tid, mid)).fetchone()[0] is False  # not withdrawing
