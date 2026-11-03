"""Scenario at DB level: upload -> probe -> montage -> render -> delivery -> download.
The job/render rows are inserted directly where FN-D-08/FN-D-11 (queue) are not implemented yet."""
from uuid import uuid4
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from test_video_media_sequence_functions import SIZE, artifact, derivs, finish, media, save, seq
from test_video_upload_functions import SHA, usage

FP = 'a1' * 32


def render_row(db, tid, pid, user, sid, rev, sha):
    run = None
    job = db.execute("""INSERT INTO photo.video_jobs(tenant_id,project_id,kind,run_id,sequence_id,sequence_revision,params_artifact_id,params_sha256,state,
        backend_requested,pool,created_by) VALUES(%s,%s,'render',%s,%s,%s,%s,%s,'running','auto','video_heavy',%s) RETURNING id""",
                      (tid, pid, uuid4(), sid, rev, uuid4(), SHA, user)).fetchone()[0]
    return db.execute("""INSERT INTO photo.video_renders(tenant_id,project_id,sequence_id,sequence_revision,document_sha256,preset,preset_sha256,job_id,width,height,
        fps_num,fps_den,video_codec,audio_codec,state,created_by) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,1920,1080,25,1,'h264','aac','verifying',%s) RETURNING id""",
                      (tid, pid, sid, rev, sha, Jsonb({'name': 'web'}), SHA, job, user)).fetchone()[0]


def test_upload_to_download_scenario(db):
    tid, user, pid, uid, mid = media(db)
    assert finish(db, tid, mid, derivs(db, tid))
    assert usage(db, tid) == (0, SIZE + 1000)

    sid = seq(db, tid, pid, user)
    sha = 'c' * 64
    doc = artifact(db, tid, 'video_sequence_document', 500, sha)
    rev = save(db, tid, sid, 0, doc, sha, [{'kind': 'video', 'id': str(mid), 'sha256': SHA}], user=user)
    assert rev == 1

    rid = render_row(db, tid, pid, user, sid, rev, sha)
    out, qc = artifact(db, tid, 'video_render', 4000), artifact(db, tid, 'video_qc_report', 100)
    with pytest.raises(errors.ObjectNotInPrerequisiteState), db.transaction():
        db.execute('SELECT photo.finish_video_render(%s,%s,%s,%s,%s,%s,%s,%s,%s)', (tid, rid, 'ready', artifact(db, tid, 'video_proxy'), qc, 10, 'h264_nvenc', 'cuda', FP))
    assert db.execute('SELECT photo.finish_video_render(%s,%s,%s,%s,%s,%s,%s,%s,%s)', (tid, rid, 'ready', out, qc, 10 * 705600000, 'h264_nvenc', 'cuda', FP)).fetchone()[0]
    assert db.execute('SELECT state,output_bytes FROM photo.video_renders WHERE id=%s', (rid,)).fetchone() == ('ready', 4000)
    assert db.execute('SELECT photo.finish_video_render(%s,%s,%s,%s,%s,%s,%s,%s,%s)', (tid, rid, 'ready', out, qc, 1, 'x', 'cpu', FP)).fetchone()[0]  # replay

    cid = db.execute('SELECT photo.create_video_delivery(%s,%s,%s)', (tid, pid, 'Wedding film')).fetchone()[0]
    with pytest.raises(errors.ObjectNotInPrerequisiteState), db.transaction():
        db.execute('SELECT photo.publish_video_delivery(%s,%s,true)', (tid, cid))  # empty
    assert db.execute('SELECT photo.add_video_render_to_delivery(%s,%s,%s,%s)', (tid, cid, rid, 'Full film')).fetchone()[0] == 0
    assert db.execute('SELECT photo.add_video_render_to_delivery(%s,%s,%s,%s)', (tid, cid, rid, 'Full film')).fetchone()[0] == 0
    secret = bytes(32)
    with pytest.raises(errors.ObjectNotInPrerequisiteState), db.transaction():
        db.execute('SELECT photo.create_video_share(%s,%s,%s,%s,%s,true,now()+interval \'1 day\',%s,%s)', (tid, cid, uuid4(), secret, 'x', Jsonb({}), user))
    assert db.execute('SELECT photo.publish_video_delivery(%s,%s,true)', (tid, cid)).fetchone()[0]
    share = uuid4()
    assert db.execute('SELECT photo.create_video_share(%s,%s,%s,%s,%s,true,now()+interval \'1 day\',%s,%s)', (tid, cid, share, secret, 'client', Jsonb({}), user)).fetchone()[0] == share

    assert db.execute('SELECT photo.record_video_egress(%s,%s,%s,%s)', (tid, share, rid, 4000)).fetchone()[0]
    assert not db.execute('SELECT photo.record_video_egress(%s,%s,%s,%s)', (tid, share, uuid4(), 1)).fetchone()[0]  # render not in this delivery
    assert db.execute('SELECT photo.revoke_video_share(%s,%s)', (tid, share)).fetchone()[0]
    assert not db.execute('SELECT photo.record_video_egress(%s,%s,%s,%s)', (tid, share, rid, 4000)).fetchone()[0]  # revoked
    assert db.execute('SELECT count(*), sum(bytes) FROM photo.video_egress_events WHERE tenant_id=%s', (tid,)).fetchone() == (1, 4000)
    assert db.execute('SELECT photo.publish_video_delivery(%s,%s,false)', (tid, cid)).fetchone()[0]
    assert db.execute('SELECT state FROM photo.collections WHERE id=%s', (cid,)).fetchone()[0] == 'draft'


