"""02684-02687 video media/sequences/jobs/renders: constraint cases from cards TK-102..105."""
import json
from uuid import uuid4
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from test_database import tenant

H = 'a' * 64
NOW = '2026-11-02T10:00:00Z'


def ctx(db):
    tid, user = tenant(db, str(uuid4()) + '@example.test')
    pid = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'V') RETURNING id", (tid,)).fetchone()[0]
    return tid, user, pid


def put(db, table, **cols):
    cols = {k: (Jsonb(v) if isinstance(v, (dict, list)) else v) for k, v in cols.items()}
    return db.execute(f'INSERT INTO photo.{table}({",".join(cols)}) VALUES({",".join(["%s"] * len(cols))})', tuple(cols.values()))


def bad(db, exc, table, **cols):
    with pytest.raises(exc), db.transaction():
        put(db, table, **cols)


def media(tid, pid, user, **o):
    v = dict(tenant_id=tid, project_id=pid, created_by=user, kind='video', name='m', state='probing', upload_id=uuid4(), source_artifact_id=uuid4(),
             source_sha256=H, source_bytes=10)
    v.update(o)
    return v


def ready_video(tid, pid, user, **o):
    return media(tid, pid, user, state='ready', probe_artifact_id=uuid4(), probe={'a': 1}, proxy_artifact_id=uuid4(),
                 filmstrip_artifact_id=uuid4(), waveform_artifact_id=uuid4(), **o)


def test_media_ready_rules(db):
    tid, user, pid = ctx(db)
    put(db, 'video_media', **ready_video(tid, pid, user))
    bad(db, errors.CheckViolation, 'video_media', **media(tid, pid, user, state='ready'))
    bad(db, errors.CheckViolation, 'video_media', **{**ready_video(tid, pid, user, kind='audio')})  # audio must not carry a proxy
    bad(db, errors.CheckViolation, 'video_media', **media(tid, pid, user, state='rejected'))
    bad(db, errors.CheckViolation, 'video_media', **media(tid, pid, user, probe={'x': 'y' * 20000}))
    bad(db, errors.CheckViolation, 'video_media', **media(tid, pid, user, state='deleted'))


def test_media_cross_tenant_artifact_fk_fails_when_checked(db):
    t1, u1, p1 = ctx(db)
    t2, u2, _ = ctx(db)
    art = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) VALUES(%s,'b',%s,%s,1,'video/mp4') RETURNING id", (t2, str(uuid4()), H)).fetchone()[0]
    put(db, 'video_media', **media(t1, p1, u1, source_artifact_id=art))
    with pytest.raises(errors.ForeignKeyViolation):
        db.execute('SET CONSTRAINTS ALL IMMEDIATE')


def seq(tid, pid, user, **o):
    v = dict(tenant_id=tid, project_id=pid, name='s', fps_num=25, fps_den=1, width=1920, height=1080, created_by=user)
    v.update(o)
    return v


def test_sequence_format_checks(db):
    tid, user, pid = ctx(db)
    put(db, 'video_sequences', **seq(tid, pid, user))
    put(db, 'video_sequences', **seq(tid, pid, user, fps_num=30000, fps_den=1001))
    bad(db, errors.CheckViolation, 'video_sequences', **seq(tid, pid, user, fps_num=7))
    bad(db, errors.CheckViolation, 'video_sequences', **seq(tid, pid, user, width=1921))
    bad(db, errors.CheckViolation, 'video_sequences', **seq(tid, pid, user, height=2306))


def revision(tid, sid, user, rev, parent, label='autosave'):
    return dict(tenant_id=tid, sequence_id=sid, revision=rev, parent_revision=parent, document_artifact_id=uuid4(), document_sha256=H,
                summary={}, label=label, created_by=user)


