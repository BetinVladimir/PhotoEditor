"""02688 delivery, 02695 egress budget, 02696 tenant limits: constraint cases and access closure."""
from uuid import uuid4
import pytest
from psycopg import errors
from test_video_tables_schema import ctx, put, bad

H = 'a' * 64


def test_collection_content_kind(db):
    tid, user, pid = ctx(db)
    cid = db.execute("INSERT INTO photo.collections(tenant_id,project_id,name) VALUES(%s,%s,'c') RETURNING id", (tid, pid)).fetchone()[0]
    assert db.execute('SELECT content_kind FROM photo.collections WHERE id=%s', (cid,)).fetchone()[0] == 'photo'
    db.execute("UPDATE photo.collections SET content_kind='video' WHERE id=%s", (cid,))
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("UPDATE photo.collections SET content_kind='audio' WHERE id=%s", (cid,))


def test_egress_events_kind_and_default_partition(db):
    tid, user, pid = ctx(db)
    base = dict(tenant_id=tid, render_id=uuid4(), bytes=10)
    bad(db, errors.CheckViolation, 'video_egress_events', kind='stream', **base)
    bad(db, errors.CheckViolation, 'video_egress_events', kind='download', **{**base, 'bytes': -1})
    with db.transaction():
        db.execute('SET CONSTRAINTS ALL DEFERRED')
        put(db, 'video_egress_events', kind='download', **base)
        assert db.execute("SELECT count(*) FROM photo.video_egress_events_default WHERE tenant_id=%s", (tid,)).fetchone()[0] == 1
        db.execute('DELETE FROM photo.video_egress_events WHERE tenant_id=%s', (tid,))


def test_collection_item_ordinal_range(db):
    tid, user, pid = ctx(db)
    base = dict(tenant_id=tid, collection_id=uuid4(), render_id=uuid4(), title='t')
    bad(db, errors.CheckViolation, 'video_collection_items', ordinal=1000, **base)
    bad(db, errors.CheckViolation, 'video_collection_items', ordinal=0, **{**base, 'title': ''})


def test_budget_invariant(db):
    tid, user, pid = ctx(db)
    for table, extra in (('video_egress_period', dict(period_start='2026-11-01')), ('video_share_budget', dict(share_id=uuid4()))):
        bad(db, errors.CheckViolation, table, tenant_id=tid, consumed_bytes=6, leased_bytes=5, limit_bytes=10, **extra)
        put(db, table, tenant_id=tid, consumed_bytes=5, leased_bytes=5, limit_bytes=10, **extra)
    bad(db, errors.CheckViolation, 'video_egress_period', tenant_id=tid, period_start='2026-11-02', limit_bytes=1)
    bad(db, errors.CheckViolation, 'video_egress_counters', tenant_id=tid, share_id=uuid4(), render_id=uuid4(), kind='download', bucket_start='2026-11-02T10:00:00Z')
    bad(db, errors.CheckViolation, 'video_egress_counters', tenant_id=tid, share_id=uuid4(), render_id=uuid4(), kind='stream', bucket_start='2026-11-02T10:30:00Z')
    bad(db, errors.CheckViolation, 'video_egress_leases', tenant_id=tid, share_id=uuid4(), period_start='2026-11-01', granted_bytes=0, expires_at='2026-11-02T10:00:00Z')


def test_limits_usage_rate_checks(db):
    tid, user, pid = ctx(db)
    bad(db, errors.CheckViolation, 'video_tenant_limits', tenant_id=tid, max_outstanding_heavy=1001)
    bad(db, errors.CheckViolation, 'video_tenant_limits', tenant_id=tid, max_active_uploads=101)
    bad(db, errors.CheckViolation, 'video_tenant_limits', tenant_id=tid, daily_cost_seconds=-1)
    put(db, 'video_tenant_limits', tenant_id=tid, max_outstanding_heavy=0, daily_cost_unlimited=True)
    bad(db, errors.CheckViolation, 'video_tenant_usage_daily', tenant_id=tid, day='2026-11-02', cost_seconds=-1)
    bad(db, errors.CheckViolation, 'video_tenant_rate', tenant_id=tid, hour_start='2026-11-02T10:15:00Z')


@pytest.mark.parametrize('table', ['video_collection_items', 'video_egress_events', 'video_egress_events_default', 'video_egress_counters',
                                   'video_egress_period', 'video_share_budget', 'video_egress_leases', 'video_tenant_limits',
                                   'video_tenant_usage_daily', 'video_tenant_rate'])
def test_app_roles_have_no_access(db, table):
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_portal', 'photo_publisher', 'photo_maintenance', 'photo_gateway', 'photo_auth'):
        for priv in ('SELECT', 'INSERT', 'UPDATE', 'DELETE'):
            assert not db.execute('SELECT has_table_privilege(%s,%s,%s)', (role, f'photo.{table}', priv)).fetchone()[0], (role, table, priv)
    assert db.execute('SELECT relrowsecurity AND relforcerowsecurity FROM pg_class WHERE oid=%s::regclass', (f'photo.{table}',)).fetchone()[0]
