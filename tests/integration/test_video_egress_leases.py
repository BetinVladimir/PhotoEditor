"""02710: egress leases (FN-D-19/20) and booking of expired leases."""
from uuid import uuid4
import pytest
from psycopg import errors
from test_video_viewer_functions import delivery


def share(db, tid, cid, user, limits=None):
    from psycopg.types.json import Jsonb
    sid = uuid4()
    db.execute("SELECT photo.create_video_share(%s,%s,%s,%s,'t',false,clock_timestamp()+interval '1 day',%s,%s)", (tid, cid, sid, b'x' * 32, Jsonb(limits or {}), user))
    return sid


def budget(db, tid):
    return db.execute("SELECT consumed_bytes,leased_bytes,limit_bytes FROM photo.video_egress_period WHERE tenant_id=%s", (tid,)).fetchone()


def test_security(db):
    for sig, role, other in (('lease_video_egress(uuid,uuid,bigint)', 'photo_portal', 'photo_api'),
                             ('settle_video_egress(uuid,uuid,bigint,uuid,text)', 'photo_portal', 'photo_api'),
                             ('expire_video_egress_leases(integer)', 'photo_maintenance', 'photo_portal')):
        row = db.execute("SELECT prosecdef,pg_get_userbyid(proowner),has_function_privilege('public',oid,'EXECUTE'),has_function_privilege(%s,oid,'EXECUTE'),"
                         "has_function_privilege(%s,oid,'EXECUTE') FROM pg_proc WHERE oid=%s::regprocedure", (role, other, f'photo.{sig}')).fetchone()
        assert row == (True, 'photo_fn_owner', False, True, False), sig


def test_lease_settle_and_replay(db):
    tid, user, cid, rid = delivery(db)
    sh = share(db, tid, cid, user)
    out = db.execute('SELECT photo.lease_video_egress(%s,%s,%s)', (tid, sh, 1000)).fetchone()[0]
    assert out['granted'] == 1000 and budget(db, tid)[:2] == (0, 1000)
    assert db.execute('SELECT photo.settle_video_egress(%s,%s,%s,%s,%s)', (tid, out['lease_id'], 300, rid, 'stream')).fetchone()[0]
    assert budget(db, tid)[:2] == (300, 0)
    assert not db.execute('SELECT photo.settle_video_egress(%s,%s,%s,%s,%s)', (tid, out['lease_id'], 300, rid, 'stream')).fetchone()[0]  # replay
    assert budget(db, tid)[:2] == (300, 0)
    assert db.execute('SELECT bytes,requests FROM photo.video_egress_counters WHERE share_id=%s', (sh,)).fetchone() == (300, 1)


def test_share_limit_caps_the_grant_and_then_refuses(db):
    tid, user, cid, rid = delivery(db)
    sh = share(db, tid, cid, user, {'egress_bytes': 500})
    a = db.execute('SELECT photo.lease_video_egress(%s,%s,%s)', (tid, sh, 400)).fetchone()[0]
    b = db.execute('SELECT photo.lease_video_egress(%s,%s,%s)', (tid, sh, 400)).fetchone()[0]
    assert (a['granted'], b['granted']) == (400, 100)
    with pytest.raises(errors.ConfigurationLimitExceeded), db.transaction():
        db.execute('SELECT photo.lease_video_egress(%s,%s,%s)', (tid, sh, 1))
    db.execute('SELECT photo.settle_video_egress(%s,%s,%s,%s,%s)', (tid, a['lease_id'], 10_000, rid, 'stream'))  # over-report is clamped to the grant
    assert db.execute('SELECT consumed_bytes,leased_bytes FROM photo.video_share_budget WHERE share_id=%s', (sh,)).fetchone() == (400, 100)


def test_period_limit_and_unavailable_share(db):
    tid, user, cid, rid = delivery(db)
    sh = share(db, tid, cid, user)
    limit = db.execute("SELECT 10*coalesce((quotas->>'storage_bytes')::bigint,0) FROM photo.tenants WHERE id=%s", (tid,)).fetchone()[0]
    assert db.execute('SELECT photo.lease_video_egress(%s,%s,%s)', (tid, sh, limit * 2)).fetchone()[0]['granted'] == limit
    with pytest.raises(errors.ConfigurationLimitExceeded), db.transaction():
        db.execute('SELECT photo.lease_video_egress(%s,%s,%s)', (tid, sh, 1))
    db.execute('SELECT photo.revoke_video_share(%s,%s)', (tid, sh))
    with pytest.raises(errors.NoDataFound), db.transaction():
        db.execute('SELECT photo.lease_video_egress(%s,%s,%s)', (tid, sh, 1))
    with pytest.raises(errors.InvalidParameterValue), db.transaction():
        db.execute('SELECT photo.lease_video_egress(%s,%s,%s)', (tid, sh, 0))


def test_expired_leases_become_consumed(db):
    tid, user, cid, rid = delivery(db)
    sh = share(db, tid, cid, user)
    out = db.execute('SELECT photo.lease_video_egress(%s,%s,%s)', (tid, sh, 700)).fetchone()[0]
    assert db.execute('SELECT photo.expire_video_egress_leases(1000)').fetchone()[0] >= 0
    assert budget(db, tid)[:2] == (0, 700)  # still alive
    db.execute("UPDATE photo.video_egress_leases SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s", (out['lease_id'],))
    assert db.execute('SELECT photo.expire_video_egress_leases(1000)').fetchone()[0] >= 1
    assert budget(db, tid)[:2] == (700, 0)
