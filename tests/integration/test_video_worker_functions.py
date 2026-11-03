"""02703: worker-facing functions. Behaviour is covered by the Go scenario test
(backend/internal/video/worker, tag videoe2e); this checks privileges and guards."""
from uuid import uuid4
import pytest
from psycopg import errors
from test_video_job_functions import mk_job
from test_video_media_sequence_functions import media

SIG = {'register_video_artifact': 'uuid,uuid,uuid,text,text,text,text,bigint,text,integer,integer', 'get_video_job_inputs': 'uuid,uuid,uuid'}


def test_worker_functions_security(db):
    for fn, args in SIG.items():
        row = db.execute(f"SELECT prosecdef,pg_get_userbyid(proowner),has_function_privilege('public','photo.{fn}({args})','EXECUTE'),"
                         f"has_function_privilege('photo_worker','photo.{fn}({args})','EXECUTE'),has_function_privilege('photo_api','photo.{fn}({args})','EXECUTE') "
                         f"FROM pg_proc WHERE oid='photo.{fn}({args})'::regprocedure").fetchone()
        assert row == (True, 'photo_fn_owner', False, True, False), fn


def test_register_artifact_requires_running_job_with_token(db):
    tid, user, pid, uid, mid = media(db)
    jid = mk_job(db, tid, pid, user, 'ingest', mid)
    args = lambda tok, role='video_probe', key=None: (tid, jid, tok, role, 'bucket-x', key or f'tenants/{tid}/video/jobs/{jid}/p.json', 'a' * 64, 10, 'application/json', None, None)
    sql = 'SELECT photo.register_video_artifact(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)'
    with pytest.raises(errors.ObjectNotInPrerequisiteState), db.transaction():
        db.execute(sql, args(uuid4()))  # queued, not claimed
    c = db.execute("SELECT job_id,claim_token FROM photo.claim_video_job('video_heavy',60)").fetchone()
    assert c[0] == jid
    with pytest.raises(errors.ObjectNotInPrerequisiteState), db.transaction():
        db.execute(sql, args(uuid4()))  # wrong token
    with pytest.raises(errors.InvalidParameterValue), db.transaction():
        db.execute(sql, args(c[1], key='other/tenant/key'))  # outside the tenant video prefix
    with pytest.raises(errors.InvalidParameterValue), db.transaction():
        db.execute(sql, args(c[1], role='video_render'))  # render role on an ingest job
    aid = db.execute(sql, args(c[1])).fetchone()[0]
    assert db.execute(sql, args(c[1])).fetchone()[0] == aid  # idempotent
    with pytest.raises(errors.UniqueViolation), db.transaction():
        db.execute(sql.replace("%s,%s,%s,%s,%s,%s,%s,%s", "%s,%s,%s,%s,%s,%s,%s,%s", 1), (tid, jid, c[1], 'video_probe', 'bucket-x', f'tenants/{tid}/video/jobs/{jid}/p.json', 'b' * 64, 10, 'application/json', None, None))
    inp = db.execute('SELECT photo.get_video_job_inputs(%s,%s,%s)', (tid, jid, c[1])).fetchone()[0]
    assert inp['kind'] == 'ingest' and inp['media'][0]['id'] == str(mid)
    with pytest.raises(errors.ObjectNotInPrerequisiteState), db.transaction():
        db.execute('SELECT photo.get_video_job_inputs(%s,%s,%s)', (tid, jid, uuid4()))
