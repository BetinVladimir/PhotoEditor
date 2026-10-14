from types import SimpleNamespace
import pytest
from photo_worker.runtime.database import TaskDatabase


@pytest.mark.parametrize('row,expected',[(None,False),((None,),False),((False,),False),((1,),False),((True,),True)])
def test_capability_requires_explicit_boolean(row,expected):
    database=TaskDatabase('unused');calls=[]
    database._query=lambda sql,args:(calls.append((sql,args)) or row)
    assert database.multi_capabilities_ready() is expected
    assert len(calls)==1 and calls[0][1]==()
    assert 'v29_worker_multi_qualification_evidence' in calls[0][0]


def test_evidence_binds_all_lease_coordinates_and_original_body():
    database=TaskDatabase('unused');calls=[]
    database._query=lambda sql,args:(calls.append((sql,args)) or ({'pin':'sentinel'},))
    command=SimpleNamespace(tenant_id='tenant',run_id='run',task_id='task',attempt=3,
        params=SimpleNamespace(artifact_id='params',sha256='sha'))
    lease=SimpleNamespace(command=command,fence=7,worker_id='worker',input_digest='digest')
    assert database.multi_qualification_evidence(lease,b'{}')=={'pin':'sentinel'}
    assert calls[0][1]==('tenant','run','task',3,7,'worker','params','sha','{}','digest')
