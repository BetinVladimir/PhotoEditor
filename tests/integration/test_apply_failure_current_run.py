"""Current failure versus cancellation SQL authority; scenarios not yet run."""
from concurrent.futures import ThreadPoolExecutor
from time import monotonic, sleep
from uuid import uuid4
import psycopg
import pytest
from test_apply_v4_plan import _build


def _reserved_build(db):
    tenant,run,token,_ = _build(db)
    actor = db.execute("SELECT created_by FROM photo.runs WHERE id=%s",(run,)).fetchone()[0]
    db.execute("UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+128 WHERE tenant_id=%s",(tenant,))
    reservation = db.execute("INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,state,expires_at) VALUES(%s,'user',%s,'run',%s,128,0,'reserved',clock_timestamp()+interval '1 hour') RETURNING id",(tenant,actor,run)).fetchone()[0]
    return tenant,run,token,reservation


def _state(db,tenant,run,reservation):
    return (
        db.execute("SELECT status,cancel_requested,revision,graph_sealed FROM photo.runs WHERE id=%s",(run,)).fetchone(),
        db.execute("SELECT phase,lease_token,lease_until,last_error FROM photo.apply_builds WHERE run_id=%s",(run,)).fetchone(),
        db.execute("SELECT bytes,state FROM photo.quota_reservations WHERE id=%s",(reservation,)).fetchone(),
        db.execute("SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s",(tenant,)).fetchone(),
    )


@pytest.mark.parametrize('status,cancel', [('cancelling',True),('cancelled',True),('completed',False),('completed_with_errors',False),('failed',False),('waiting_review',False)])
def test_failure_preserves_cancel_or_nonactive_run(db,status,cancel):
    tenant,run,token,reservation = _reserved_build(db)
    db.execute("UPDATE photo.runs SET status=%s,cancel_requested=%s WHERE id=%s",(status,cancel,run))
    before = _state(db,tenant,run,reservation)
    assert db.execute("SELECT photo.fail_apply_build(%s,%s,'QUALITY_FAILED')",(run,token)).fetchone()[0] is False
    assert _state(db,tenant,run,reservation) == before


def test_active_failure_releases_once_and_stale_token_cannot_mutate(db):
    tenant,run,token,reservation = _reserved_build(db)
    before = _state(db,tenant,run,reservation)
    assert db.execute("SELECT photo.fail_apply_build(%s,%s,'QUALITY_FAILED')",(run,uuid4())).fetchone()[0] is False
    assert _state(db,tenant,run,reservation) == before
    assert db.execute("SELECT photo.fail_apply_build(%s,%s,'QUALITY_FAILED')",(run,token)).fetchone()[0] is True
    after = _state(db,tenant,run,reservation)
    assert after[0] == ('failed',False,before[0][2]+1,True)
    assert after[1] == ('failed',None,None,'QUALITY_FAILED')
    assert after[2] == (0,'released')
    assert after[3] == (before[3][0]-128,)
    assert db.execute("SELECT photo.fail_apply_build(%s,%s,'QUALITY_FAILED')",(run,token)).fetchone()[0] is False
    assert _state(db,tenant,run,reservation) == after


def test_failure_waits_for_cancel_commit_then_preserves_cancelling(database):
    with psycopg.connect(**database,autocommit=True) as seed:
        tenant,run,token,reservation = _reserved_build(seed)
    name = 'apply-failure-race-' + uuid4().hex
    with psycopg.connect(**database) as cancel, psycopg.connect(**database,autocommit=True) as observer:
        cancel.execute("SELECT id FROM photo.runs WHERE id=%s FOR UPDATE",(run,))
        cancel.execute("UPDATE photo.runs SET status='cancelling',cancel_requested=true,revision=revision+1 WHERE id=%s",(run,))
        def fail():
            with psycopg.connect(**database,application_name=name,autocommit=True) as worker:
                worker.execute("SET statement_timeout='10s'")
                return worker.execute("SELECT photo.fail_apply_build(%s,%s,'QUALITY_FAILED')",(run,token)).fetchone()[0]
        with ThreadPoolExecutor(max_workers=1) as pool:
            pending = pool.submit(fail)
            try:
                deadline = monotonic()+5
                while not observer.execute("SELECT EXISTS(SELECT FROM pg_stat_activity WHERE application_name=%s AND wait_event_type='Lock')",(name,)).fetchone()[0]:
                    assert not pending.done(), 'failure must wait for current run authority'
                    assert monotonic()<deadline, 'failure did not reach run lock'
                    sleep(.01)
                cancel.commit()
                assert pending.result(timeout=5) is False
            finally:
                cancel.rollback()
        after = _state(observer,tenant,run,reservation)
        assert after[0][0:2] == ('cancelling',True)
        assert after[1][0:2] == ('resolve',token)
        assert after[2] == (128,'reserved')
        assert after[3][0] >= 128
