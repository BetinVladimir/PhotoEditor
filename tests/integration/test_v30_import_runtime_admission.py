"""Import admission requires live scheduler and adopter for the source bucket."""

from uuid import UUID, uuid4


def _as_scheduler(db, function, *args):
    db.execute('SET LOCAL ROLE photo_scheduler')
    try:
        placeholders = ','.join(['%s'] * len(args))
        return db.execute(f'SELECT photo.{function}({placeholders})', args).fetchone()[0]
    finally:
        db.execute('RESET ROLE')


def _ready(db, bucket=None):
    if bucket is None:
        return db.execute('SELECT photo.v30_import_runtime_ready()').fetchone()[0]
    return db.execute('SELECT photo.v30_import_runtime_ready(%s)', (bucket,)).fetchone()[0]


def test_import_runtime_lease_roles_and_rotation(db):
    bound = (
        'photo.announce_v30_import_runtime_bound(uuid,text)',
        'photo.announce_v30_import_adopter_runtime_bound(uuid,text)',
    )
    withdraw = (
        'photo.withdraw_v30_import_runtime(uuid)',
        'photo.withdraw_v30_import_adopter_runtime(uuid)',
    )
    legacy = (
        'photo.announce_v30_import_runtime(uuid)',
        'photo.announce_v30_import_adopter_runtime(uuid)',
    )
    ready = ('photo.v30_import_runtime_ready()', 'photo.v30_import_runtime_ready(text)')
    for signature in bound + withdraw + legacy + ready:
        assert db.execute('SELECT to_regprocedure(%s) IS NOT NULL', (signature,)).fetchone()[0]
    for role in ('photo_api', 'photo_worker', 'photo_gateway', 'photo_portal', 'photo_maintenance'):
        for signature in bound + withdraw + legacy:
            assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
                                  (role, signature, 'EXECUTE')).fetchone()[0]
    for signature in bound + withdraw:
        assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                          ('photo_scheduler', signature, 'EXECUTE')).fetchone()[0]
    # Unbound legacy entry points remain installed for migration continuity,
    # but must never admit runtime via the scheduler role at current head.
    for signature in legacy:
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
                              ('photo_scheduler', signature, 'EXECUTE')).fetchone()[0]
    for signature in ready:
        assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                          ('photo_api', signature, 'EXECUTE')).fetchone()[0]

    bucket, other = 'import-runtime-a', 'import-runtime-b'
    scheduler, adopter = uuid4(), uuid4()
    assert not _ready(db)
    for function in ('announce_v30_import_runtime_bound', 'announce_v30_import_adopter_runtime_bound'):
        for instance, invalid_bucket in ((None, bucket), (UUID(int=0), bucket),
                                         (uuid4(), None), (uuid4(), ''),
                                         (uuid4(), 'ab'), (uuid4(), 'x' * 64)):
            assert _as_scheduler(db, function, instance, invalid_bucket) is False
    assert _as_scheduler(db, 'announce_v30_import_runtime_bound', scheduler, bucket)
    assert not _ready(db)  # Scheduler alone is insufficient.
    assert _as_scheduler(db, 'announce_v30_import_adopter_runtime_bound', adopter, other)
    assert not _ready(db)  # Both services exist but buckets do not match.
    assert not _ready(db, bucket)
    assert not _ready(db, other)
    assert _as_scheduler(db, 'announce_v30_import_adopter_runtime_bound', adopter, bucket)
    assert _ready(db) and _ready(db, bucket)
    assert not _ready(db, other)
    for invalid_bucket in ('', 'ab', 'x' * 64):
        assert not _ready(db, invalid_bucket)

    # Either service expiring or withdrawing closes admission immediately.
    for table, announce, withdraw_function, instance in (
        ('v30_import_runtime_leases', 'announce_v30_import_runtime_bound',
         'withdraw_v30_import_runtime', scheduler),
        ('v30_import_adopter_runtime_leases', 'announce_v30_import_adopter_runtime_bound',
         'withdraw_v30_import_adopter_runtime', adopter),
    ):
        db.execute(f"UPDATE photo.{table} SET expires_at=clock_timestamp()-interval '1 second' "
                   "WHERE instance_id=%s", (instance,))
        assert not _ready(db) and not _ready(db, bucket)
        assert _as_scheduler(db, announce, instance, bucket)
        assert _ready(db, bucket)
        _as_scheduler(db, withdraw_function, instance)
        assert not _ready(db) and not _ready(db, bucket)
        # Rotation reuses an expired slot instead of accumulating stale rows.
        db.execute(f"UPDATE photo.{table} SET expires_at=clock_timestamp()-interval '2 days' "
                   "WHERE instance_id=%s", (instance,))
        replacement = uuid4()
        assert _as_scheduler(db, announce, replacement, bucket)
        assert db.execute(f'SELECT count(*) FROM photo.{table}').fetchone()[0] == 1
        assert db.execute(f'SELECT instance_id,bucket FROM photo.{table}').fetchone() == (replacement, bucket)
        assert _ready(db, bucket)
        # Restore this service so the next iteration isolates the other lease.
    rows = db.execute('SELECT instance_id FROM photo.v30_import_runtime_leases').fetchall()
    _as_scheduler(db, 'withdraw_v30_import_runtime', rows[0][0])
    assert not _ready(db)


def test_import_parse_new_run_has_runtime_gate(db):
    definition = db.execute("SELECT pg_get_functiondef('photo.begin_v30_interchange_parse_run"
                            "(uuid,uuid,uuid,text,text,text,text,text)'::regprocedure)").fetchone()[0]
    replay = definition.index('IF FOUND THEN')
    readiness = definition.index('photo.v30_import_runtime_ready((SELECT a.bucket')
    assert replay < readiness
    gate = definition[readiness:definition.index('IS DISTINCT FROM true', readiness)]
    assert 'a.tenant_id=p_tenant' in gate
    assert "a.id=(bundle->>'artifact_id')::uuid" in gate
    assert "a.sha256=bundle->>'sha256'" in gate
    assert "a.state='ready'" in gate


def test_import_readiness_real_roles_on_clean_migrations(database, monkeypatch):
    """Execute the existing worker real-role test with an owned migrated DB."""
    from pathlib import Path
    import runpy
    from psycopg.conninfo import make_conninfo

    root = Path(__file__).resolve().parents[2]
    checks = runpy.run_path(str(root / 'workers/tests/test_interchange_import_adopter_runtime_sql.py'))
    monkeypatch.setenv('PHOTO_TEST_DATABASE_URL', make_conninfo(**database))
    checks['test_import_readiness_requires_scheduler_and_adopter_leases']()
