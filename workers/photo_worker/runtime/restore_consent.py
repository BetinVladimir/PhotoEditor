"""Verify restored consent against a separately trusted current catalogue.

The checkpoint and current catalogue must be provisioned independently of the
restored DB. A backup's own grant list cannot establish current consent safety.
"""
from dataclasses import dataclass
from hashlib import sha256
import re
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes
from .restore_authority import require_restore_active


@dataclass(frozen=True)
class RestoreConsentCheckpoint:
    grants: int
    sha256: str


@dataclass(frozen=True)
class RestoreConsentEvidence:
    schema_version: str
    grants: int
    sha256: str


def _owned_row(row):
    fields={'tenant_id','project_id','id','purpose','person_id','status','epoch','revision','policy_version'}
    if not isinstance(row,dict) or set(row)!=fields:raise ValueError('RESTORE_CONSENT_ROW')
    value=dict(row)
    for name in ('tenant_id','project_id','id','person_id'):
        if name=='person_id' and value[name] is None:continue
        try:parsed=UUID(value[name])
        except (ValueError,TypeError,AttributeError):raise ValueError('RESTORE_CONSENT_ROW') from None
        if parsed.int==0 or str(parsed)!=value[name]:raise ValueError('RESTORE_CONSENT_ROW')
    if (value['purpose'] not in ('identity_matching','learning_person','learning_photographer')
            or (value['purpose']=='learning_photographer')!=(value['person_id'] is None)
            or value['status'] not in ('granted','revoked')
            or any(type(value[k]) is not int or not 1<=value[k]<=2**63-1 for k in ('epoch','revision'))
            or not isinstance(value['policy_version'],str) or not 1<=len(value['policy_version'])<=256):
        raise ValueError('RESTORE_CONSENT_ROW')
    return value


def verify_restore_consents(current,restored,checkpoint,*,check_active,maximum_grants=10_000_000):
    """Require complete exact catalogues, sorted by (tenant UUID,id UUID).

    No silent epoch downgrade, grant recreation or revoked->granted repair.
    A mismatch requires canonical consent reconciliation before writer access.
    """
    if (not isinstance(checkpoint,RestoreConsentCheckpoint)
            or type(maximum_grants) is not int or not 1<=maximum_grants<=10_000_000
            or type(checkpoint.grants) is not int or not 0<=checkpoint.grants<=maximum_grants
            or not isinstance(checkpoint.sha256,str) or re.fullmatch('[0-9a-f]{64}',checkpoint.sha256) is None
            or not callable(check_active)):
        raise ValueError('RESTORE_CONSENT_INSTALLATION')
    require_restore_active(check_active);left=iter(current);right=iter(restored);end=object()
    digest=sha256(b'photoeditor.restore-consent-catalogue/v1\n');count=0;previous=None
    while True:
        require_restore_active(check_active);a=next(left,end);require_restore_active(check_active)
        if a is end:
            if next(right,end) is not end:raise ValueError('RESTORE_CONSENT_EXTRA_GRANT')
            break
        a=_owned_row(a);raw=canonical_bytes(a)
        key=(a['tenant_id'],a['id'])
        if previous is not None and key<=previous:raise ValueError('RESTORE_CONSENT_ORDER')
        if count>=maximum_grants:raise ValueError('RESTORE_CONSENT_LIMIT')
        require_restore_active(check_active);b=next(right,end);require_restore_active(check_active)
        if b is end or canonical_bytes(_owned_row(b))!=raw:
            raise ValueError('RESTORE_CONSENT_RECONCILIATION_REQUIRED')
        digest.update(raw+b'\n');previous=key;count+=1
    require_restore_active(check_active)
    if count!=checkpoint.grants or digest.hexdigest()!=checkpoint.sha256:
        raise ValueError('RESTORE_CONSENT_CHECKPOINT_MISMATCH')
    return RestoreConsentEvidence('restore-consent-verification/v1',count,digest.hexdigest())


def verify_restore_database_consents(connection,current,checkpoint,*,expected_database,
                                     check_active,maximum_grants=10_000_000):
    """Enumerate every restored grant under one privileged read-only snapshot."""
    import psycopg
    if (not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
            or not callable(check_active)):
        raise ValueError('RESTORE_CONSENT_DATABASE_INSTALLATION')
    require_restore_active(check_active)
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute('SET LOCAL row_security = off')
        connection.execute("SET LOCAL statement_timeout = '30s'")
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_CONSENT_DATABASE_AUTHORITY')
        with connection.cursor(name='restore_consent_catalogue') as cursor:
            cursor.itersize=512
            cursor.execute('SELECT tenant_id,project_id,id,purpose,person_id,status,epoch,revision,policy_version FROM photo.consent_grants ORDER BY tenant_id,id')
            def rows():
                for row in cursor:
                    require_restore_active(check_active)
                    if len(row)!=9:raise ValueError('RESTORE_CONSENT_ROW')
                    yield dict(zip(('tenant_id','project_id','id','purpose','person_id','status','epoch','revision','policy_version'),
                        (str(row[0]),str(row[1]),str(row[2]),row[3],None if row[4] is None else str(row[4]),*row[5:])))
            evidence=verify_restore_consents(current,rows(),checkpoint,
                check_active=check_active,maximum_grants=maximum_grants)
        require_restore_active(check_active)
    require_restore_active(check_active)
    return evidence
