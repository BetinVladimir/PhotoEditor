"""Owner final volume delivery with fresh intent and post-redeem authority checks."""
from dataclasses import dataclass
from time import monotonic
from .restore_download_intent import RestoreDownloadIntentTarget,validate_fixture_download_intent
from .restore_preview_download import RestorePreviewDownloadTarget,validate_preview_download_installation
from .restore_export_result import RestoreEncodedExport
from .restore_export_volume import read_fixture_export_volume
from .restore_intent_lifecycle import create_fixture_owner_intent
from .restore_intent_catalogue import observe_fixture_owner_intent
from .restore_intent_redeem import verify_fixture_intent_delivery
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreVolumeDelivery:
    volume: object
    registration: object
    download: object


def run_fixture_volume_delivery(connection,intent_target,download_target,actor_id,encoded,*,expected_database,
        pepper,tls_context,maximum_seconds,assert_installed_fixture,check_active):
    """No completion report: encoded media qualification is a separate prerequisite."""
    if (not isinstance(intent_target,RestoreDownloadIntentTarget)
            or not isinstance(download_target,RestorePreviewDownloadTarget) or not isinstance(encoded,RestoreEncodedExport)
            or intent_target.download_job_id!=encoded.registration.download_job_id
            or intent_target.volume!=encoded.volume_no or intent_target.origin!=download_target.api_origin
            or intent_target.authorization!=download_target.authorization
            or type(maximum_seconds) is not int or not 1<=maximum_seconds<=60
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_VOLUME_DELIVERY_INSTALLATION')
    validate_fixture_download_intent(intent_target,tls_context=tls_context,
        assert_fixture_scope=assert_installed_fixture,check_active=check_active)
    validate_preview_download_installation(download_target,tls_context=tls_context,
        assert_fixture_scope=assert_installed_fixture,check_active=check_active)
    deadline=monotonic()+maximum_seconds
    def guard():
        require_restore_fixture(check_active,assert_installed_fixture)
        if monotonic()>=deadline:raise ValueError('RESTORE_VOLUME_DELIVERY_TIMEOUT')
    def scope(*_):guard()
    def read():return read_fixture_export_volume(connection,intent_target.tenant_id,encoded,
        expected_database=expected_database,check_active=guard)
    guard();volume=read()
    intent=create_fixture_owner_intent(connection,intent_target,actor_id,volume,expected_database=expected_database,
        pepper=pepper,tls_context=tls_context,assert_installed_fixture=guard,check_active=guard)
    if read()!=volume:raise ValueError('RESTORE_VOLUME_DELIVERY_CHANGED')
    download=verify_fixture_intent_delivery(download_target,intent,volume,tls_context=tls_context,
        assert_fixture_scope=scope,check_active=guard)
    registration=observe_fixture_owner_intent(connection,intent_target,intent.ticket,actor_id,volume,
        expected_database=expected_database,pepper=pepper,check_active=guard,consumed=True)
    if registration!=intent.registration or read()!=volume:
        raise ValueError('RESTORE_VOLUME_DELIVERY_CHANGED')
    guard();return RestoreVolumeDelivery(volume,registration,download)
