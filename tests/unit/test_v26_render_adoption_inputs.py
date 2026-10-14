from copy import deepcopy
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.render_adoption_inputs import load_render_adoption_inputs


def case():
    parent={'artifact_id':str(uuid4()),'sha256':'b'*64}
    row=(uuid4(),'a'*64,{'backend':'cuda','resource_estimate':{'ram_bytes':123}},[parent,{'artifact_id':str(uuid4()),'sha256':'d'*64}])
    context=SimpleNamespace(operation='render.compose',version='1',attempt=2,fence=3,result_id=uuid4())
    class Connection:
        calls=[]
        def execute(self,statement,args):
            self.calls.append((statement,args));return self
        def fetchone(self):return row
    return Connection(),context,row


def test_exact_scoped_query_and_detached_profile_and_parents():
    connection,context,row=case();tenant,task=uuid4(),uuid4()
    value=load_render_adoption_inputs(connection,tenant,task,context)
    assert connection.calls==[('SELECT * FROM photo.v26_render_adoption_inputs(%s,%s,%s,%s,%s)',
        (tenant,task,2,3,context.result_id))]
    profile=value.execution_profile();profile['resource_estimate']['ram_bytes']=7
    row[2]['resource_estimate']['ram_bytes']=8;row[3][0]['sha256']='c'*64
    assert value.execution_profile()['resource_estimate']['ram_bytes']==123
    assert value.parent_roots[0].sha256=='b'*64
    assert [ref.sha256 for ref in value.parent_roots]==['b'*64,'d'*64]


@pytest.mark.parametrize('kind',['stale','shape','recipe_id','zero_id','sha','profile','nonfinite',
                                  'oversize','parents','many','parent_id','parent_sha','extra','duplicate'])
def test_invalid_primary_rows_do_not_become_replay_evidence(kind):
    connection,context,row=case();row=list(deepcopy(row))
    if kind=='stale':row=None
    elif kind=='shape':row.pop()
    elif kind=='recipe_id':row[0]='bad'
    elif kind=='zero_id':row[0]='00000000-0000-0000-0000-000000000000'
    elif kind=='sha':row[1]='A'*64
    elif kind=='profile':row[2]=[]
    elif kind=='nonfinite':row[2]={'invalid':float('nan')}
    elif kind=='oversize':row[2]={'value':'x'*1048576}
    elif kind=='parents':row[3]={}
    elif kind=='many':row[3]=[{'artifact_id':str(uuid4()),'sha256':'b'*64} for _ in range(34)]
    elif kind=='parent_id':row[3][0]['artifact_id']='invalid'
    elif kind=='parent_sha':row[3][0]['sha256']='x'
    elif kind=='extra':row[3][0]['other']=1
    elif kind=='duplicate':row[3].append(dict(row[3][0]))
    connection.fetchone=lambda:row
    with pytest.raises(AdoptionInvalid,match='STALE' if kind=='stale' else 'INVALID'):
        load_render_adoption_inputs(connection,uuid4(),uuid4(),context)


def test_wrong_operation_never_queries_primary():
    connection,context,_=case();context.operation='layer.compose'
    with pytest.raises(AdoptionInvalid,match='INVALID'):
        load_render_adoption_inputs(connection,uuid4(),uuid4(),context)
    assert not connection.calls
