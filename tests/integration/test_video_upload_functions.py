"""02699 FN-D-01..05: video upload lifecycle, quota accounting, idempotency and limits."""
from uuid import uuid4
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from test_video_tables_schema import ctx

PART = 33554432
SHA = 'b' * 64


def usage(db, tid):
    return db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tid,)).fetchone()


def give_quota(db, tid, n=10 ** 12):
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT(tenant_id) DO NOTHING', (tid,))
    db.execute("UPDATE photo.tenants SET quotas=quotas||jsonb_build_object('storage_bytes',%s::bigint) WHERE id=%s", (n, tid))


def begin(db, tid, pid, user, size=PART * 2 + 5, req=None, reserve=None, sha=SHA):
    parts = -(-size // PART)
    return db.execute('SELECT photo.begin_video_upload(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                      (tid, pid, user, 'video', 'a.mp4', size, sha, req or uuid4().hex * 2, PART, parts, f'k/{uuid4()}', reserve or size + 1000)).fetchone()[0]


def upload_all(db, tid, uid, size):
    parts = -(-size // PART)
    assert db.execute("SELECT photo.bind_video_upload_multipart(%s,%s,'mp-1')", (tid, uid)).fetchone()[0]
    for n in range(1, parts + 1):
        b = PART if n < parts else size - (parts - 1) * PART
        assert db.execute('SELECT photo.record_video_upload_part(%s,%s,%s,%s,%s)', (tid, uid, n, f'e{n}', b)).fetchone()[0]
    return [{'part_no': n, 'etag': f'e{n}'} for n in range(1, parts + 1)]


def test_full_upload_flow(db):
    tid, user, pid = ctx(db); give_quota(db, tid)
    size = PART * 2 + 5
    uid = begin(db, tid, pid, user, size)
    assert usage(db, tid)[0] == size + 1000
    parts = upload_all(db, tid, uid, size)
    tok = db.execute('SELECT photo.complete_video_upload(%s,%s,%s)', (tid, uid, Jsonb(parts))).fetchone()[0]
    assert tok and db.execute('SELECT photo.complete_video_upload(%s,%s,%s)', (tid, uid, Jsonb(parts))).fetchone()[0] == tok
    mid = db.execute("SELECT photo.finish_video_upload_completion(%s,%s,%s,'bucket-x','v1',%s,'video/mp4')", (tid, uid, tok, SHA)).fetchone()[0]
    assert mid
    assert db.execute('SELECT state,source_bytes FROM photo.video_media WHERE id=%s', (mid,)).fetchone() == ('probing', size)
    assert db.execute("SELECT a.output_role FROM photo.artifacts a JOIN photo.video_media m ON m.source_artifact_id=a.id WHERE m.id=%s", (mid,)).fetchone()[0] == 'video_original'
    assert db.execute('SELECT state FROM photo.video_upload_sessions WHERE id=%s', (uid,)).fetchone()[0] == 'verifying'
    assert db.execute("SELECT photo.finish_video_upload_completion(%s,%s,%s,'bucket-x','v1',%s,'video/mp4')", (tid, uid, tok, SHA)).fetchone()[0] == mid


def test_begin_is_idempotent_and_detects_key_reuse(db):
    tid, user, pid = ctx(db); give_quota(db, tid)
    req = 'c' * 64
    a = begin(db, tid, pid, user, req=req)
    assert begin(db, tid, pid, user, req=req) == a
    assert usage(db, tid)[0] == PART * 2 + 5 + 1000
    with pytest.raises(errors.UniqueViolation), db.transaction():
        begin(db, tid, pid, user, req=req, size=PART * 2 + 6)


def test_quota_and_reserve_bounds(db):
    tid, user, pid = ctx(db); give_quota(db, tid, PART * 3)
    with pytest.raises(errors.ConfigurationLimitExceeded), db.transaction():
        begin(db, tid, pid, user, size=PART * 3)
    with pytest.raises(errors.InvalidParameterValue), db.transaction():
        begin(db, tid, pid, user, size=PART, reserve=PART - 1)
    assert usage(db, tid)[0] == 0


def test_active_upload_limit(db):
    tid, user, pid = ctx(db); give_quota(db, tid)
    for _ in range(3):
        begin(db, tid, pid, user)
    with pytest.raises(errors.ConfigurationLimitExceeded), db.transaction():
        begin(db, tid, pid, user)


def test_part_rules_and_complete_mismatch(db):
    tid, user, pid = ctx(db); give_quota(db, tid)
    size = PART + 7
    uid = begin(db, tid, pid, user, size)
    assert not db.execute("SELECT photo.record_video_upload_part(%s,%s,1,'e1',%s)", (tid, uid, PART)).fetchone()[0]  # not uploading yet
    db.execute("SELECT photo.bind_video_upload_multipart(%s,%s,'mp')", (tid, uid))
    assert not db.execute("SELECT photo.record_video_upload_part(%s,%s,1,'e1',%s)", (tid, uid, PART - 1)).fetchone()[0]
    assert not db.execute("SELECT photo.record_video_upload_part(%s,%s,3,'e3',7)", (tid, uid)).fetchone()[0]
    assert db.execute("SELECT photo.record_video_upload_part(%s,%s,1,'e1',%s)", (tid, uid, PART)).fetchone()[0]
    assert db.execute("SELECT photo.record_video_upload_part(%s,%s,1,'e1',%s)", (tid, uid, PART)).fetchone()[0]
    assert not db.execute("SELECT photo.record_video_upload_part(%s,%s,1,'other',%s)", (tid, uid, PART)).fetchone()[0]
    with pytest.raises(errors.InvalidParameterValue), db.transaction():
        db.execute('SELECT photo.complete_video_upload(%s,%s,%s)', (tid, uid, Jsonb([{'part_no': 1, 'etag': 'e1'}])))


def test_hash_mismatch_rejects_and_releases(db):
    tid, user, pid = ctx(db); give_quota(db, tid)
    size = PART + 7
    uid = begin(db, tid, pid, user, size)
    tok = db.execute('SELECT photo.complete_video_upload(%s,%s,%s)', (tid, uid, Jsonb(upload_all(db, tid, uid, size)))).fetchone()[0]
    assert db.execute("SELECT photo.finish_video_upload_completion(%s,%s,%s,'bucket-x','v1',%s,'video/mp4')", (tid, uid, tok, 'f' * 64)).fetchone()[0] is None
    assert db.execute('SELECT state,reject_code FROM photo.video_upload_sessions WHERE id=%s', (uid,)).fetchone() == ('rejected', 'UPLOAD_HASH_MISMATCH')
    assert usage(db, tid)[0] == 0


def test_wrong_token_does_nothing(db):
    tid, user, pid = ctx(db); give_quota(db, tid)
    size = PART + 7
    uid = begin(db, tid, pid, user, size)
    db.execute('SELECT photo.complete_video_upload(%s,%s,%s)', (tid, uid, Jsonb(upload_all(db, tid, uid, size))))
    assert db.execute("SELECT photo.finish_video_upload_completion(%s,%s,%s,'bucket-x','v1',%s,'video/mp4')", (tid, uid, uuid4(), SHA)).fetchone()[0] is None


def test_abort_and_expire_release_reservation(db):
    tid, user, pid = ctx(db); give_quota(db, tid)
    a = begin(db, tid, pid, user); b = begin(db, tid, pid, user)
    assert usage(db, tid)[0] == 2 * (PART * 2 + 5 + 1000)
    assert db.execute('SELECT photo.abort_video_upload(%s,%s)', (tid, a)).fetchone()[0]
    assert db.execute('SELECT photo.abort_video_upload(%s,%s)', (tid, a)).fetchone()[0]
    db.execute("UPDATE photo.video_upload_sessions SET expires_at=now()-interval '1 second' WHERE id=%s", (b,))
    assert db.execute('SELECT photo.expire_video_uploads(10)').fetchone()[0] == 1
    assert usage(db, tid)[0] == 0
    assert db.execute('SELECT state FROM photo.video_upload_sessions WHERE id=%s', (b,)).fetchone()[0] == 'expired'


def test_function_security_attributes(db):
    rows = db.execute("""SELECT proname,prosecdef,pg_get_userbyid(proowner),proconfig::text,
      has_function_privilege('public',oid,'EXECUTE') FROM pg_proc WHERE pronamespace='photo'::regnamespace AND proname ~ '^(begin|bind|record|complete|finish|abort|expire)_video_upload'""").fetchall()
    assert len(rows) == 7
    for name, secdef, owner, cfg, public in rows:
        assert secdef and owner == 'photo_fn_owner' and 'search_path=pg_catalog, photo' in cfg and not public, name
    assert db.execute("SELECT has_function_privilege('photo_api','photo.finish_video_upload_completion(uuid,uuid,uuid,text,text,text,text)','EXECUTE')").fetchone()[0] is False
    assert db.execute("SELECT has_function_privilege('photo_scheduler','photo.finish_video_upload_completion(uuid,uuid,uuid,text,text,text,text)','EXECUTE')").fetchone()[0]
