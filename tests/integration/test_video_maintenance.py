"""02709: autosave compaction (FN-D-16) and reconciliation of stuck ingest (FN-D-22)."""
import pytest
from test_video_media_sequence_functions import artifact, derivs, finish, media, save, seq
from test_video_upload_functions import SHA, usage


def test_security(db):
    for sig in ('compact_video_autosaves(integer)', 'reconcile_video_stuck(integer)'):
        row = db.execute("SELECT prosecdef,pg_get_userbyid(proowner),has_function_privilege('public',oid,'EXECUTE'),has_function_privilege('photo_maintenance',oid,'EXECUTE'),"
                         "has_function_privilege('photo_api',oid,'EXECUTE') FROM pg_proc WHERE oid=%s::regprocedure", (f'photo.{sig}',)).fetchone()
        assert row == (True, 'photo_fn_owner', False, True, False), sig


def test_compaction_keeps_last_of_day_manual_head_and_rendered(db):
    tid, user, pid, uid, mid = media(db)
    finish(db, tid, mid, derivs(db, tid))
    sid = seq(db, tid, pid, user)
    src = [{'kind': 'video', 'id': str(mid), 'sha256': SHA}]
    docs = []
    for i, label in enumerate(['autosave', 'autosave', 'autosave', 'manual', 'autosave', 'autosave'], start=1):
        sha = f'{i:x}' * 64
        d = artifact(db, tid, 'video_sequence_document', sha=sha); docs.append(d)
        assert save(db, tid, sid, i - 1, d, sha, src, label=label, user=user) == i
    db.execute("SET LOCAL session_replication_role = replica")  # backdating needs the immutability trigger off
    # rev 1,2 same old day; rev 3 next old day; rev 4 manual; rev 5 and 6 share a day (6 is head)
    for rev, ago in ((1, '41 days 3 hours'), (2, '41 days 2 hours'), (3, '40 days'), (4, '40 days'), (5, '39 days'), (6, '39 days')):
        db.execute("UPDATE photo.video_sequence_revisions SET created_at=clock_timestamp()-%s::interval WHERE sequence_id=%s AND revision=%s", (ago, sid, rev))
    db.execute("SET LOCAL session_replication_role = origin")
    n = db.execute('SELECT photo.compact_video_autosaves(100)').fetchone()[0]
    assert n >= 1
    left = [r[0] for r in db.execute('SELECT revision FROM photo.video_sequence_revisions WHERE sequence_id=%s ORDER BY revision', (sid,))]
    assert left == [2, 3, 4, 6]  # 1 and 5 have a newer autosave the same day; 2, 3 close their day; 4 is manual; 6 is head
    assert db.execute('SELECT expires_at IS NOT NULL FROM photo.artifacts WHERE id=%s', (docs[0],)).fetchone()[0]
    assert db.execute('SELECT expires_at IS NULL FROM photo.artifacts WHERE id=%s', (docs[1],)).fetchone()[0]
    assert db.execute('SELECT photo.compact_video_autosaves(100)').fetchone()[0] == 0  # idempotent


def test_reconcile_rejects_abandoned_probing_and_releases_quota(db):
    tid, user, pid, uid, mid = media(db)
    assert usage(db, tid)[0] > 0
    db.execute('SELECT photo.reconcile_video_stuck(1000)')  # leftovers of other tests may be reconciled; ours is fresh
    assert db.execute('SELECT state FROM photo.video_media WHERE id=%s', (mid,)).fetchone()[0] == 'probing'
    db.execute("UPDATE photo.video_media SET updated_at=clock_timestamp()-interval '1 hour' WHERE id=%s", (mid,))
    assert db.execute('SELECT photo.reconcile_video_stuck(1000)').fetchone()[0] >= 1
    assert db.execute('SELECT state,reject_code FROM photo.video_media WHERE id=%s', (mid,)).fetchone() == ('rejected', 'VIDEO_INGEST_ABANDONED')
    assert usage(db, tid)[0] == 0
    assert db.execute('SELECT photo.reconcile_video_stuck(1000)').fetchone()[0] == 0
