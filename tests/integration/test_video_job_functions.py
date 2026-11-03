"""02702 video job queue: cost/retry/admission vectors (scale.json), create/claim/progress/finish/cancel, leases, quota."""
import json
from pathlib import Path
from uuid import uuid4
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from test_video_media_sequence_functions import SIZE, artifact, derivs, finish, media, save, seq
from test_video_upload_functions import SHA, give_quota, usage
from test_video_tables_schema import ctx

VEC = json.loads((Path(__file__).resolve().parents[2] / 'docs/video/vectors/scale.json').read_text())
GROUP = {'ingest': 'ingest', 'render': 'heavy', 'detect_cuts': 'heavy', 'color_analyze': 'heavy', 'audio_analyze': 'light', 'preview_frames': 'preview'}
GROUP_KIND = {'ingest': 'ingest', 'heavy': 'detect_cuts', 'light': 'audio_analyze', 'preview': 'preview_frames'}
H = 'a' * 64


def raw_job(db, tid, pid, user, kind, state='running', cost=0, pool=None):
    pool = pool or ('video_light' if kind in ('audio_analyze', 'preview_frames') else 'video_heavy')
    extra = {'media_id': uuid4()} if kind in ('detect_cuts', 'color_analyze', 'audio_analyze') else {'sequence_id': uuid4(), 'sequence_revision': 1} if kind in ('preview_frames', 'render') else {}
    cols = dict(tenant_id=tid, project_id=pid, kind=kind, params_artifact_id=uuid4(), params_sha256=H, state=state, backend_requested='auto', cost_seconds=cost,
                pool=pool, created_by=user, **extra)
    db.execute(f'INSERT INTO photo.video_jobs({",".join(cols)}) VALUES({",".join(["%s"] * len(cols))})', tuple(cols.values()))


@pytest.mark.parametrize('case', VEC['job_cost'], ids=lambda c: f"{c['kind']}-{c['duration_ticks']}")
def test_cost_vectors(db, case):
    if 'expect' in case:
        assert db.execute('SELECT photo.video_job_cost_seconds(%s,%s)', (case['kind'], case['duration_ticks'])).fetchone()[0] == case['expect']
    else:
        with pytest.raises(errors.InvalidParameterValue) as e, db.transaction():
            db.execute('SELECT photo.video_job_cost_seconds(%s,%s)', (case['kind'], case['duration_ticks']))
        assert case['error'] in str(e.value)


@pytest.mark.parametrize('case', VEC['retry_after'], ids=lambda c: f"{c['ahead']}-{c['workers']}")
def test_retry_after_vectors(db, case):
    if 'expect' in case:
        assert db.execute('SELECT photo.video_retry_after_seconds(%s,%s)', (case['ahead'], case['workers'])).fetchone()[0] == case['expect']
    else:
        with pytest.raises(errors.InvalidParameterValue), db.transaction():
            db.execute('SELECT photo.video_retry_after_seconds(%s,%s)', (case['ahead'], case['workers']))


@pytest.mark.parametrize('i', range(len(VEC['admit_job'])))
def test_admit_vectors(db, i):
    case = VEC['admit_job'][i]
    tid, user, pid = ctx(db)
    kind, st = case['kind'], case['state']
    if kind not in GROUP:
        with pytest.raises(errors.InvalidParameterValue), db.transaction():
            db.execute('SELECT photo.admit_video_job(%s,%s,%s,%s)', (tid, kind, case['cost'], st['activeWorkers']))
        return
    for g, n in st['outstanding'].items():
        for _ in range(n):
            raw_job(db, tid, pid, user, GROUP_KIND[g])
    if st['poolAheadCost']:
        ahead_kind = 'detect_cuts' if GROUP[kind] == 'ingest' else 'ingest'
        raw_job(db, tid, pid, user, ahead_kind, 'queued', st['poolAheadCost'], 'video_heavy')
    if st['dailyUsedCost']:
        db.execute("INSERT INTO photo.video_tenant_usage_daily(tenant_id,day,cost_seconds) VALUES(%s,(now() AT TIME ZONE 'UTC')::date,%s)", (tid, st['dailyUsedCost']))
    ov = case.get('limits_override', {})
    if 'dailyCost' in ov and ov['dailyCost'] is None:
        db.execute('INSERT INTO photo.video_tenant_limits(tenant_id,daily_cost_unlimited) VALUES(%s,true)', (tid,))
    shed = ov.get('shedCost', 21600)
    exp = case['expect']
    if exp['ok']:
        db.execute('SELECT photo.admit_video_job(%s,%s,%s,%s,%s)', (tid, kind, case['cost'], st['activeWorkers'], shed))
        return
    with pytest.raises((errors.ConfigurationLimitExceeded, errors.TooManyConnections)) as e, db.transaction():
        db.execute('SELECT photo.admit_video_job(%s,%s,%s,%s,%s)', (tid, kind, case['cost'], st['activeWorkers'], shed))
    assert exp['code'] in str(e.value)
    detail = e.value.diag.message_detail
    assert detail == (exp['limit_code'] if 'limit_code' in exp else str(exp['retry_after_s']))


