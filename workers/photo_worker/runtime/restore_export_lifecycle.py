"""One native export through physical encoded/ZIP verification and owner delivery."""
from dataclasses import dataclass
from time import monotonic,sleep
from pathlib import Path
from photo_worker.contracts.export import EncodeParams
from .restore_export_geometry import verify_export_geometry
from .restore_export import validate_fixture_export,request_fixture_export
from .restore_export_preflight import verify_fixture_export_preflight
from .restore_export_result import observe_fixture_encoded_export
from .restore_export_physical import verify_fixture_encoded_export
from .restore_export_volume import read_fixture_export_volume
from .restore_archive_physical import verify_fixture_archive
from .restore_legacy_physical import verify_fixture_legacy_working_image
from .restore_download_intent import RestoreDownloadIntentTarget,validate_fixture_download_intent
from .restore_preview_download import validate_preview_download_installation
from .restore_volume_delivery import run_fixture_volume_delivery
from photo_worker.operations.export.encode import _dimensions
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreCompletedExport:
    ticket: object
    physical: object
    archive: object | None
    delivery: object
    polls: int


def run_fixture_export(connection,client,buckets,target,binding,selection,apply_target,apply_ticket,download_target,*,
        expected_database,params,intent_key,pepper,scratch_root,tls_context,maximum_seconds,
        assert_installed_fixture,check_active,alpha_dtype='uint16'):
    """Observations only; independent installation pin/report remain mandatory."""
    if (target.tenant_id!=apply_target.tenant_id or target.origin!=apply_target.origin
            or target.authorization!=apply_target.authorization or download_target.api_origin!=target.origin
            or download_target.authorization!=target.authorization or intent_key==target.idempotency_key
            or not isinstance(params,EncodeParams) or alpha_dtype not in ('uint16','float32')
            or type(pepper) is not bytes or len(pepper)<32
            or type(maximum_seconds) is not int or not 1<=maximum_seconds<=300
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_EXPORT_LIFECYCLE_INSTALLATION')
    validate_fixture_export(target,tls_context=tls_context,assert_fixture_scope=assert_installed_fixture,check_active=check_active)
    validate_preview_download_installation(download_target,tls_context=tls_context,
        assert_fixture_scope=assert_installed_fixture,check_active=check_active)
    # Validate future intent credentials/key before the export POST exists.
    provisional=RestoreDownloadIntentTarget(target.origin,target.tenant_id,target.selection_snapshot_id,1,intent_key,target.authorization)
    validate_fixture_download_intent(provisional,tls_context=tls_context,assert_fixture_scope=assert_installed_fixture,check_active=check_active)
    require_restore_fixture(check_active,assert_installed_fixture)
    root=Path(scratch_root)
    if not root.is_absolute() or root.resolve(strict=True)!=root or not root.is_dir():
        raise ValueError('RESTORE_EXPORT_SCRATCH_ROOT')
    deadline=monotonic()+maximum_seconds
    def guard():
        require_restore_fixture(check_active,assert_installed_fixture)
        if monotonic()>=deadline:raise ValueError('RESTORE_EXPORT_LIFECYCLE_TIMEOUT')
    def scope(*_):guard()
    def source():return verify_fixture_legacy_working_image(connection,client,buckets,apply_target,binding,apply_ticket,
        expected_database=expected_database,assert_fixture_scope=scope,check_active=guard,alpha_dtype=alpha_dtype)
    guard();before=source();width,height=_dimensions(before.width,before.height,params)
    alpha=before.alpha is not None and params.alpha=='preserve'
    verify_export_geometry(params,before,before.result.asset_version_id,width,height,alpha)
    verify_fixture_export_preflight(connection,target,binding,before.result,selection,
        expected_database=expected_database,check_active=guard)
    ticket=request_fixture_export(target,tls_context=tls_context,assert_fixture_scope=scope,check_active=guard)
    def observe():return observe_fixture_encoded_export(connection,target,binding,ticket,selection,before.result.asset_version_id,
        expected_database=expected_database,check_active=guard)
    polls=0
    while True:
        guard();polls+=1
        try:
            encoded=observe()
            volume=read_fixture_export_volume(connection,target.tenant_id,encoded,expected_database=expected_database,check_active=guard)
            break
        except ValueError as exc:
            if str(exc) not in ('RESTORE_EXPORT_RESULT_NOT_READY','RESTORE_EXPORT_VOLUME_NOT_READY'):raise
        guard();sleep(min(.25,max(0,deadline-monotonic())))
    if encoded.delivery_kind not in ('single','zip'):
        raise ValueError('RESTORE_EXPORT_DELIVERY_KIND')
    physical=verify_fixture_encoded_export(connection,client,buckets,target,binding,ticket,selection,before.result.asset_version_id,
        expected_database=expected_database,params=params,source=before,width=width,height=height,alpha=alpha,
        scratch_root=scratch_root,check_active=guard)
    if physical.result!=encoded:raise ValueError('RESTORE_EXPORT_LIFECYCLE_CHANGED')
    archive=None
    if encoded.delivery_kind=='zip':
        archive=verify_fixture_archive(connection,client,buckets,target,selection,encoded,
            expected_database=expected_database,scratch_root=scratch_root,check_active=guard)
        if archive.volume!=volume:raise ValueError('RESTORE_EXPORT_LIFECYCLE_CHANGED')
    intent_target=RestoreDownloadIntentTarget(target.origin,target.tenant_id,ticket.download_job_id,encoded.volume_no,intent_key,target.authorization)
    delivery=run_fixture_volume_delivery(connection,intent_target,download_target,binding.actor_id,encoded,
        expected_database=expected_database,pepper=pepper,tls_context=tls_context,maximum_seconds=60,
        assert_installed_fixture=guard,check_active=guard)
    if delivery.volume!=volume or observe()!=encoded or source()!=before:
        raise ValueError('RESTORE_EXPORT_LIFECYCLE_CHANGED')
    verify_fixture_export_preflight(connection,target,binding,before.result,selection,
        expected_database=expected_database,check_active=guard,require_fresh_key=False)
    guard();return RestoreCompletedExport(ticket,physical,archive,delivery,polls)
