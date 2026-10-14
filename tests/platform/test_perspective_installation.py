from types import SimpleNamespace
import pytest
from photo_worker.operations.optical.perspective_installation import perspective_operations,PerspectiveInstallation
from photo_worker.runtime.database import TaskDatabase


def test_incomplete_installation_refused_before_database_io():
    class NoDatabase:
        def perspective_qualification_evidence(self,*args):raise AssertionError('unexpected IO')
    with pytest.raises(ValueError,match='INSTALLATION_INVALID'):
        perspective_operations(SimpleNamespace(),NoDatabase(),PerspectiveInstallation(None,'photos',None),verify_color=lambda _:None)


@pytest.mark.parametrize('row,expected',[(None,False),((None,),False),((False,),False),((1,),False),((True,),True)])
def test_capability_result_requires_explicit_boolean(row,expected):
    database=TaskDatabase('unused');calls=[]
    def query(sql,args):calls.append((sql,args));return row
    database._query=query
    assert database.perspective_capabilities_ready() is expected
    assert len(calls)==1 and calls[0][1]==()
    assert 'v29_worker_perspective_qualification_evidence' in calls[0][0]
