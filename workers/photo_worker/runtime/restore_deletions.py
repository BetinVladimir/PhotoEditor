"""Verify independent append-only deletion history against restored tenants.

The latest checkpoint must come from a trusted independent backup authority,
never from the restored database or a self-declared journal file. This verifier
performs no purge and never treats a missing history segment as success.
"""
from dataclasses import dataclass
from datetime import datetime
from hashlib import sha256
import re
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes
from .restore_authority import require_restore_active


@dataclass(frozen=True)
class DeletionHistoryCheckpoint:
    sequence: int
    head_sha256: str


@dataclass(frozen=True)
class RestoreDeletionEvidence:
    schema_version: str
    sequence: int
    head_sha256: str
    checked_tenants: int


def _sha(value):return isinstance(value,str) and re.fullmatch(r'[0-9a-f]{64}',value) is not None


def verify_restore_deletions(entries,checkpoint,read_tenant_status,*,check_active,
                             maximum_entries=10_000_000):
    """Require the entire chain from genesis through the trusted latest head.

    Both requested and completed deletions forbid an active restored tenant.
    Tenant absence is safe; a present tenant must be deleting/deleted. Repeated
    events are checked again without retaining a tenant-sized in-memory set.
    checked_tenants counts event observations, not distinct tenants.
    """
    if (not isinstance(checkpoint,DeletionHistoryCheckpoint)
            or type(checkpoint.sequence) is not int or not 0<=checkpoint.sequence<=2**53-1
            or not _sha(checkpoint.head_sha256) or not callable(read_tenant_status)
            or not callable(check_active) or type(maximum_entries) is not int
            or not 1<=maximum_entries<=10_000_000 or checkpoint.sequence>maximum_entries):
        raise ValueError('RESTORE_DELETION_INSTALLATION')
    require_restore_active(check_active);sequence=checked=0;head='0'*64
    for entry in entries:
        require_restore_active(check_active)
        if (not isinstance(entry,dict) or set(entry)!= {'schema_version','sequence','previous_sha256','tenant_id','phase','occurred_at'}
                or entry['schema_version']!='tenant-deletion-history/v1'
                or type(entry['sequence']) is not int or entry['sequence']!=sequence+1
                or entry['previous_sha256']!=head or sequence>=maximum_entries
                or entry['phase'] not in ('requested','completed')):
            raise ValueError('RESTORE_DELETION_CHAIN_INVALID')
        try:
            tenant=UUID(entry['tenant_id'])
            occurred=datetime.fromisoformat(entry['occurred_at'].replace('Z','+00:00'))
        except (ValueError,TypeError,AttributeError):
            raise ValueError('RESTORE_DELETION_EVENT_INVALID') from None
        if str(tenant)!=entry['tenant_id'] or tenant.int==0 or occurred.tzinfo is None:
            raise ValueError('RESTORE_DELETION_EVENT_INVALID')
        # Freeze exact event bytes before independent database IO.
        raw=canonical_bytes(entry)
        status=read_tenant_status(tenant)
        require_restore_active(check_active)
        if status not in (None,'deleting','deleted'):
            raise ValueError('RESTORE_DELETED_TENANT_RESURRECTED')
        head=sha256(raw).hexdigest();sequence+=1;checked+=1
    require_restore_active(check_active)
    if sequence!=checkpoint.sequence or head!=checkpoint.head_sha256:
        raise ValueError('RESTORE_DELETION_HISTORY_INCOMPLETE')
    return RestoreDeletionEvidence('restore-deletion-reconciliation/v1',sequence,head,checked)
