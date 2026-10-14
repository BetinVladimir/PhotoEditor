"""Restore verification and compute recovery; never grants writer access."""
from dataclasses import dataclass
from .restore_preflight import RestorePreflightEvidence, verify_restore_with_live_history
from .restore_leases import RestoreLeaseEvidence, fence_restored_compute_leases
from .restore_deletions import DeletionHistoryCheckpoint
from .restore_intents import RestoreIntentEvidence, invalidate_expired_restore_intents
from .restore_outbox import RestoreOutboxEvidence, prepare_restore_outbox
from .restore_mail import RestoreMailEvidence, recover_restored_mail
from .restore_cleanup import RestoreCleanupEvidence, fence_restored_cleanup
from .restore_scheduler_grants import RestoreSchedulerGrantEvidence, fence_restored_scheduler_grants
from .restore_consent import RestoreConsentCheckpoint, RestoreConsentEvidence, verify_restore_database_consents
from .restore_keys import RestoreKeyEvidence, verify_restored_key_versions
from .restore_fence_check import verify_recovery_fences
from .restore_resources import RestoreResourceEvidence, recover_restored_resources
from .restore_quarantine_activate import RestoreQuarantineActivation, activate_restore_quarantine


@dataclass(frozen=True)
class RestoreComputeRecoveryEvidence:
    schema_version: str
    preflight: RestorePreflightEvidence
    leases: RestoreLeaseEvidence
    deletion_checkpoint: DeletionHistoryCheckpoint
    intents: RestoreIntentEvidence
    outbox: RestoreOutboxEvidence
    mail: RestoreMailEvidence
    cleanup: RestoreCleanupEvidence
    consent: RestoreConsentEvidence
    scheduler_grants: RestoreSchedulerGrantEvidence
    keys: RestoreKeyEvidence
    resources: RestoreResourceEvidence
    quarantine: RestoreQuarantineActivation


def recover_verified_restore(connection,client,buckets,scratch_root,journal_root,
                             run_id,deletion_backup,manifest_sha,*,history_store,
                             expected_database,assert_isolated_and_paused,
                             open_current_consents,read_latest_consent_checkpoint,verify_key_version,
                             check_active,maximum_grants=10_000_000,maximum_tasks=10_000_000,maximum_mail_payloads=10_000_000,**limits):
    """Verify pinned backup/storage before any compute authority mutation.

    Recheck independent deletion authority after all reaper transactions; a new
    deletion during recovery requires fresh reconciliation. Failure may retain
    a sealed storage report and committed fencing, neither authorizes writers.
    """
    if (not callable(open_current_consents) or not callable(read_latest_consent_checkpoint)
            or not callable(verify_key_version) or not callable(check_active)
            or not callable(assert_isolated_and_paused)):
        raise ValueError('RESTORE_CONSENT_AUTHORITY_INSTALLATION')
    def active():
        if check_active() is False:
            raise RuntimeError('RESTORE_RECOVERY_INACTIVE')
    def guard():
        active()
        if assert_isolated_and_paused() is False:
            raise RuntimeError('RESTORE_WRITERS_NOT_PAUSED')
        active()
    guard()
    preflight=verify_restore_with_live_history(connection,client,buckets,scratch_root,
        journal_root,run_id,deletion_backup,manifest_sha,history_store=history_store,
        expected_database=expected_database,
        assert_isolated_and_paused=guard,check_active=active,
        **limits)
    history=preflight.deletions.history
    expected=DeletionHistoryCheckpoint(history.sequence,history.head_sha256)
    guard()
    with open_current_consents() as (consent_checkpoint,current_consents):
        if not isinstance(consent_checkpoint,RestoreConsentCheckpoint):
            raise ValueError('RESTORE_CONSENT_CHECKPOINT_UNAVAILABLE')
        consent=verify_restore_database_consents(connection,current_consents,consent_checkpoint,
            expected_database=expected_database,check_active=active,
            maximum_grants=maximum_grants)
    guard()
    keys=verify_restored_key_versions(connection,expected_database=expected_database,
        verify_key_version=verify_key_version,
        assert_isolated_and_paused=guard,check_active=active)
    guard()
    leases=fence_restored_compute_leases(connection,expected_database=expected_database,
        assert_isolated_and_paused=guard,check_active=active,
        maximum_tasks=maximum_tasks)
    if leases.database!=expected_database:
        raise ValueError('RESTORE_RECOVERY_DATABASE_CHANGED')
    guard()
    resources=recover_restored_resources(connection,expected_database=expected_database,
        assert_isolated_and_paused=guard,check_active=active,
        maximum_reservations=maximum_tasks)
    guard()
    scheduler_grants=fence_restored_scheduler_grants(connection,expected_database=expected_database,
        assert_isolated_and_paused=guard,check_active=active)
    guard()
    cleanup=fence_restored_cleanup(connection,expected_database=expected_database,
        assert_isolated_and_paused=guard,check_active=active)
    guard()
    intents=invalidate_expired_restore_intents(connection,expected_database=expected_database,
        assert_isolated_and_paused=guard,check_active=active)
    guard()
    mail=recover_restored_mail(connection,expected_database=expected_database,
        assert_isolated_and_paused=guard,check_active=active,
        maximum_payloads=maximum_mail_payloads)
    guard()
    outbox=prepare_restore_outbox(connection,expected_database=expected_database,
        assert_isolated_and_paused=guard,check_active=active)
    guard()
    quarantine=activate_restore_quarantine(connection,preflight,expected_database=expected_database,
        assert_isolated_and_paused=guard,check_active=active)
    guard()
    verify_recovery_fences(connection,expected_database=expected_database,
        assert_isolated_and_paused=guard,check_active=active)
    guard()
    latest=history_store.latest_verified_checkpoint(
        maximum_entries=limits.get('maximum_history_entries',10_000_000))
    if latest!=expected:raise ValueError('RESTORE_RECOVERY_DELETION_HEAD_CHANGED')
    guard()
    latest_consent=read_latest_consent_checkpoint()
    if latest_consent!=consent_checkpoint:
        raise ValueError('RESTORE_RECOVERY_CONSENT_HEAD_CHANGED')
    guard()
    return RestoreComputeRecoveryEvidence('restore-compute-recovery/v1',preflight,
                                         leases,latest,intents,outbox,mail,cleanup,consent,scheduler_grants,keys,resources,quarantine)


