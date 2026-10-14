from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.restore_deletions import DeletionHistoryCheckpoint,verify_restore_deletions


def event(tenant):return {'schema_version':'tenant-deletion-history/v1','sequence':1,'previous_sha256':'0'*64,'tenant_id':str(tenant),'phase':'requested','occurred_at':'2026-10-11T12:00:00Z'}
def checkpoint(entry):return DeletionHistoryCheckpoint(1,sha256(canonical_bytes(entry)).hexdigest())


@pytest.mark.parametrize('status',[None,'deleting','deleted'])
def test_restore_deletion_history_accepts_only_nonresurrected_tenants(status):
    tenant=uuid4();entry=event(tenant);seen=[]
    def read(got):seen.append(got);return status
    result=verify_restore_deletions([entry],checkpoint(entry),read,check_active=lambda:None)
    assert result.sequence==1 and seen==[tenant]


@pytest.mark.parametrize('mode',['active','suspended','missing_tail','wrong_head','gap','previous','phase','unknown','cancelled'])
def test_restore_deletion_history_refuses_resurrection_or_incomplete_authority(mode):
    tenant=uuid4();entry=event(tenant);expected=checkpoint(entry);entries=[entry];checks=0
    if mode=='missing_tail':entries=[]
    if mode=='wrong_head':expected=DeletionHistoryCheckpoint(1,'b'*64)
    if mode=='gap':entry['sequence']=2
    if mode=='previous':entry['previous_sha256']='b'*64
    if mode=='phase':entry['phase']='undo'
    if mode=='unknown':entry['approved']=True
    def active():
        nonlocal checks
        checks+=1
        if mode=='cancelled' and checks==3:raise RuntimeError('cancelled')
    with pytest.raises((ValueError,RuntimeError)):
        verify_restore_deletions(entries,expected,lambda _:mode if mode in ('active','suspended') else 'deleted',check_active=active)


@pytest.mark.parametrize('denial_at',[1,3])
def test_deletion_history_explicit_false_refuses_before_evidence(denial_at):
    tenant=uuid4();entry=event(tenant);checks=0;reads=[]
    def active():
        nonlocal checks
        checks+=1
        return False if checks==denial_at else None
    def read(value):reads.append(value);return 'deleted'
    with pytest.raises(RuntimeError,match='RESTORE_INACTIVE'):
        verify_restore_deletions([entry],checkpoint(entry),read,check_active=active)
    assert reads==([] if denial_at==1 else [tenant])
