"""Rollback-only probes against a private, physically completed shown-media fixture.

PHOTO_TEST_V27_SHOWN_MEDIA_EVENT identifies a completed media job whose source
is a materialized shown-copy version. No synthetic result/qualification is used.
PHOTO_TEST_DATABASE_URL addresses its dedicated database migrated through 02261.
These tests do not provision or qualify that fixture, and must not use the
suite fresh-database fixture: it cannot contain the referenced physical result.
"""
import os
from uuid import UUID

import psycopg
import pytest


@pytest.fixture
def db():
    dsn = os.environ.get('PHOTO_TEST_DATABASE_URL')
    if not dsn or not os.environ.get('PHOTO_TEST_V27_SHOWN_MEDIA_EVENT'):
        pytest.skip('explicit isolated shown-media fixture database required')
    with psycopg.connect(dsn) as connection:
        with connection.transaction(force_rollback=True):
            yield connection


@pytest.fixture
def shown_media(db):
    raw = os.environ.get('PHOTO_TEST_V27_SHOWN_MEDIA_EVENT')
    if not raw:
        pytest.skip('private completed shown-media fixture required')
    event = UUID(raw)
    row = db.execute("""
        SELECT j.tenant_id,j.run_id,j.asset_version_id,v.asset_id,j.status
        FROM photo.media_retry_jobs j
        JOIN photo.asset_versions v ON v.tenant_id=j.tenant_id AND v.id=j.asset_version_id
        JOIN photo.runs source ON source.tenant_id=v.tenant_id AND source.id=v.created_by_run_id
        WHERE j.event_id=%s AND source.config_snapshot ? 'shown_semantic'
    """, (event,)).fetchone()
    assert row is not None and row[4] == 'completed', 'fixture must be physically completed'
    tenant, run, version, asset, _ = row
    assert db.execute("SELECT count(*) FROM photo.media_retry_jobs WHERE status='running' AND event_id<>%s", (event,)).fetchone()[0] == 0, 'fixture database must be isolated'
    nodes = db.execute("SELECT id FROM photo.media_retry_nodes WHERE tenant_id=%s AND run_id=%s ORDER BY variant", (tenant, run)).fetchall()
    assert len(nodes) in (2, 4)
    assert db.execute("SELECT count(*) FROM photo.tasks WHERE tenant_id=%s AND run_id=%s AND (state<>'succeeded' OR canonical_result_id IS NULL)", (tenant, run)).fetchone()[0] == 0
    return event, tenant, run, version, asset, nodes[-1][0]


def prepare(db, fixture):
    event, tenant, run, version, _, _ = fixture
    db.execute("UPDATE photo.media_retry_jobs SET status='running',error_code=NULL WHERE event_id=%s", (event,))
    db.execute("UPDATE photo.runs SET status='running',cancel_requested=false WHERE tenant_id=%s AND id=%s", (tenant, run))
    db.execute("DELETE FROM photo.derivatives WHERE tenant_id=%s AND asset_version_id=%s", (tenant, version))


def finish(db):
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.finalize_media_retry_job()').fetchone() == (True,)
    db.execute('RESET ROLE')


def test_shown_media_publishes_complete_family(db, shown_media):
    with db.transaction(force_rollback=True):
        prepare(db, shown_media)
        event, tenant, run, version, _, _ = shown_media
        expected = db.execute('SELECT count(*) FROM photo.media_retry_nodes WHERE tenant_id=%s AND run_id=%s', (tenant, run)).fetchone()[0]
        finish(db)
        assert db.execute('SELECT status FROM photo.media_retry_jobs WHERE event_id=%s', (event,)).fetchone() == ('completed',)
        assert db.execute("SELECT count(*) FROM photo.derivatives WHERE tenant_id=%s AND asset_version_id=%s AND status='ready'", (tenant, version)).fetchone()[0] == expected


@pytest.mark.parametrize('failure', ['deleted_asset', 'expired_last_output', 'expired_last_manifest', 'oversized_last_output', 'cancelled_run'])
def test_shown_media_rejects_entire_family(db, shown_media, failure):
    with db.transaction(force_rollback=True):
        prepare(db, shown_media)
        event, tenant, run, version, asset, last = shown_media
        if failure == 'deleted_asset':
            db.execute('UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE tenant_id=%s AND id=%s', (tenant, asset))
        elif failure == 'expired_last_output':
            db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND created_by_task_id=%s AND output_role='derivative'", (tenant, last))
        elif failure == 'expired_last_manifest':
            db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND created_by_task_id=%s AND output_role='derivative_manifest'", (tenant, last))
        elif failure == 'oversized_last_output':
            db.execute("UPDATE photo.artifacts SET width=4097 WHERE tenant_id=%s AND created_by_task_id=%s AND output_role='derivative'", (tenant, last))
        else:
            db.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s', (tenant, run))
        finish(db)
        assert db.execute('SELECT status FROM photo.media_retry_jobs WHERE event_id=%s', (event,)).fetchone() == ('failed',)
        assert db.execute('SELECT count(*) FROM photo.derivatives WHERE tenant_id=%s AND asset_version_id=%s', (tenant, version)).fetchone() == (0,)