def recover_with_sql_consent_authority(connection,*args,consent_authority,**kwargs):
    """Install concrete independent live SQL consent source into recovery."""
    from .restore_consent_authority import SQLCurrentConsentAuthority
    if (not isinstance(consent_authority,SQLCurrentConsentAuthority)
            or consent_authority.connection is connection
            or 'open_current_consents' in kwargs
            or 'read_latest_consent_checkpoint' in kwargs):
        raise ValueError('RESTORE_SQL_CONSENT_AUTHORITY_INSTALLATION')
    return recover_verified_restore(connection,*args,
        open_current_consents=consent_authority.snapshot,
        read_latest_consent_checkpoint=consent_authority.latest_checkpoint,**kwargs)


def recover_with_installed_crypto(connection,*args,consent_authority,keys_path,
                                  witness_path,witness_sha256,check_active,**kwargs):
    """Concrete crypto and current SQL consent installation for operator restore."""
    from .restore_key_installation import load_restore_key_probe
    if 'verify_key_version' in kwargs:
        raise ValueError('RESTORE_COMPETING_KEY_INSTALLATION')
    probe=load_restore_key_probe(keys_path,witness_path,witness_sha256,
                                 check_active=check_active)
    from .restore_lock import exclusive_restore
    with exclusive_restore(connection,expected_database=kwargs.get('expected_database'),
            assert_isolated_and_paused=kwargs.get('assert_isolated_and_paused'),
            check_active=check_active) as assert_lock_owned:
        guarded=dict(kwargs)
        guarded['assert_isolated_and_paused']=assert_lock_owned
        evidence=recover_with_sql_consent_authority(connection,*args,
            consent_authority=consent_authority,verify_key_version=probe,
            check_active=check_active,**guarded)
    return evidence


def recover_and_report(connection,*args,report_root,build_sha256,backup_at,
                       last_wal_lsn,check_active,**kwargs):
    """Installed recovery plus durable report; still no writer authorization."""
    from datetime import datetime,timezone
    from .restore_report import persist_recovery_report
    started=datetime.now(timezone.utc)
    evidence=recover_with_installed_crypto(connection,*args,check_active=check_active,**kwargs)
    receipt=persist_recovery_report(report_root,evidence,build_sha256=build_sha256,
        backup_at=backup_at,last_wal_lsn=last_wal_lsn,started_at=started,
        finished_at=datetime.now(timezone.utc),check_active=check_active)
    return evidence,receipt


def recover_isolated_and_report(connection,*args,expected_database,
                                expected_system_identifier,expected_data_directory,
                                assert_external_isolation,check_active,**kwargs):
    """Install concrete SQL isolation, crypto, consent and exclusive recovery."""
    from .restore_isolation import SQLRestoreIsolationGuard
    if 'assert_isolated_and_paused' in kwargs:
        raise ValueError('RESTORE_COMPETING_ISOLATION_INSTALLATION')
    guard=SQLRestoreIsolationGuard(connection,expected_database=expected_database,
        expected_system_identifier=expected_system_identifier,
        expected_data_directory=expected_data_directory,
        assert_external_isolation=assert_external_isolation,check_active=check_active)
    return recover_and_report(connection,*args,expected_database=expected_database,
        assert_isolated_and_paused=guard,check_active=check_active,**kwargs)
