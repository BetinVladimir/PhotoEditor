"""02705 viewer functions and the 02706 fix of the portal share exchange (needs a real share)."""
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb
from test_video_e2e_db import FP, render_row
from test_video_media_sequence_functions import artifact, derivs, finish, media, seq


def delivery(db, publish=True):
    tid, user, pid, uid, mid = media(db)
    finish(db, tid, mid, derivs(db, tid))
    sid = seq(db, tid, pid, user)
    rid = render_row(db, tid, pid, user, sid, 1, 'c' * 64)
    db.execute('SELECT photo.finish_video_render(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
               (tid, rid, 'ready', artifact(db, tid, 'video_render', 4000), artifact(db, tid, 'video_qc_report', 100), 705600000, 'libx264', 'cpu', FP))
    cid = db.execute('SELECT photo.create_video_delivery(%s,%s,%s)', (tid, pid, 'Film')).fetchone()[0]
    db.execute('SELECT photo.add_video_render_to_delivery(%s,%s,%s,%s)', (tid, cid, rid, 'Full'))
    if publish:
        db.execute('SELECT photo.publish_video_delivery(%s,%s,true)', (tid, cid))
    return tid, user, cid, rid


def test_viewer_functions_security(db):
    for sig in ('get_video_delivery_view(uuid,uuid)', 'get_video_delivery_object(uuid,uuid,uuid)'):
        row = db.execute("SELECT prosecdef,pg_get_userbyid(proowner),has_function_privilege('public',oid,'EXECUTE'),has_function_privilege('photo_portal',oid,'EXECUTE'),"
                         "has_function_privilege('photo_api',oid,'EXECUTE') FROM pg_proc WHERE oid=%s::regprocedure", (f'photo.{sig}',)).fetchone()
        assert row == (True, 'photo_fn_owner', False, True, False), sig


def test_view_and_object_only_for_published_delivery(db):
    tid, user, cid, rid = delivery(db, publish=False)
    assert db.execute('SELECT photo.get_video_delivery_view(%s,%s)', (tid, cid)).fetchone()[0] is None
    assert db.execute('SELECT photo.get_video_delivery_object(%s,%s,%s)', (tid, cid, rid)).fetchone()[0] is None
    db.execute('SELECT photo.publish_video_delivery(%s,%s,true)', (tid, cid))
    v = db.execute('SELECT photo.get_video_delivery_view(%s,%s)', (tid, cid)).fetchone()[0]
    assert v['name'] == 'Film' and [(i['render_id'], i['title'], i['bytes']) for i in v['items']] == [(str(rid), 'Full', 4000)]
    o = db.execute('SELECT photo.get_video_delivery_object(%s,%s,%s)', (tid, cid, rid)).fetchone()[0]
    assert o['bytes'] == 4000 and o['media_type'] == 'application/octet-stream'
    assert db.execute('SELECT photo.get_video_delivery_object(%s,%s,%s)', (tid, cid, uuid4())).fetchone()[0] is None  # render not in this delivery
    assert db.execute('SELECT photo.get_video_delivery_object(%s,%s,%s)', (uuid4(), cid, rid)).fetchone()[0] is None  # other tenant


def test_share_exchange_works_for_existing_share(db):
    tid, user, cid, rid = delivery(db)
    share = uuid4()
    db.execute('SELECT photo.create_video_share(%s,%s,%s,%s,%s,true,now()+interval \'1 day\',%s,%s)', (tid, cid, share, bytes(32), 'c', Jsonb({}), user))
    row = db.execute("SELECT tenant_id,collection_id,permissions FROM photo.exchange_share_session(%s,%s,%s,%s,'en',now()+interval '1 hour')",
                     (share, bytes(32), b'\x01' * 32, b'\x02' * 32)).fetchone()
    assert row[0] == tid and row[1] == cid and row[2]['download'] is True
    got = db.execute("SELECT tenant_id,share_id,collection_id FROM photo.authorize_portal_session(%s,NULL,'view')", (b'\x01' * 32,)).fetchone()
    assert got == (tid, share, cid)
    assert db.execute("SELECT count(*) FROM photo.exchange_share_session(%s,%s,%s,%s,'en',now()+interval '1 hour')", (share, b'\x09' * 32, b'\x03' * 32, b'\x04' * 32)).fetchone()[0] == 0  # wrong secret
