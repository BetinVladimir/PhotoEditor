"""Mounted worker SQL readiness response contract; authored, not executed."""
import pytest
from photo_worker.runtime.database import TaskDatabase


@pytest.mark.parametrize('row,ready',[(None,False),((False,),False),((None,),False),((True,),True),((1,),False),((True,False),False)])
def test_worker_media_renderer_policy_requires_exact_sql_ready(row,ready):
    database=object.__new__(TaskDatabase)
    calls=[]
    database._query=lambda query,args:calls.append((query,args)) or row
    assert database.media_renderer_policy_ready() is ready
    assert calls==[('SELECT photo.media_renderer_policy_ready()',())]
