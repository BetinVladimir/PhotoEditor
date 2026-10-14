"""Authored qualification timing persistence boundary; NOT RUN."""
import pytest
import psycopg
from psycopg.types.json import Jsonb

@pytest.mark.parametrize('value,expected',[(0,None),(0.1,1),(1000,1000),(1000.01,1001),(9007199254740991,9007199254740991)])
def test_qualification_timing_rounding(db,value,expected):
    assert db.execute('SELECT photo.qualification_timing_ms(%s)',(Jsonb({'p50_ms':0,'p95_ms':value}),)).fetchone()==(expected,)

@pytest.mark.parametrize('metrics',[None,{}, {'p50_ms':0,'p95_ms':None},{'p50_ms':0,'p95_ms':'1'},{'p50_ms':0,'p95_ms':True},{'p50_ms':-1,'p95_ms':1},{'p50_ms':2,'p95_ms':1},{'p50_ms':0,'p95_ms':9007199254740992}])
def test_qualification_timing_invalid(db,metrics):
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        with db.transaction():
            db.execute('SELECT photo.qualification_timing_ms(%s)',(Jsonb(metrics),))

@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler'])
def test_qualification_timing_helper_private(db,role):
    assert db.execute("SELECT has_function_privilege(%s,'photo.qualification_timing_ms(jsonb)','EXECUTE')",(role,)).fetchone()==(False,)
