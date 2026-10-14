from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.database import TaskDatabase


@pytest.mark.parametrize('row,expected',[(None,None),((None,),None),(({'pin':'value'},),{'pin':'value'})])
def test_negative_source_transport_is_exact_and_fail_closed(row,expected):
    command=SimpleNamespace(tenant_id=uuid4(),run_id=uuid4(),task_id=uuid4(),attempt=3,
        params=ArtifactRef(artifact_id=uuid4(),sha256='a'*64))
    lease=SimpleNamespace(command=command,fence=17,worker_id=uuid4(),input_digest='b'*64)
    db=TaskDatabase('unused')
    calls=[]
    def query(sql,args):calls.append((sql,args));return row
    db._query=query
    assert db.negative_qualification_evidence(lease,b'{"strength":1}') == expected
    assert calls==[('SELECT photo.v29_worker_negative_qualification_evidence(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
        (command.tenant_id,command.run_id,command.task_id,3,17,lease.worker_id,
         command.params.artifact_id,command.params.sha256,'{"strength":1}',lease.input_digest))]
