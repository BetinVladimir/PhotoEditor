"""02700 FN-D-06 finish_video_ingest and FN-D-07 save_video_sequence_revision."""
from uuid import uuid4
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from test_video_tables_schema import ctx
from test_video_upload_functions import PART, SHA, begin, give_quota, upload_all, usage

SIZE = PART + 7


def artifact(db, tid, role, size=1000, sha=None):
    aid = uuid4()
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role) VALUES(%s,%s,'bucket-x',%s,'v1',%s,%s,'application/octet-stream','ready',now(),%s)",
               (aid, tid, f'k/{aid}', sha or uuid4().hex * 2, size, role))
    return aid


def media(db, kind='video'):
    tid, user, pid = ctx(db); give_quota(db, tid)
    uid = begin(db, tid, pid, user, SIZE)
    tok = db.execute('SELECT photo.complete_video_upload(%s,%s,%s)', (tid, uid, Jsonb(upload_all(db, tid, uid, SIZE)))).fetchone()[0]
    mid = db.execute("SELECT photo.finish_video_upload_completion(%s,%s,%s,'bucket-x','v1',%s,'video/mp4')", (tid, uid, tok, SHA)).fetchone()[0]
    return tid, user, pid, uid, mid


def derivs(db, tid, sizes=(100, 200, 300, 400)):
    return [artifact(db, tid, r, s) for r, s in zip(('video_probe', 'video_proxy', 'video_filmstrip', 'video_waveform'), sizes)]


def finish(db, tid, mid, a, state='ready', code=None):
    return db.execute('SELECT photo.finish_video_ingest(%s,%s,%s,%s,%s,%s,%s,%s,%s)', (tid, mid, state, code, Jsonb({'duration_ticks': 1}), *a)).fetchone()[0]


def test_ingest_ready_moves_reservation_to_storage_and_is_idempotent(db):
    tid, user, pid, uid, mid = media(db)
    reserved_before = usage(db, tid)[0]
    assert finish(db, tid, mid, derivs(db, tid))
    assert usage(db, tid) == (0, SIZE + 1000) and reserved_before == SIZE + 1000
    assert db.execute('SELECT state FROM photo.video_media WHERE id=%s', (mid,)).fetchone()[0] == 'ready'
    assert db.execute('SELECT state FROM photo.video_upload_sessions WHERE id=%s', (uid,)).fetchone()[0] == 'verified'
    assert finish(db, tid, mid, derivs(db, tid))  # replay: state already ready -> no-op, no extra accounting
    assert usage(db, tid) == (0, SIZE + 1000)


def test_ingest_rejects_when_derivatives_exceed_reservation_or_missing(db):
    tid, user, pid, uid, mid = media(db)
    with pytest.raises(errors.ConfigurationLimitExceeded), db.transaction():
        finish(db, tid, mid, derivs(db, tid, (500, 500, 500, 500)))
    bad = derivs(db, tid); bad[1] = artifact(db, tid, 'video_probe')
    with pytest.raises(errors.ObjectNotInPrerequisiteState), db.transaction():
        finish(db, tid, mid, bad)
    assert db.execute('SELECT state FROM photo.video_media WHERE id=%s', (mid,)).fetchone()[0] == 'probing'


def test_ingest_rejected_releases_reservation(db):
    tid, user, pid, uid, mid = media(db)
    assert finish(db, tid, mid, [None] * 4, 'rejected', 'VIDEO_UNSUPPORTED_FORMAT')
    assert usage(db, tid) == (0, 0)
    assert db.execute('SELECT state,reject_code FROM photo.video_media WHERE id=%s', (mid,)).fetchone() == ('rejected', 'VIDEO_UNSUPPORTED_FORMAT')
    assert finish(db, tid, mid, [None] * 4, 'rejected', 'VIDEO_UNSUPPORTED_FORMAT')
    assert not finish(db, tid, mid, derivs(db, tid))


def seq(db, tid, pid, user):
    return db.execute('INSERT INTO photo.video_sequences(tenant_id,project_id,name,fps_num,fps_den,width,height,created_by) VALUES(%s,%s,%s,25,1,1920,1080,%s) RETURNING id', (tid, pid, 's', user)).fetchone()[0]


def save(db, tid, sid, expected, doc, sha, sources, label='manual', user=None):
    return db.execute('SELECT photo.save_video_sequence_revision(%s,%s,%s,%s,%s,%s,%s,%s,%s)', (tid, sid, expected, doc, sha, Jsonb({'clips': 1}), Jsonb(sources), label, user)).fetchone()[0]


def test_save_revision_cas_replay_and_sources(db):
    tid, user, pid, uid, mid = media(db)
    finish(db, tid, mid, derivs(db, tid))
    sid = seq(db, tid, pid, user)
    sha1, sha2 = 'c' * 64, 'd' * 64
    d1, d2 = artifact(db, tid, 'video_sequence_document', sha=sha1), artifact(db, tid, 'video_sequence_document', sha=sha2)
    src = [{'kind': 'video', 'id': str(mid), 'sha256': SHA}]
    assert save(db, tid, sid, 0, d1, sha1, src, user=user) == 1
    assert save(db, tid, sid, 0, d1, sha1, src, user=user) == 1  # replay of the same save
    assert db.execute('SELECT head_revision FROM photo.video_sequences WHERE id=%s', (sid,)).fetchone()[0] == 1
    assert db.execute('SELECT count(*) FROM photo.video_sequence_sources WHERE sequence_id=%s', (sid,)).fetchone()[0] == 1
    assert save(db, tid, sid, 1, d2, sha2, src, user=user) == 2
    with pytest.raises(errors.SerializationFailure) as e, db.transaction():
        save(db, tid, sid, 1, d2, 'e' * 64, src, user=user)
    assert 'head_revision=2' in e.value.diag.message_detail


def test_save_revision_rejects_bad_document_and_sources(db):
    tid, user, pid, uid, mid = media(db)
    sid = seq(db, tid, pid, user)
    sha = 'c' * 64
    d = artifact(db, tid, 'video_sequence_document', sha=sha)
    with pytest.raises(errors.InvalidParameterValue), db.transaction():
        save(db, tid, sid, 0, d, 'f' * 64, [], user=user)  # hash differs from artifact
    with pytest.raises(errors.InvalidParameterValue), db.transaction():
        save(db, tid, sid, 0, d, sha, [{'kind': 'video', 'id': str(mid), 'sha256': SHA}], user=user)  # media still probing
    with pytest.raises(errors.InvalidParameterValue), db.transaction():
        save(db, tid, sid, 0, d, sha, [{'kind': 'sound', 'id': str(uuid4()), 'sha256': SHA}], user=user)
    assert db.execute('SELECT head_revision FROM photo.video_sequences WHERE id=%s', (sid,)).fetchone()[0] == 0


def test_functions_are_closed_to_other_roles(db):
    for fn, ok, no in (('finish_video_ingest(uuid,uuid,text,text,jsonb,uuid,uuid,uuid,uuid)', 'photo_scheduler', 'photo_api'),
                       ('save_video_sequence_revision(uuid,uuid,bigint,uuid,text,jsonb,jsonb,text,uuid)', 'photo_api', 'photo_worker')):
        assert db.execute('SELECT has_function_privilege(%s,%s,%s)', (ok, f'photo.{fn}', 'EXECUTE')).fetchone()[0]
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)', (no, f'photo.{fn}', 'EXECUTE')).fetchone()[0]
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)', ('public', f'photo.{fn}', 'EXECUTE')).fetchone()[0]
