"""Concrete read-only PostgreSQL + MinIO restore verification composition."""
from .restore_catalogue import verify_restore_database_storage
from .restore_objects import MinioRestoreObjects
from .restore_physical_catalogue import sorted_restore_versions
from .restore_authority import require_restore_active


def verify_minio_restore_storage(connection,client,buckets,scratch_root,stage_extra,*,
                                 check_active,maximum_objects=10_000_000,
                                 maximum_bytes=2**53-1,maximum_extra_bytes=2**53-1,
                                 maximum_spool_bytes=1<<30):
    """Build listing, check bytes, and compare references before returning.

    stage_extra must retain incomplete rows as inactive until complete evidence
    is durably accepted. Operator must independently hold storage/database
    writers and outgoing traffic paused in the isolated restore environment.
    No deletion/repair/quarantine activation or writer-unlock is performed here.
    """
    if not callable(stage_extra):raise ValueError('RESTORE_QUARANTINE_INSTALLATION')
    require_restore_active(check_active)
    objects=MinioRestoreObjects(client,buckets,check_active=check_active)
    with sorted_restore_versions(client,buckets,scratch_root,check_active=check_active,
            maximum_entries=maximum_objects,maximum_spool_bytes=maximum_spool_bytes) as physical:
        evidence=verify_restore_database_storage(connection,objects.open_version,
            physical,stage_extra,check_active=check_active,maximum_objects=maximum_objects,
            maximum_bytes=maximum_bytes,maximum_extra_bytes=maximum_extra_bytes)
    require_restore_active(check_active)
    return evidence


def verify_and_journal_restore_storage(connection,client,buckets,scratch_root,
                                       journal_root,run_id,*,check_active,**limits):
    """Seal a durable plan only after complete storage verification returns."""
    from .restore_journal import RestoreQuarantineJournal
    with RestoreQuarantineJournal(journal_root,run_id,check_active=check_active,
            maximum_bytes=limits.get('maximum_spool_bytes',1<<30)) as journal:
        evidence=verify_minio_restore_storage(connection,client,buckets,scratch_root,
            journal.stage,check_active=check_active,**limits)
        destination=journal.seal(evidence)
    return evidence,destination
