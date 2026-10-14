"""Concrete restore verification orchestration; never opens writers.

The guard and latest-history reader are process-installed independent operator
adapters. Boolean configuration flags or backup self-declarations cannot replace
actual namespace/writer isolation and current independent watermark checks.
"""
from dataclasses import dataclass
from pathlib import Path
from uuid import UUID
from .restore_deletions import DeletionHistoryCheckpoint
from .restore_deletion_catalogue import RestoreDatabaseDeletionEvidence
from .deletion_history_backup_read import verify_restore_deletion_backup
from .restore_catalogue import RestoreStorageEvidence
from .restore_storage import verify_minio_restore_storage
from .restore_journal import RestoreQuarantineJournal


@dataclass(frozen=True)
class RestorePreflightEvidence:
    schema_version: str
    run_id: UUID
    deletions: RestoreDatabaseDeletionEvidence
    storage: RestoreStorageEvidence
    quarantine_journal: Path


def verify_isolated_restore(connection,client,buckets,scratch_root,journal_root,run_id,
                            deletion_backup,manifest_sha,*,expected_database,
                            assert_isolated_and_paused,read_latest_deletion_checkpoint,
                            check_active,**limits):
    """Verify all supplied state, seal plan only after final live authority checks.

    This is preflight, not full RestoreCheckpoint: lease fencing, access-intent
    invalidation, consent epochs, key restoration, outbox replay, probes and
    explicit operator approval must still complete before any writer unlock.
    """
    if (not isinstance(run_id,UUID) or run_id.int==0 or not callable(check_active)
            or not callable(assert_isolated_and_paused)
            or not callable(read_latest_deletion_checkpoint)):
        raise ValueError('RESTORE_PREFLIGHT_INSTALLATION')
    def guard():
        if check_active() is False:
            raise RuntimeError('RESTORE_PREFLIGHT_INACTIVE')
        if assert_isolated_and_paused() is False:
            raise RuntimeError('RESTORE_WRITERS_NOT_PAUSED')
        if check_active() is False:
            raise RuntimeError('RESTORE_PREFLIGHT_INACTIVE')
    guard()
    checkpoint=read_latest_deletion_checkpoint()
    if not isinstance(checkpoint,DeletionHistoryCheckpoint):
        raise ValueError('RESTORE_DELETION_CHECKPOINT_UNAVAILABLE')
    guard()
    deletions=verify_restore_deletion_backup(connection,deletion_backup,manifest_sha,checkpoint,
        expected_database=expected_database,check_active=guard)
    guard()
    with RestoreQuarantineJournal(journal_root,run_id,check_active=guard,
            maximum_bytes=limits.get('maximum_spool_bytes',1<<30)) as journal:
        storage=verify_minio_restore_storage(connection,client,buckets,scratch_root,
            journal.stage,check_active=guard,**limits)
        if storage.database.database!=expected_database or deletions.database!=expected_database:
            raise ValueError('RESTORE_DATABASE_SCOPE_CHANGED')
        guard()
        current=read_latest_deletion_checkpoint()
        if current!=checkpoint:
            raise ValueError('RESTORE_DELETION_CHECKPOINT_CHANGED')
        guard()
        destination=journal.seal(storage)
    guard()
    return RestorePreflightEvidence('restore-preflight/v1',run_id,deletions,storage,destination)


def verify_restore_with_live_history(connection,client,buckets,scratch_root,journal_root,
                                    run_id,deletion_backup,manifest_sha,*,history_store,
                                    expected_database,assert_isolated_and_paused,
                                    check_active,**limits):
    """Install the concrete live independent journal as latest-head authority.

    Operator provisioning must keep history_store outside restored namespaces
    and keep it current with all production deletion writers. Both authority
    reads validate the entire live chain; the pinned backup must match it.
    """
    from .deletion_history_store import DeletionHistoryStore
    if not isinstance(history_store,DeletionHistoryStore):
        raise ValueError('RESTORE_LIVE_HISTORY_INSTALLATION')
    maximum_entries=limits.pop('maximum_history_entries',10_000_000)
    def latest():
        if check_active() is False:
            raise RuntimeError('RESTORE_PREFLIGHT_INACTIVE')
        checkpoint=history_store.latest_verified_checkpoint(maximum_entries=maximum_entries)
        if check_active() is False:
            raise RuntimeError('RESTORE_PREFLIGHT_INACTIVE')
        return checkpoint
    return verify_isolated_restore(connection,client,buckets,scratch_root,journal_root,
        run_id,deletion_backup,manifest_sha,expected_database=expected_database,
        assert_isolated_and_paused=assert_isolated_and_paused,
        read_latest_deletion_checkpoint=latest,check_active=check_active,**limits)
