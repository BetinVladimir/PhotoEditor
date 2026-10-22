"""Deferred installed-catalog tests in conftest's disposable migrated database."""
import pytest
from psycopg import sql

from photo_worker.runtime.database import TaskDatabase
from photo_worker.runtime.service import _require_retouch_sql_authority, WorkerConfigurationError


GATES = [
    ('preview', 'photo.admit_semantic_retouch_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'),
    ('preview', 'photo.v27_retouch_preview_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,text)'),
    ('preview', 'photo.semantic_retouch_preview_worker_landmark_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text)'),
    ('preview', 'photo.semantic_retouch_preview_protection_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,uuid,text)'),
    ('shown', 'photo.admit_semantic_shown_retouch(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'),
    ('shown', 'photo.v27_shown_retouch_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'),
]


def worker_database(db):
    database = TaskDatabase('same-disposable-transaction')
    def query(statement, args):
        # Savepoint rollback also restores the caller role after errors.
        with db.transaction(force_rollback=True):
            db.execute('SET LOCAL ROLE photo_worker')
            return db.execute(statement, args).fetchone()
    database._query = query
    return database


def test_current_retouch_sql_installation_is_ready(db):
    _require_retouch_sql_authority(worker_database(db), preview_enabled=True, shown_enabled=True)


@pytest.mark.parametrize('gate,signature', GATES)
@pytest.mark.parametrize('mutation', [
    'unsafe_path', 'wrong_owner', 'invoker', 'missing_worker',
    'photo_api', 'photo_gateway', 'photo_scheduler', 'photo_portal',
    'photo_semantic_admission', 'PUBLIC', 'missing_signature',
])
def test_live_catalog_mutation_prevents_retouch_startup(db, gate, signature, mutation):
    database = worker_database(db)
    function = sql.SQL(signature)  # Only hard-coded signatures above; no external input.
    with db.transaction(force_rollback=True):
        if mutation == 'unsafe_path':
            db.execute(sql.SQL('ALTER FUNCTION {} SET search_path=pg_catalog,public,photo').format(function))
        elif mutation == 'wrong_owner':
            db.execute(sql.SQL('ALTER FUNCTION {} OWNER TO photo_api').format(function))
        elif mutation == 'invoker':
            db.execute(sql.SQL('ALTER FUNCTION {} SECURITY INVOKER').format(function))
        elif mutation == 'missing_worker':
            db.execute(sql.SQL('REVOKE EXECUTE ON FUNCTION {} FROM photo_worker').format(function))
        elif mutation == 'missing_signature':
            db.execute(sql.SQL('ALTER FUNCTION {} RENAME TO retouch_readiness_missing_fixture').format(function))
        else:
            db.execute(sql.SQL('GRANT EXECUTE ON FUNCTION {} TO {}').format(function, sql.SQL('PUBLIC') if mutation == 'PUBLIC' else sql.Identifier(mutation)))
        with pytest.raises(WorkerConfigurationError, match='SQL authority is not installed'):
            _require_retouch_sql_authority(database, preview_enabled=gate == 'preview', shown_enabled=gate == 'shown')
    # Every mutation is reverted in the same disposable database.
    _require_retouch_sql_authority(database, preview_enabled=True, shown_enabled=True)
