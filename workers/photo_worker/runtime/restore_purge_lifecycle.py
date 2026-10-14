"""Canonical fixture tenant deletion request and bounded read-only completion wait."""
from dataclasses import dataclass, field
import http.client
import ssl
from time import monotonic, sleep
from urllib.parse import urlsplit
from uuid import UUID
import psycopg
from photo_worker.contracts.base import parse_json
from .restore_authority import require_restore_active,require_restore_fixture


@dataclass(frozen=True)
class RestoreTenantDeletionTarget:
    origin: str
    tenant_id: UUID
    revision: int
    authorization: str = field(repr=False)


@dataclass(frozen=True)
class RestorePurgeCompletion:
    database: str
    tenant_id: UUID
    deletion_job_id: UUID
    polls: int


def request_fixture_tenant_deletion(target, *, tls_context, assert_fixture_scope, check_active):
    """Native-client DELETE uses existing revision/owner authorization unchanged.

    A transport failure has unknown outcome. Never automatically resend, change
    revision, manufacture completion or log credential-bearing request details.
    """
    if (not isinstance(target,RestoreTenantDeletionTarget) or not isinstance(target.tenant_id,UUID)
            or target.tenant_id.int==0 or type(target.revision) is not int or not 1<=target.revision<=2**63-1
            or not isinstance(target.origin,str) or not isinstance(target.authorization,str)
            or not target.authorization.startswith('Bearer ') or not 8<=len(target.authorization)<=8192
            or any(c in target.authorization for c in '\r\n')
            or not isinstance(tls_context,ssl.SSLContext) or tls_context.verify_mode!=ssl.CERT_REQUIRED
            or not tls_context.check_hostname or tls_context.minimum_version<ssl.TLSVersion.TLSv1_2
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_PURGE_REQUEST_INSTALLATION')
    origin=urlsplit(target.origin)
    if (origin.scheme!='https' or not origin.hostname or origin.username is not None
            or origin.password is not None or origin.path not in ('','/') or origin.query or origin.fragment):
        raise ValueError('RESTORE_PURGE_REQUEST_ORIGIN')
    def guard():require_restore_fixture(check_active,assert_fixture_scope,target)
    guard();connection=http.client.HTTPSConnection(origin.hostname,origin.port or 443,context=tls_context,timeout=5)
    try:
        connection.request('DELETE','/api/v1/tenants/'+str(target.tenant_id),headers={
            'Authorization':target.authorization,'X-Photo-Client':'native','If-Match':'"'+str(target.revision)+'"',
            'Accept':'application/json','Accept-Encoding':'identity'})
        response=connection.getresponse();guard();raw=response.read(16385)
        if (response.status!=202 or len(raw)>16384
                or response.getheader('Content-Encoding') not in (None,'identity')
                or response.getheader('Content-Type','').split(';')[0].strip().lower()!='application/json'):
            raise ValueError('RESTORE_PURGE_REQUEST_REFUSED')
        value=parse_json(raw,max_bytes=16384)
        if not isinstance(value,dict) or set(value)!= {'job_id','status'} or value['status']!='queued' or not isinstance(value['job_id'],str):
            raise ValueError('RESTORE_PURGE_REQUEST_RECEIPT')
        job=UUID(value['job_id'])
        if job.int==0 or str(job)!=value['job_id']:raise ValueError('RESTORE_PURGE_REQUEST_RECEIPT')
        guard();return job
    except (OSError,http.client.HTTPException):raise ValueError('RESTORE_PURGE_REQUEST_OUTCOME_UNKNOWN') from None
    finally:connection.close()


def wait_for_fixture_purge(connection,tenant_id,deletion_job_id,*,expected_database,
                           maximum_seconds,assert_fixture_scope,check_active):
    if (not isinstance(tenant_id,UUID) or tenant_id.int==0 or not isinstance(deletion_job_id,UUID)
            or deletion_job_id.int==0 or not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
            or type(maximum_seconds) is not int or not 1<=maximum_seconds<=300
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_PURGE_WAIT_INSTALLATION')
    deadline=monotonic()+maximum_seconds;polls=0
    def guard():
        require_restore_fixture(check_active,assert_fixture_scope,tenant_id,deletion_job_id)
        if monotonic()>=deadline:raise ValueError('RESTORE_PURGE_WAIT_TIMEOUT')
    while True:
        guard()
        if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
            raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
        with connection.transaction():
            connection.execute('SET TRANSACTION READ ONLY')
            connection.execute("SET LOCAL statement_timeout='5s'")
            connection.execute('SET LOCAL row_security=off')
            scope=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
            if scope!=(expected_database,True):raise ValueError('RESTORE_PURGE_WAIT_AUTHORITY')
            row=connection.execute('''SELECT j.status,t.status,j.cleanup_token,j.cleanup_lease_until
 FROM photo.deletion_jobs j JOIN photo.tenants t ON t.id=j.tenant_id
 WHERE j.id=%s AND j.tenant_id=%s AND j.resource_type='tenant' AND j.resource_id=%s''',
                (deletion_job_id,tenant_id,tenant_id)).fetchone()
            polls+=1
            if not row or len(row)!=4 or row[0] not in ('queued','running','completed'):
                raise ValueError('RESTORE_PURGE_WAIT_JOB')
            if row[0]=='completed' and row!=('completed','deleted',None,None):
                raise ValueError('RESTORE_PURGE_WAIT_COMPLETION')
            if row[0]!='completed' and row[1]!='deleting':raise ValueError('RESTORE_PURGE_WAIT_TENANT')
            guard()
        guard()
        if row[0]=='completed':return RestorePurgeCompletion(expected_database,tenant_id,deletion_job_id,polls)
        sleep(min(0.25,max(0,deadline-monotonic())))


def verify_fixture_purge_start(connection,target,*,expected_database,assert_fixture_scope,check_active):
    """Read-only exact SQL target/revision precondition before destructive HTTP."""
    if not isinstance(target,RestoreTenantDeletionTarget) or not callable(assert_fixture_scope) or not callable(check_active):
        raise ValueError('RESTORE_PURGE_START_INSTALLATION')
    require_restore_fixture(check_active,assert_fixture_scope,target)
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        scope=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if scope!=(expected_database,True):raise ValueError('RESTORE_PURGE_START_AUTHORITY')
        row=connection.execute('SELECT status,revision FROM photo.tenants WHERE id=%s',(target.tenant_id,)).fetchone()
        if row!=('active',target.revision):raise ValueError('RESTORE_PURGE_START_TENANT')
        require_restore_fixture(check_active,assert_fixture_scope,target)
    require_restore_active(check_active)
