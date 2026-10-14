"""One native restore preview through completed catalogue, bytes and API delivery."""
from dataclasses import dataclass
from time import monotonic,sleep
from hashlib import sha256
import psycopg
from .restore_legacy_preview import RestoreLegacyPreviewTarget,request_fixture_legacy_preview,validate_fixture_legacy_preview
from .restore_legacy_catalogue import RestoreLegacyApplyBinding
from .restore_legacy_preview_result import observe_fixture_legacy_preview_result
from .restore_legacy_preview_manifest import verify_fixture_legacy_preview_manifest
from .restore_legacy_preview_jpeg import verify_fixture_legacy_preview_jpeg
from .restore_preview_download import RestorePreviewDownloadTarget,verify_fixture_preview_download,validate_preview_download_installation
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreCompletedPreview:
    ticket: object
    result: object
    manifest: object
    jpeg: object
    download: object
    polls: int


def require_preview_evidence_parity(binding,result,manifest,jpeg,download,srgb_profile):
    """Bind independently observed physical and API evidence to one catalogue."""
    try:
        derivative=result.derivative
        output_manifest=result.manifest
        if (manifest.source.artifact_id!=binding.source_artifact_id
                or manifest.source.sha256!=binding.source_sha256
                or manifest.derivative.artifact_id!=derivative.artifact_id
                or manifest.derivative.sha256!=derivative.object.sha256
                or manifest.manifest_sha256!=output_manifest.object.sha256
                or jpeg.sha256!=derivative.object.sha256
                or jpeg.bytes_verified!=derivative.object.size
                or (jpeg.width,jpeg.height)!=(manifest.width,manifest.height)
                or jpeg.icc_sha256!=sha256(srgb_profile).hexdigest()
                or download.artifact_id!=str(derivative.artifact_id)
                or download.sha256!=derivative.object.sha256
                or download.bytes_verified!=derivative.object.size
                or download.manifest_sha256!=output_manifest.object.sha256
                or download.manifest_bytes_verified!=output_manifest.object.size):
            raise ValueError('RESTORE_PREVIEW_LIFECYCLE_EVIDENCE')
    except (AttributeError,TypeError) as exc:
        raise ValueError('RESTORE_PREVIEW_LIFECYCLE_EVIDENCE') from exc


def run_fixture_preview(connection,client,buckets,target,binding,download_target,*,expected_database,
        source_width,source_height,srgb_profile,tls_context,maximum_seconds,assert_installed_fixture,check_active):
    """Installed isolated scheduler/worker required; does not start services.

    No mutation retry, report publication or writer activation. Geometry and ICC
    are independently installed fixture pins; source verification is upstream.
    """
    if (not isinstance(target,RestoreLegacyPreviewTarget) or not isinstance(binding,RestoreLegacyApplyBinding)
            or not isinstance(download_target,RestorePreviewDownloadTarget)
            or target.asset_version_id!=binding.asset_version_id
            or download_target.api_origin!=target.origin or download_target.authorization!=target.authorization
            or type(maximum_seconds) is not int or not 1<=maximum_seconds<=300
            or type(srgb_profile) is not bytes or not 128<=len(srgb_profile)<=4194304
            or any(type(v) is not int or not 1<=v<=200000 for v in (source_width,source_height))
            or source_width*source_height>200000000
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_PREVIEW_LIFECYCLE_INSTALLATION')
    validate_fixture_legacy_preview(target,tls_context=tls_context,
        assert_fixture_scope=assert_installed_fixture,check_active=check_active)
    validate_preview_download_installation(download_target,tls_context=tls_context,
        assert_fixture_scope=assert_installed_fixture,check_active=check_active)
    if target.roi is not None:
        x,y,width,height=target.roi
        if x+width>source_width or y+height>source_height:
            raise ValueError('RESTORE_PREVIEW_LIFECYCLE_ROI')
    deadline=monotonic()+maximum_seconds
    def guard():
        require_restore_fixture(check_active,assert_installed_fixture)
        if monotonic()>=deadline:raise ValueError('RESTORE_PREVIEW_LIFECYCLE_TIMEOUT')
    def scope(*_):guard()
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION READ ONLY');connection.execute("SET LOCAL statement_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_PREVIEW_LIFECYCLE_AUTHORITY')
        saved=connection.execute('''SELECT EXISTS(SELECT FROM photo.idempotency_records
 WHERE tenant_id=%s AND principal_type='user' AND principal_id=%s AND route='Preview.Request' AND key=%s)''',
            (target.tenant_id,binding.actor_id,target.idempotency_key)).fetchone()
        if saved!=(False,):raise ValueError('RESTORE_PREVIEW_LIFECYCLE_ALREADY_REQUESTED')
    guard()
    ticket=request_fixture_legacy_preview(target,tls_context=tls_context,assert_fixture_scope=scope,check_active=guard)
    polls=0
    def observe():return observe_fixture_legacy_preview_result(connection,target,binding,ticket,
        expected_database=expected_database,assert_fixture_scope=scope,check_active=guard)
    while True:
        guard();polls+=1
        try:result=observe();guard();break
        except ValueError as exc:
            if str(exc)!='RESTORE_PREVIEW_RESULT_NOT_READY':raise
        guard();sleep(min(.25,max(0,deadline-monotonic())))
    manifest=verify_fixture_legacy_preview_manifest(connection,client,buckets,target,binding,ticket,
        expected_database=expected_database,source_width=source_width,source_height=source_height,
        srgb_sha256=sha256(srgb_profile).hexdigest(),assert_fixture_scope=scope,check_active=guard)
    jpeg=verify_fixture_legacy_preview_jpeg(connection,client,buckets,target,binding,ticket,manifest,
        expected_database=expected_database,srgb_profile=srgb_profile,assert_fixture_scope=scope,check_active=guard)
    download=verify_fixture_preview_download(download_target,target.tenant_id,result,
        tls_context=tls_context,assert_fixture_scope=scope,check_active=guard)
    require_preview_evidence_parity(binding,result,manifest,jpeg,download,srgb_profile)
    if observe()!=result:raise ValueError('RESTORE_PREVIEW_LIFECYCLE_CHANGED')
    guard();return RestoreCompletedPreview(ticket,result,manifest,jpeg,download,polls)