def test_download_not_allowed_without_download_permission(db):
    tid, user, pid, uid, mid = media(db)
    finish(db, tid, mid, derivs(db, tid))
    sid = seq(db, tid, pid, user)
    rid = render_row(db, tid, pid, user, sid, 1, 'c' * 64)
    out, qc = artifact(db, tid, 'video_render', 4000), artifact(db, tid, 'video_qc_report', 100)
    db.execute('SELECT photo.finish_video_render(%s,%s,%s,%s,%s,%s,%s,%s,%s)', (tid, rid, 'ready', out, qc, 1000, 'h264_nvenc', 'cuda', FP))
    cid = db.execute('SELECT photo.create_video_delivery(%s,%s,%s)', (tid, pid, 'View only')).fetchone()[0]
    db.execute('SELECT photo.add_video_render_to_delivery(%s,%s,%s,%s)', (tid, cid, rid, 't'))
    db.execute('SELECT photo.publish_video_delivery(%s,%s,true)', (tid, cid))
    share = uuid4()
    db.execute('SELECT photo.create_video_share(%s,%s,%s,%s,%s,false,now()+interval \'1 day\',%s,%s)', (tid, cid, share, bytes(32), 'v', Jsonb({}), user))
    assert not db.execute('SELECT photo.record_video_egress(%s,%s,%s,%s)', (tid, share, rid, 1)).fetchone()[0]


def test_delivery_functions_security(db):
    rows = db.execute("""SELECT proname,prosecdef,pg_get_userbyid(proowner),has_function_privilege('public',oid,'EXECUTE')
      FROM pg_proc WHERE pronamespace='photo'::regnamespace AND proname IN('finish_video_render','create_video_delivery','add_video_render_to_delivery',
      'publish_video_delivery','create_video_share','revoke_video_share','record_video_egress','finish_video_ingest','save_video_sequence_revision')""").fetchall()
    assert len(rows) == 9 and all(r[1] and r[2] == 'photo_fn_owner' and not r[3] for r in rows)
    assert db.execute("SELECT has_function_privilege('photo_portal','photo.record_video_egress(uuid,uuid,uuid,bigint)','EXECUTE')").fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_api','photo.record_video_egress(uuid,uuid,uuid,bigint)','EXECUTE')").fetchone()[0]