def test_revision_parent_rule_and_immutability(db):
    tid, user, pid = ctx(db)
    sid = db.execute('INSERT INTO photo.video_sequences(tenant_id,project_id,name,fps_num,fps_den,width,height,created_by) VALUES(%s,%s,%s,25,1,1920,1080,%s) RETURNING id',
                     (tid, pid, 's', user)).fetchone()[0]
    put(db, 'video_sequence_revisions', **revision(tid, sid, user, 1, None))
    put(db, 'video_sequence_revisions', **revision(tid, sid, user, 2, 1, 'manual'))
    bad(db, errors.CheckViolation, 'video_sequence_revisions', **revision(tid, sid, user, 4, 2))
    with pytest.raises(errors.InsufficientPrivilege), db.transaction():
        db.execute('UPDATE photo.video_sequence_revisions SET label=%s WHERE tenant_id=%s AND revision=1', ('manual', tid))
    with pytest.raises(errors.InsufficientPrivilege), db.transaction():
        db.execute('DELETE FROM photo.video_sequence_revisions WHERE tenant_id=%s AND revision=1', (tid,))
    with db.transaction():  # the compaction function raises the transaction-local flag and may drop only autosaves
        db.execute("SELECT set_config('photo.video_gc','on',true)")
        db.execute('DELETE FROM photo.video_sequence_revisions WHERE tenant_id=%s AND revision=1', (tid,))
        with pytest.raises(errors.InsufficientPrivilege), db.transaction():
            db.execute('DELETE FROM photo.video_sequence_revisions WHERE tenant_id=%s AND revision=2', (tid,))


def job(tid, pid, user, **o):
    v = dict(tenant_id=tid, project_id=pid, kind='ingest', run_id=uuid4(), params_artifact_id=uuid4(), params_sha256=H, state='queued',
             backend_requested='auto', pool='video_heavy', created_by=user)
    v.update(o)
    return v


def test_job_rules(db):
    tid, user, pid = ctx(db)
    put(db, 'video_jobs', **job(tid, pid, user))
    bad(db, errors.CheckViolation, 'video_jobs', **job(tid, pid, user, state='failed'))
    bad(db, errors.CheckViolation, 'video_jobs', **job(tid, pid, user, kind='render'))
    bad(db, errors.CheckViolation, 'video_jobs', **job(tid, pid, user, kind='detect_cuts'))
    bad(db, errors.CheckViolation, 'video_jobs', **job(tid, pid, user, progress=1.0001))
    bad(db, errors.CheckViolation, 'video_jobs', **job(tid, pid, user, kind='audio_analyze', media_id=uuid4(), pool='video_heavy'))
    run = uuid4()
    put(db, 'video_jobs', **job(tid, pid, user, run_id=run))
    bad(db, errors.UniqueViolation, 'video_jobs', **job(tid, pid, user, run_id=run))


def render(tid, pid, user, **o):
    v = dict(tenant_id=tid, project_id=pid, sequence_id=uuid4(), sequence_revision=1, document_sha256=H, preset={}, preset_sha256=H, job_id=uuid4(),
             width=1920, height=1080, fps_num=25, fps_den=1, video_codec='h264', audio_codec='aac', state='queued', created_by=user)
    v.update(o)
    return v


def test_render_rules(db):
    tid, user, pid = ctx(db)
    put(db, 'video_renders', **render(tid, pid, user))
    put(db, 'video_renders', **render(tid, pid, user, state='ready', output_artifact_id=uuid4(), qc_report_artifact_id=uuid4(), output_bytes=5,
                                      duration_ticks=705600000))
    bad(db, errors.CheckViolation, 'video_renders', **render(tid, pid, user, state='ready'))
    bad(db, errors.CheckViolation, 'video_renders', **render(tid, pid, user, width=1919))
    bad(db, errors.CheckViolation, 'video_renders', **render(tid, pid, user, video_codec='hevc'))
    bad(db, errors.CheckViolation, 'video_renders', **render(tid, pid, user, duration_ticks=7620480000001))
    job_id = uuid4()
    put(db, 'video_renders', **render(tid, pid, user, job_id=job_id))
    bad(db, errors.UniqueViolation, 'video_renders', **render(tid, pid, user, job_id=job_id))


@pytest.mark.parametrize('table', ['video_media', 'video_sequences', 'video_sequence_revisions', 'video_sequence_sources', 'video_jobs', 'video_job_outputs', 'video_renders'])
@pytest.mark.parametrize('role', ['photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_maintenance'])
def test_no_direct_access_for_application_roles(db, table, role):
    with pytest.raises(errors.InsufficientPrivilege), db.transaction():
        db.execute(f'SET LOCAL ROLE {role}')
        db.execute(f'SELECT count(*) FROM photo.{table}')
