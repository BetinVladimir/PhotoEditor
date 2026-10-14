"""Native one-item restore Apply through adoption and complete physical master."""
from dataclasses import dataclass
from time import monotonic,sleep
import psycopg
from .restore_legacy_apply import RestoreLegacyApplyTarget,request_fixture_legacy_apply,validate_fixture_legacy_apply
from .restore_legacy_catalogue import RestoreLegacyApplyBinding
from .restore_legacy_result import observe_fixture_legacy_apply_result
from .restore_legacy_physical import verify_fixture_legacy_working_image
from .restore_apply_download import verify_fixture_apply_download
from .restore_preview_download import RestorePreviewDownloadTarget,validate_preview_download_installation
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreCompletedApply:
    ticket: object
    result: object
    physical: object
    download: object
    polls: int


def require_apply_download_parity(result,download):
    try:
        if (download.artifact_id!=str(result.artifact_id)
                or download.sha256!=result.object.sha256
                or download.bytes_verified!=result.object.size):
            raise ValueError('RESTORE_APPLY_LIFECYCLE_EVIDENCE')
    except (AttributeError,TypeError) as exc:
        raise ValueError('RESTORE_APPLY_LIFECYCLE_EVIDENCE') from exc


def run_fixture_legacy_apply(connection,client,buckets,target,binding,download_target,*,expected_database,tls_context,
        maximum_seconds,assert_installed_fixture,check_active,alpha_dtype='uint16'):
    """Pass-quality fixture; one POST, no retry or writer/report authority.

    Scheduler/worker must already run in the explicitly isolated installation.
    Native transport validates the entire Apply request before sending it.
    """
    if (not isinstance(target,RestoreLegacyApplyTarget) or not isinstance(binding,RestoreLegacyApplyBinding)
            or not isinstance(download_target,RestorePreviewDownloadTarget)
            or download_target.api_origin!=target.origin or download_target.authorization!=target.authorization
            or type(maximum_seconds) is not int or not 1<=maximum_seconds<=300
            or alpha_dtype not in ('uint16','float32')
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_APPLY_LIFECYCLE_INSTALLATION')
    validate_fixture_legacy_apply(target,tls_context=tls_context,
        assert_fixture_scope=assert_installed_fixture,check_active=check_active)
    validate_preview_download_installation(download_target,tls_context=tls_context,
        assert_fixture_scope=assert_installed_fixture,check_active=check_active)
    deadline=monotonic()+maximum_seconds
    def guard():
        require_restore_fixture(check_active,assert_installed_fixture)
        if monotonic()>=deadline:raise ValueError('RESTORE_APPLY_LIFECYCLE_TIMEOUT')
    def scope(*_):guard()
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION READ ONLY');connection.execute("SET LOCAL statement_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_APPLY_LIFECYCLE_AUTHORITY')
        saved=connection.execute('''SELECT EXISTS(SELECT FROM photo.idempotency_records
 WHERE tenant_id=%s AND principal_type='user' AND principal_id=%s AND route='Run.Apply' AND key=%s)''',
            (target.tenant_id,binding.actor_id,target.idempotency_key)).fetchone()
        if saved!=(False,):raise ValueError('RESTORE_APPLY_LIFECYCLE_ALREADY_REQUESTED')
    guard()
    ticket=request_fixture_legacy_apply(target,tls_context=tls_context,assert_fixture_scope=scope,check_active=guard)
    def observe():return observe_fixture_legacy_apply_result(connection,target,binding,ticket,
        expected_database=expected_database,assert_fixture_scope=scope,check_active=guard)
    polls=0
    while True:
        guard();polls+=1
        try:result=observe();guard();break
        except ValueError as exc:
            if str(exc)!='RESTORE_LEGACY_RESULT_NOT_READY':raise
        guard();sleep(min(.25,max(0,deadline-monotonic())))
    physical=verify_fixture_legacy_working_image(connection,client,buckets,target,binding,ticket,
        expected_database=expected_database,assert_fixture_scope=scope,check_active=guard,alpha_dtype=alpha_dtype)
    download=verify_fixture_apply_download(download_target,target.tenant_id,result,
        tls_context=tls_context,assert_fixture_scope=scope,check_active=guard)
    require_apply_download_parity(result,download)
    if physical.result!=result or observe()!=result:raise ValueError('RESTORE_APPLY_LIFECYCLE_CHANGED')
    guard();return RestoreCompletedApply(ticket,result,physical,download,polls)
