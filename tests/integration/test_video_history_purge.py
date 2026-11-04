"""02711: retention of history (FN-D-21)."""
from psycopg.types.json import Jsonb
from test_video_job_functions import mk_job
from test_video_media_sequence_functions import media
from test_video_tables_schema import ctx
from test_video_upload_functions import begin, give_quota, upload_all
from test_video_viewer_functions import delivery


def test_security(db):
    row = db.execute("SELECT prosecdef,pg_get_userbyid(proowner),has_function_privilege('public',oid,'EXECUTE'),has_function_privilege('photo_maintenance',oid,'EXECUTE'),"
                     "has_function_privilege('photo_api',oid,'EXECUTE') FROM pg_proc WHERE oid='photo.purge_video_history(integer)'::regprocedure").fetchone()
    assert row == (True, 'photo_fn_owner', False, True, False)


def test_old_dead_uploads_are_removed_but_media_uploads_stay(db):
    tid, user, pid = ctx(db); give_quota(db, tid)
    dead = begin(db, tid, pid, user)
    db.execute('SELECT photo.abort_video_upload(%s,%s)', (tid, dead))
    fresh = begin(db, tid, pid, user)
    db.execute('SELECT photo.abort_video_upload(%s,%s)', (tid, fresh))
    tid2, user2, pid2, uid, mid = media(db)  # an upload that became media
    db.execute("UPDATE photo.video_upload_sessions SET updated_at=clock_timestamp()-interval '91 days' WHERE id IN(%s,%s)", (dead, uid))
    assert db.execute('SELECT photo.purge_video_history(100)').fetchone()[0] >= 1
    exists = lambda i: db.execute('SELECT count(*) FROM photo.video_upload_sessions WHERE id=%s', (i,)).fetchone()[0]
    assert (exists(dead), exists(fresh), exists(uid)) == (0, 1, 1)
    assert db.execute('SELECT photo.purge_video_history(100)').fetchone()[0] == 0  # idempotent


def test_old_unreferenced_jobs_are_removed(db):
    tid, user, pid, uid, mid = media(db)
    old = mk_job(db, tid, pid, user, 'ingest', mid)
    kept = mk_job(db, tid, pid, user, 'ingest', mid)
    db.execute("UPDATE photo.video_jobs SET state='succeeded',finished_at=clock_timestamp()-interval '181 days' WHERE id=%s", (old,))
    db.execute("UPDATE photo.video_jobs SET state='succeeded',finished_at=clock_timestamp()-interval '10 days' WHERE id=%s", (kept,))
    db.execute('SELECT photo.purge_video_history(100)')
    left = {r[0] for r in db.execute('SELECT id FROM photo.video_jobs WHERE id IN(%s,%s)', (old, kept))}
    assert left == {kept}


def test_old_hourly_counters_fold_into_days(db):
    tid, user, cid, rid = delivery(db)
    sh = db.execute('SELECT id FROM photo.video_renders WHERE id=%s', (rid,)).fetchone()[0]
    for hour, b in ((3, 10), (5, 20)):
        db.execute("INSERT INTO photo.video_egress_counters(tenant_id,share_id,render_id,kind,bucket_start,bytes,requests) "
                   "VALUES(%s,%s,%s,'stream',date_trunc('day',clock_timestamp()-interval '401 days')+%s*interval '1 hour',%s,1)", (tid, sh, rid, hour, b))
    db.execute('SELECT photo.purge_video_history(100)')
    rows = db.execute('SELECT bucket_start=date_trunc(%s,bucket_start),bytes,requests FROM photo.video_egress_counters WHERE tenant_id=%s', ('day', tid)).fetchall()
    assert rows == [(True, 30, 2)]
    db.execute('SELECT photo.purge_video_history(100)')
    assert db.execute('SELECT count(*),sum(bytes) FROM photo.video_egress_counters WHERE tenant_id=%s', (tid,)).fetchone() == (1, 30)
