"""Whole-database ready-artifact verification in an isolated restore namespace.

An operator supplies a dedicated idle privileged connection and read-only object
adapter. No application role gets extra grants. This stage neither changes rows
nor opens writers, and does not replace independent deletion reconciliation.
"""
from dataclasses import dataclass
from datetime import datetime
from uuid import uuid4
import psycopg

from .restore_inventory import RestoreObject, RestoreInventoryEvidence, verify_restore_inventory
from .restore_quarantine import RestoreQuarantineEvidence, plan_restore_quarantine
from .restore_authority import require_restore_active


# Do not filter expired ready rows: expiry alone does not prove that object bytes
# were deleted. Identical physical references collapse; conflicting SHA/size for
# one version remain adjacent and are rejected by the inventory verifier.
READY_OBJECTS_SQL = """
SELECT DISTINCT bucket COLLATE "C", object_key COLLATE "C",
 object_version COLLATE "C", sha256 COLLATE "C", bytes
FROM photo.artifacts
WHERE state='ready'
ORDER BY 1,2,3,4,5
"""


@dataclass(frozen=True)
class RestoreDatabaseEvidence:
    schema_version: str
    database: str
    snapshot: str
    observed_at: datetime
    inventory: RestoreInventoryEvidence


@dataclass(frozen=True)
class RestoreStorageEvidence:
    schema_version: str
    database: RestoreDatabaseEvidence
    quarantine: RestoreQuarantineEvidence


def verify_restore_database_storage(connection,open_version,physical,stage_extra,*,check_active,
                                    maximum_objects=10_000_000,maximum_bytes=2**53-1,
                                    maximum_extra_bytes=2**53-1):
    if physical is None or not callable(stage_extra):
        raise ValueError('RESTORE_QUARANTINE_INSTALLATION')
    return _verify_database(connection,open_version,check_active=check_active,
        maximum_objects=maximum_objects,maximum_bytes=maximum_bytes,
        physical=physical,stage_extra=stage_extra,maximum_extra_bytes=maximum_extra_bytes)


def verify_restore_database_objects(connection, open_version, *, check_active,
                                    maximum_objects=10_000_000,maximum_bytes=2**53-1):
    return _verify_database(connection,open_version,check_active=check_active,
        maximum_objects=maximum_objects,maximum_bytes=maximum_bytes)


def _verify_database(connection, open_version, *, check_active,
                                    maximum_objects=10_000_000,
                                    maximum_bytes=2**53-1,physical=None,stage_extra=None,maximum_extra_bytes=2**53-1):
    """Consume the entire server cursor under REPEATABLE READ READ ONLY.

    An idle connection is mandatory so a caller's existing transaction cannot
    weaken snapshot/isolation/read-only configuration. row_security=off plus a
    privileged operator role prevents silent tenant filtering by application RLS.
    The caller must independently establish that this is an isolated restored
    database and keep writers/outgoing disabled throughout the full restore.
    """
    require_restore_active(check_active)
    if connection.info.transaction_status != psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute('SET LOCAL row_security = off')
        row = connection.execute("""SELECT current_database(), pg_current_snapshot()::text,
 transaction_timestamp(), (SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname=current_user)
""").fetchone()
        if (not row or len(row)!=4 or not isinstance(row[0],str) or not row[0]
                or not isinstance(row[1],str) or not row[1]
                or not isinstance(row[2],datetime) or row[2].tzinfo is None or row[3] is not True):
            raise ValueError('RESTORE_CATALOGUE_AUTHORITY_INVALID')
        require_restore_active(check_active)
        with connection.cursor(name='restore_objects_'+uuid4().hex) as cursor:
            cursor.itersize=512
            cursor.execute(READY_OBJECTS_SQL)
            def references():
                for values in cursor:
                    require_restore_active(check_active)
                    if len(values)!=5:
                        raise ValueError('RESTORE_CATALOGUE_ROW_INVALID')
                    yield RestoreObject(*values)
            inventory=verify_restore_inventory(references(),open_version,
                check_active=check_active,maximum_objects=maximum_objects,
                maximum_bytes=maximum_bytes)
        require_restore_active(check_active)
        evidence=RestoreDatabaseEvidence('restore-database-objects/v1',row[0],row[1],row[2],inventory)
        if physical is not None:
            # Re-enumerate from the SAME repeatable-read snapshot, never a new
            # connection that could omit references added during byte checking.
            with connection.cursor(name='restore_quarantine_'+uuid4().hex) as cursor:
                cursor.itersize=512;cursor.execute(READY_OBJECTS_SQL)
                def expected():
                    for values in cursor:
                        require_restore_active(check_active)
                        if len(values)!=5:raise ValueError('RESTORE_CATALOGUE_ROW_INVALID')
                        yield RestoreObject(*values)
                quarantine=plan_restore_quarantine(expected(),physical,stage_extra,
                    check_active=check_active,maximum_objects=maximum_objects,
                    maximum_extra_bytes=maximum_extra_bytes)
            if quarantine.referenced!=inventory.objects:
                raise ValueError('RESTORE_CATALOGUE_COUNT_MISMATCH')
            evidence=RestoreStorageEvidence('restore-storage-verification/v1',evidence,quarantine)
        require_restore_active(check_active)
    require_restore_active(check_active)
    return evidence
