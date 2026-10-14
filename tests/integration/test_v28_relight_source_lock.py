from uuid import uuid4
from pathlib import Path
import psycopg
import pytest
from psycopg.types.json import Jsonb
from test_v27_semantic_final_producer_gate import fixture


@pytest.mark.migration_head(994)
def test_relight_source_lock_upgrade_from_previous_head(db):
    sig='photo.lock_v28_relight_source(uuid,uuid,uuid,uuid,uuid,jsonb)'
    assert db.execute('SELECT to_regprocedure(%s)',(sig,)).fetchone()[0] is None
    tenant,*_,scope=fixture(db,snapshot_authority=True)
    migration=Path(__file__).resolve().parents[2]/'backend/db/migrations/00995_v28_relight_source_lock.sql'
    db.execute(migration.read_text().split('-- +goose Down')[0])
    base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='c'*64)
    db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.lock_v28_relight_source(%s,%s,%s,%s,%s,%s)',(tenant,scope['project'],scope['snapshot'],scope['actor'],scope['session'],Jsonb(base))).fetchone()[0] is True
    db.execute('RESET ROLE')


def test_relight_prepublication_source_gate_and_revocation(db):
    tenant,*_,scope=fixture(db,snapshot_authority=True)
    base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='c'*64)
    def lock(value=base,tid=tenant):
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tid),))
            db.execute('SET LOCAL ROLE photo_scheduler')
            result=db.execute('SELECT photo.lock_v28_relight_source(%s,%s,%s,%s,%s,%s)',(tid,scope['project'],scope['snapshot'],scope['actor'],scope['session'],Jsonb(value))).fetchone()[0]
            db.execute('RESET ROLE')
            return result
    assert lock() is True
    assert lock(tid=uuid4()) is False
    assert lock(value={}) is False
    for query,args in (
        ("UPDATE photo.asset_versions SET review_state='review' WHERE id=%s",(scope['version'],)),
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],)),
        ('UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE id=%s',(scope['asset'],)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(scope['original'],)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(query,args);assert lock() is False
        assert lock() is True
    sig='photo.lock_v28_relight_source(uuid,uuid,uuid,uuid,uuid,jsonb)'
    for role in ('photo_scheduler','photo_worker','photo_api','public'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,sig)).fetchone()[0] is (role=='photo_scheduler')


def test_relight_source_locks_block_competing_revocation(database):
    with psycopg.connect(**database) as setup:
        tenant,*_,scope=fixture(setup,snapshot_authority=True)
    base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='c'*64)
    with psycopg.connect(**database) as held:
        held.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        held.execute('SET LOCAL ROLE photo_scheduler')
        assert held.execute('SELECT photo.lock_v28_relight_source(%s,%s,%s,%s,%s,%s)',(tenant,scope['project'],scope['snapshot'],scope['actor'],scope['session'],Jsonb(base))).fetchone()[0] is True
        for query,args in (
            ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],)),
            ("UPDATE photo.asset_versions SET review_state='review' WHERE id=%s",(scope['version'],)),
            ("UPDATE photo.artifacts SET expires_at=clock_timestamp() WHERE id=%s",(scope['original'],)),
            ("UPDATE photo.memberships SET status='suspended' WHERE tenant_id=%s AND user_id=%s",(tenant,scope['actor'])),
        ):
            with psycopg.connect(**database) as competing:
                competing.execute("SET LOCAL lock_timeout='100ms'")
                with pytest.raises(psycopg.errors.LockNotAvailable):competing.execute(query,args)
