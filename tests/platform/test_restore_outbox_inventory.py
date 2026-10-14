from hashlib import sha256
from uuid import UUID
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.restore_outbox_inventory import DOMAIN, pending_task_inventory


def row(i=1):
    return (UUID(int=i),UUID(int=100),UUID(int=200+i),'tasks','route',{'task':i})


def test_pending_inventory_canonical_digest_and_empty():
    value=row()
    expected=sha256(DOMAIN+canonical_bytes({'id':str(value[0]),'tenant_id':str(value[1]),
        'event_id':str(value[2]),'destination':'tasks','routing_key':'route','payload':{'task':1}})+b'\n').hexdigest()
    assert pending_task_inventory([value],expected_count=1,check_active=lambda:None)==expected
    assert pending_task_inventory([],expected_count=0,check_active=lambda:None)==sha256(DOMAIN).hexdigest()


@pytest.mark.parametrize('mode',['reordered','duplicate','short','extra','nil_uuid','foreign_payload','oversized','cancelled'])
def test_pending_inventory_refuses_incomplete_or_invalid_envelopes(mode):
    rows=[row(1),row(2)];count=2
    if mode=='reordered':rows.reverse()
    if mode=='duplicate':rows[1]=rows[0]
    if mode=='short':rows.pop()
    if mode=='extra':count=1
    if mode=='nil_uuid':rows[0]=(UUID(int=0),*rows[0][1:])
    if mode=='foreign_payload':rows[0]=(*rows[0][:-1],[])
    if mode=='oversized':rows[0]=(*rows[0][:-1],{'data':'x'*(1<<20)})
    def active():
        if mode=='cancelled':raise RuntimeError('cancelled')
    with pytest.raises((ValueError,RuntimeError)):
        pending_task_inventory(rows,expected_count=count,check_active=active)


def test_same_count_different_payload_changes_inventory():
    original=row();changed=(*original[:-1],{'task':999})
    assert pending_task_inventory([original],expected_count=1,check_active=lambda:None)!=pending_task_inventory([changed],expected_count=1,check_active=lambda:None)


@pytest.mark.parametrize('denial_at',[1,3])
def test_pending_inventory_explicit_false_refuses_before_digest(denial_at):
    checks=0;consumed=[]
    def active():
        nonlocal checks
        checks+=1
        return False if checks==denial_at else None
    def rows():
        consumed.append(True);yield row()
    with pytest.raises(RuntimeError,match='RESTORE_INACTIVE'):
        pending_task_inventory(rows(),expected_count=1,check_active=active)
    assert consumed==([] if denial_at==1 else [True])


def test_shared_python_go_inventory_golden_bytes():
    import json
    from pathlib import Path
    fixture=json.loads((Path(__file__).resolve().parents[1]/'fixtures'/'restore-task-outbox-v1.json').read_text())
    assert fixture['schema_version']=='restore-task-outbox-golden/v1'
    assert fixture['domain'].encode()==DOMAIN
    rows=[]
    for envelope,expected in zip(fixture['rows'],fixture['canonical_lines'],strict=True):
        assert canonical_bytes(envelope)+b'\n'==expected.encode()
        rows.append(tuple(envelope[key] for key in ('id','tenant_id','event_id','destination','routing_key','payload')))
    expected=sha256((fixture['domain']+''.join(fixture['canonical_lines'])).encode()).hexdigest()
    assert pending_task_inventory(rows,expected_count=len(rows),check_active=lambda:None)==expected
