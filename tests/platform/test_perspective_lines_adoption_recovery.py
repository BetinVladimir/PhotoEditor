from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.perspective_lines_adoption_recovery import perspective_lines_adoption_committed
from photo_worker.v2.adoption import AdoptionInvalid


@pytest.mark.parametrize('committed',[True,False])
def test_recovery_preserves_scope_and_boolean_outcome(committed):
    tenant,task=uuid4(),uuid4();calls=[];activity=[]
    def execute(query,args):
        calls.append((query,args));return SimpleNamespace(fetchone=lambda:(committed,))
    connection=SimpleNamespace(autocommit=True,execute=execute)
    assert perspective_lines_adoption_committed(connection,tenant,task,check_active=lambda:activity.append(1)) is committed
    assert calls==[('SELECT photo.v29_perspective_lines_adoption_committed(%s,%s)',(tenant,task))]
    assert activity==[1,1]


@pytest.mark.parametrize('row',[None,(),(None,),(1,),('true',),(True,False)])
def test_recovery_refuses_ambiguous_database_reply(row):
    connection=SimpleNamespace(autocommit=True,execute=lambda *args:SimpleNamespace(fetchone=lambda:row))
    with pytest.raises(AdoptionInvalid):perspective_lines_adoption_committed(connection,uuid4(),uuid4(),check_active=lambda:None)


def test_recovery_cancellation_precedes_sql():
    def stop():raise RuntimeError('cancelled')
    connection=SimpleNamespace(autocommit=True)
    with pytest.raises(RuntimeError,match='cancelled'):
        perspective_lines_adoption_committed(connection,uuid4(),uuid4(),check_active=stop)
