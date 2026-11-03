"""02704: functions through which photo_api reads and creates video data (it has no table privileges)."""
from uuid import uuid4
import pytest
from psycopg import errors
from test_video_media_sequence_functions import derivs, finish, media, seq
from test_video_upload_functions import SHA

FUNCS = {
    'create_video_sequence': 'uuid,uuid,uuid,text,integer,integer,integer,integer', 'get_video_upload': 'uuid,uuid',
    'list_video_media': 'uuid,uuid,integer', 'get_video_media_object': 'uuid,uuid,text', 'get_video_sequence': 'uuid,uuid', 'get_video_render': 'uuid,uuid',
}


def test_api_functions_security(db):
    for fn, args in FUNCS.items():
        sig = f'photo.{fn}({args})'
        row = db.execute("SELECT prosecdef,pg_get_userbyid(proowner),has_function_privilege('public',oid,'EXECUTE'),has_function_privilege('photo_api',oid,'EXECUTE'),"
                         "has_function_privilege('photo_worker',oid,'EXECUTE') FROM pg_proc WHERE oid=%s::regprocedure", (sig,)).fetchone()
        assert row == (True, 'photo_fn_owner', False, True, False), fn
    assert not db.execute("SELECT has_table_privilege('photo_api','photo.video_media','SELECT')").fetchone()[0]


def test_create_sequence_validates_and_isolates_tenants(db):
    tid, user, pid, uid, mid = media(db)
    sql = 'SELECT photo.create_video_sequence(%s,%s,%s,%s,%s,%s,%s,%s)'
    sid = db.execute(sql, (tid, pid, user, ' Main ', 25, 1, 640, 360)).fetchone()[0]
    assert db.execute('SELECT name,head_revision FROM photo.video_sequences WHERE id=%s', (sid,)).fetchone() == ('Main', 0)
    for args in ((tid, pid, user, '', 25, 1, 640, 360), (tid, pid, user, 'x', 26, 1, 640, 360), (tid, pid, user, 'x', 25, 1, 641, 360)):
        with pytest.raises(errors.InvalidParameterValue), db.transaction():
            db.execute(sql, args)
    with pytest.raises(errors.NoDataFound), db.transaction():
        db.execute(sql, (tid, uuid4(), user, 'x', 25, 1, 640, 360))
    got = db.execute('SELECT photo.get_video_sequence(%s,%s)', (tid, sid)).fetchone()[0]
    assert got['head_revision'] == 0 and got['document'] is None
    assert db.execute('SELECT photo.get_video_sequence(%s,%s)', (uuid4(), sid)).fetchone()[0] is None  # other tenant sees nothing


def test_media_reads_only_ready_and_scoped(db):
    tid, user, pid, uid, mid = media(db)
    items = db.execute('SELECT photo.list_video_media(%s,%s,50)', (tid, pid)).fetchone()[0]
    assert [i['state'] for i in items] == ['probing'] and items[0]['source_sha256'] == SHA
    assert db.execute("SELECT photo.get_video_media_object(%s,%s,'proxy')", (tid, mid)).fetchone()[0] is None  # not ready yet
    finish(db, tid, mid, derivs(db, tid))
    o = db.execute("SELECT photo.get_video_media_object(%s,%s,'proxy')", (tid, mid)).fetchone()[0]
    assert o['bytes'] == 200 and o['bucket'] == 'bucket-x'
    assert db.execute("SELECT photo.get_video_media_object(%s,%s,'bogus')", (tid, mid)).fetchone()[0] is None
    assert db.execute("SELECT photo.get_video_media_object(%s,%s,'proxy')", (uuid4(), mid)).fetchone()[0] is None
    assert db.execute('SELECT photo.get_video_upload(%s,%s)', (tid, uid)).fetchone()[0]['state'] == 'verified'
