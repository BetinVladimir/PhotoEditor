"""Compose concrete SQL/MinIO/HTTPS observations after canonical fixture purge."""
from dataclasses import dataclass
from uuid import UUID
from .restore_purge_probe import RestorePurgeMetadataEvidence, verify_purged_tenant_metadata
from .restore_purge_storage import RestorePurgeStorageEvidence, verify_purged_tenant_storage
from .restore_access_probe import RestoreAccessDenialEvidence, observe_restore_access_denied
from .restore_s3_probe import RestoreS3AccessTarget, RestoreS3DenialEvidence, observe_purged_signed_s3
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestorePurgeObservation:
    schema_version: str
    metadata: RestorePurgeMetadataEvidence
    storage: RestorePurgeStorageEvidence
    api_access: RestoreAccessDenialEvidence
    s3_access: RestoreS3DenialEvidence


def observe_completed_fixture_purge(connection,client,buckets,tenant_id,deletion_job_id,*,
                                    expected_database,api_target,s3_target,tls_context,
                                    assert_installed_fixture,check_active):
    """Only reads; does not initiate deletion or fabricate pre-purge baseline.

    Installed fixture authority must bind SQL connection, storage endpoint/all
    buckets, API/S3 targets/principal and retained pre-purge readable baseline to
    this tenant/job. Scope cannot be established from returned observations alone.
    """
    if (not isinstance(tenant_id,UUID) or tenant_id.int==0
            or not isinstance(deletion_job_id,UUID) or deletion_job_id.int==0
            or not isinstance(s3_target,RestoreS3AccessTarget) or s3_target.tenant_id!=tenant_id
            or not isinstance(buckets,(list,tuple)) or s3_target.bucket not in buckets
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_PURGE_OBSERVATION_INSTALLATION')
    installed=tuple(buckets)
    def guard():
        require_restore_fixture(check_active,assert_installed_fixture)
    guard()
    metadata=verify_purged_tenant_metadata(connection,tenant_id,deletion_job_id,
        expected_database=expected_database,assert_fixture_scope=lambda *_:guard(),check_active=guard)
    storage=verify_purged_tenant_storage(client,installed,tenant_id,
        assert_fixture_scope=lambda *_:guard(),check_active=guard)
    api=observe_restore_access_denied(api_target,tls_context=tls_context,
        assert_fixture_scope=lambda *_:guard(),check_active=guard)
    s3=observe_purged_signed_s3(s3_target,tls_context=tls_context,
        assert_fixture_scope=lambda *_:guard(),check_active=guard)
    # Fresh metadata/storage pass closes ordinary inter-stage gaps while the
    # independent operator fixture fence remains continuously mandatory.
    if verify_purged_tenant_metadata(connection,tenant_id,deletion_job_id,
        expected_database=expected_database,assert_fixture_scope=lambda *_:guard(),check_active=guard)!=metadata:
        raise ValueError('RESTORE_PURGE_OBSERVATION_CHANGED')
    if verify_purged_tenant_storage(client,installed,tenant_id,
        assert_fixture_scope=lambda *_:guard(),check_active=guard)!=storage:
        raise ValueError('RESTORE_PURGE_OBSERVATION_CHANGED')
    guard()
    return RestorePurgeObservation('restore-purge-observation/v1',metadata,storage,api,s3)