PARAMS = 'a' * 64


def mk_job(db, tid, pid, user, kind='ingest', media_id=None, reserve=0, ticks=60 * 705600000, req=None, **kw):
    return db.execute('SELECT photo.create_video_job(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                      (tid, pid, user, kind, media_id, kw.get('sequence'), kw.get('revision'), uuid4(), PARAMS, 'auto', ticks, req or uuid4().hex * 2, reserve, 1)).fetchone()[0]


def ready_media(db):
    tid, user, pid, uid, mid = media(db)
    finish(db, tid, mid, derivs(db, tid))
    return tid, user, pid, mid


def test_job_lifecycle_with_quota_and_outputs(db):
    tid, user, pid, mid = ready_media(db)
    before = usage(db, tid)
    jid = mk_job(db, tid, pid, user, 'detect_cuts', mid, reserve=10_000)
    assert usage(db, tid)[0] == before[0] + 10_000
    assert db.execute('SELECT state,cost_seconds,pool FROM photo.video_jobs WHERE id=%s', (jid,)).fetchone() == ('queued', 60 + (60 * 7 + 19) // 20, 'video_heavy')
    assert db.execute("SELECT cost_seconds,jobs_admitted FROM photo.video_tenant_usage_daily WHERE tenant_id=%s", (tid,)).fetchone() == (60 + 21, 1)
    c = db.execute("SELECT job_id,claim_token,attempt FROM photo.claim_video_job('video_heavy',60)").fetchone()
    assert c[0] == jid and c[2] == 1
    assert db.execute('SELECT photo.report_video_job_progress(%s,%s,%s,%s,%s::numeric,%s::integer,%s::integer)', (tid, jid, c[1], 'scan', 0.5, 10, 60)).fetchone()[0]
    assert not db.execute('SELECT photo.report_video_job_progress(%s,%s,%s,%s,%s::numeric,%s::integer,%s::integer)', (tid, jid, uuid4(), 'scan', 0.6, 10, 60)).fetchone()[0]
    out = artifact(db, tid, 'video_cut_report', 2000)
    outs = Jsonb([{'role': 'video_cut_report', 'ordinal': 0, 'artifact_id': str(out)}])
    assert not db.execute('SELECT photo.finish_video_job(%s,%s,%s,%s,%s,%s,%s,%s)', (tid, jid, uuid4(), 'succeeded', None, None, 'cpu', outs)).fetchone()[0]  # wrong fence
    assert db.execute('SELECT photo.finish_video_job(%s,%s,%s,%s,%s,%s,%s,%s)', (tid, jid, c[1], 'succeeded', None, None, 'cpu', outs)).fetchone()[0]
    assert usage(db, tid) == (before[0], before[1] + 2000)
    assert db.execute('SELECT state,progress FROM photo.video_jobs WHERE id=%s', (jid,)).fetchone() == ('succeeded', 1)
    assert db.execute('SELECT count(*) FROM photo.video_job_outputs WHERE job_id=%s', (jid,)).fetchone()[0] == 1
    assert db.execute('SELECT photo.finish_video_job(%s,%s,%s,%s,%s,%s,%s,%s)', (tid, jid, c[1], 'succeeded', None, None, 'cpu', outs)).fetchone()[0]  # replay no-op
    assert usage(db, tid) == (before[0], before[1] + 2000)


def test_output_over_reservation_fails_job_and_releases(db):
    tid, user, pid, mid = ready_media(db)
    before = usage(db, tid)
    jid = mk_job(db, tid, pid, user, 'detect_cuts', mid, reserve=1000)
    c = db.execute("SELECT job_id,claim_token FROM photo.claim_video_job('video_heavy',60)").fetchone()
    out = artifact(db, tid, 'video_cut_report', 5000)
    db.execute('SELECT photo.finish_video_job(%s,%s,%s,%s,%s,%s,%s,%s)', (tid, jid, c[1], 'succeeded', None, None, 'cpu', Jsonb([{'role': 'video_cut_report', 'ordinal': 0, 'artifact_id': str(out)}])))
    assert db.execute('SELECT state,error_code FROM photo.video_jobs WHERE id=%s', (jid,)).fetchone() == ('failed', 'OUTPUT_EXCEEDS_RESERVATION')
    assert usage(db, tid) == before
    assert db.execute('SELECT count(*) FROM photo.video_job_outputs WHERE job_id=%s', (jid,)).fetchone()[0] == 0


def test_create_is_idempotent_and_detects_reuse(db):
    tid, user, pid, mid = ready_media(db)
    a = mk_job(db, tid, pid, user, 'color_analyze', mid, req='1' * 64)
    assert mk_job(db, tid, pid, user, 'color_analyze', mid, req='1' * 64) == a
    assert db.execute('SELECT jobs_admitted FROM photo.video_tenant_usage_daily WHERE tenant_id=%s', (tid,)).fetchone()[0] == 1
    with pytest.raises(errors.UniqueViolation), db.transaction():
        mk_job(db, tid, pid, user, 'detect_cuts', mid, req='1' * 64)


def test_create_rejections_leave_no_side_effects(db):
    tid, user, pid, mid = ready_media(db)
    with pytest.raises(errors.ConfigurationLimitExceeded), db.transaction():
        mk_job(db, tid, pid, user, 'detect_cuts', mid, reserve=10 ** 13)
    for _ in range(4):
        mk_job(db, tid, pid, user, 'detect_cuts', mid)
    with pytest.raises(errors.ConfigurationLimitExceeded) as e, db.transaction():
        mk_job(db, tid, pid, user, 'color_analyze', mid)
    assert e.value.diag.message_detail == 'OUTSTANDING_HEAVY'
    assert db.execute('SELECT jobs_admitted FROM photo.video_tenant_usage_daily WHERE tenant_id=%s', (tid,)).fetchone()[0] == 4
    with pytest.raises(errors.NoDataFound), db.transaction():
        mk_job(db, tid, pid, user, 'detect_cuts', uuid4())  # unknown media
    

def test_cancel_queued_and_running(db):
    tid, user, pid, mid = ready_media(db)
    before = usage(db, tid)
    a = mk_job(db, tid, pid, user, 'detect_cuts', mid, reserve=500)
    assert db.execute('SELECT photo.cancel_video_job(%s,%s)', (tid, a)).fetchone()[0] == 'cancelled'
    assert usage(db, tid) == before
    b = mk_job(db, tid, pid, user, 'color_analyze', mid, reserve=500)
    c = db.execute("SELECT job_id,claim_token FROM photo.claim_video_job('video_heavy',60)").fetchone()
    assert c[0] == b
    assert db.execute('SELECT photo.cancel_video_job(%s,%s)', (tid, b)).fetchone()[0] == 'cancelling'
    assert not db.execute('SELECT photo.report_video_job_progress(%s,%s,%s,%s,%s::numeric,%s::integer,%s::integer)', (tid, b, c[1], 's', 0.2, None, 60)).fetchone()[0]
    assert db.execute('SELECT photo.finish_video_job(%s,%s,%s,%s,%s,%s,%s,%s)', (tid, b, c[1], 'cancelled', None, None, None, Jsonb([]))).fetchone()[0]
    assert usage(db, tid) == before
    with pytest.raises(errors.ObjectNotInPrerequisiteState), db.transaction():
        db.execute('SELECT photo.cancel_video_job(%s,%s)', (tid, b if False else mk_and_fail(db, tid, pid, user, mid)))


def mk_and_fail(db, tid, pid, user, mid):
    j = mk_job(db, tid, pid, user, 'audio_analyze', mid)
    c = db.execute("SELECT job_id,claim_token FROM photo.claim_video_job('video_light',60)").fetchone()
    db.execute('SELECT photo.finish_video_job(%s,%s,%s,%s,%s,%s,%s,%s)', (tid, j, c[1], 'failed', 'VIDEO_DECODE', 'x', 'cpu', Jsonb([])))
    return j


def test_lost_worker_requeues_then_abandons(db):
    tid, user, pid, mid = ready_media(db)
    before = usage(db, tid)
    jid = mk_job(db, tid, pid, user, 'detect_cuts', mid, reserve=700)
    for attempt in (1, 2, 3):
        c = db.execute("SELECT job_id,attempt FROM photo.claim_video_job('video_heavy',60)").fetchone()
        assert c == (jid, attempt)
        db.execute("UPDATE photo.video_jobs SET lease_until=now()-interval '1 second' WHERE id=%s", (jid,))
    assert db.execute("SELECT job_id FROM photo.claim_video_job('video_heavy',60)").fetchone() is None
    assert db.execute('SELECT state,error_code FROM photo.video_jobs WHERE id=%s', (jid,)).fetchone() == ('failed', 'VIDEO_JOB_ABANDONED')
    assert usage(db, tid) == before


def test_render_job_creates_render_row_and_follows_job(db):
    tid, user, pid, mid = ready_media(db)
    sid = seq(db, tid, pid, user)
    sha = 'c' * 64
    rev = save(db, tid, sid, 0, artifact(db, tid, 'video_sequence_document', 500, sha), sha, [{'kind': 'video', 'id': str(mid), 'sha256': SHA}], user=user)
    args = (tid, pid, user, sid, rev, Jsonb({'name': 'web'}), 'e' * 64, uuid4(), PARAMS, 'auto', 10 * 705600000, '2' * 64, 50_000, 1)
    rid = db.execute('SELECT photo.create_video_render(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)', args).fetchone()[0]
    assert db.execute('SELECT photo.create_video_render(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)', args).fetchone()[0] == rid
    assert db.execute('SELECT state,document_sha256,width,height FROM photo.video_renders WHERE id=%s', (rid,)).fetchone() == ('queued', sha, 1920, 1080)
    c = db.execute("SELECT job_id,claim_token,kind FROM photo.claim_video_job('video_heavy',60)").fetchone()
    assert c[2] == 'render'
    assert db.execute('SELECT state FROM photo.video_renders WHERE id=%s', (rid,)).fetchone()[0] == 'rendering'
    out = artifact(db, tid, 'video_render', 4000)
    db.execute('SELECT photo.finish_video_job(%s,%s,%s,%s,%s,%s,%s,%s)', (tid, c[0], c[1], 'succeeded', None, None, 'cuda', Jsonb([{'role': 'video_render', 'ordinal': 0, 'artifact_id': str(out)}])))
    assert db.execute('SELECT state FROM photo.video_renders WHERE id=%s', (rid,)).fetchone()[0] == 'verifying'
    qc = artifact(db, tid, 'video_qc_report', 100)
    assert db.execute('SELECT photo.finish_video_render(%s,%s,%s,%s,%s,%s,%s,%s,%s)', (tid, rid, 'ready', out, qc, 7056000000, 'h264_nvenc', 'cuda', 'f' * 64)).fetchone()[0]


def test_job_function_security(db):
    for fn, ok, no in (('claim_video_job(text,integer)', 'photo_worker', 'photo_api'), ('create_video_job(uuid,uuid,uuid,text,uuid,uuid,bigint,uuid,text,text,bigint,text,bigint,integer)', 'photo_api', 'photo_worker'),
                       ('settle_video_job(uuid,uuid,text,text,text,text,jsonb)', None, 'photo_api'), ('admit_video_job(uuid,text,integer,integer,bigint)', None, 'photo_api')):
        assert not db.execute("SELECT has_function_privilege('public',%s,'EXECUTE')", (f'photo.{fn}',)).fetchone()[0]
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)', (no, f'photo.{fn}', 'EXECUTE')).fetchone()[0]
        if ok:
            assert db.execute('SELECT has_function_privilege(%s,%s,%s)', (ok, f'photo.{fn}', 'EXECUTE')).fetchone()[0]
