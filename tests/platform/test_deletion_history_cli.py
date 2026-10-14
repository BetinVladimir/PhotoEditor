from datetime import datetime,timezone
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.runtime.deletion_history_cli import append_request
from photo_worker.runtime.deletion_history_store import DeletionHistoryStore


def test_deletion_bridge_acknowledges_committed_exact_event_and_replay(tmp_path):
    store=DeletionHistoryStore(tmp_path,check_active=lambda:None)
    request={'schema_version':1,'event_id':str(uuid4()),'tenant_id':str(uuid4()),'occurred_at':datetime.now(timezone.utc).isoformat()}
    first=append_request(canonical_bytes(request),store)
    assert first==append_request(canonical_bytes(request),store)
    response=parse_json(first,max_bytes=4096)
    assert response['event_id']==request['event_id'] and response['tenant_id']==request['tenant_id']
    with store.snapshot() as (head,entries):assert head.sequence==1 and len(list(entries))==1


@pytest.mark.parametrize('patch',[{'schema_version':True},{'unknown':True},{'event_id':'invalid'},{'occurred_at':'2026-10-11T12:00:00'}])
def test_deletion_bridge_refuses_bad_request_before_append(tmp_path,patch):
    store=DeletionHistoryStore(tmp_path,check_active=lambda:None)
    request={'schema_version':1,'event_id':str(uuid4()),'tenant_id':str(uuid4()),'occurred_at':'2026-10-11T12:00:00Z',**patch}
    with pytest.raises((ValueError,TypeError)):append_request(canonical_bytes(request),store)
    with store.snapshot() as (head,entries):assert head.sequence==0 and list(entries)==[]
