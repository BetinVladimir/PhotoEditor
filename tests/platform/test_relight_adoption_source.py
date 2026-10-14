from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.v2.adoption import ArtifactRef
from photo_worker.v2.relight_adoption_source import load_relight_adoption_source,relight_adoption_source_checkpoint
from test_relight_qualification import evidence


@pytest.mark.parametrize('failure',['','stale','params','root','runtime','extra','checkpoint'])
def test_completed_source_evidence_exact_identity_and_checkpoint(failure):
    params,pin,*_=evidence();tenant,task,result,run,root=uuid4(),uuid4(),uuid4(),uuid4(),uuid4()
    ctx=SimpleNamespace(attempt=1,fence=1,result_id=result,root=ArtifactRef(root,'d'*64))
    value=dict(schema_version=1,run_id=str(run),task_id=str(task),attempt=1,fence=1,result_id=str(result),params_ref=dict(artifact_id=str(uuid4()),sha256=canonical_hash(params)),root_ref=dict(artifact_id=str(root),sha256='d'*64),profile=dict(backend='cpu',renderer_version='v2.2',relight_runtime_sha='b'*64,relight_qualification=pin),qualification=pin)
    if failure=='params':value['params_ref']['sha256']='e'*64
    if failure=='root':value['root_ref']['artifact_id']=str(uuid4())
    if failure=='runtime':value['profile']['relight_runtime_sha']='e'*64
    if failure=='extra':value['extra']=True
    calls=[]
    def execute(sql,args):calls.append((sql,args));return SimpleNamespace(fetchone=lambda:(None if failure=='stale' else value,))
    connection=SimpleNamespace(autocommit=True,execute=execute)
    def load():return load_relight_adoption_source(connection,tenant,task,ctx,canonical_bytes(params),check_active=lambda:None)
    if failure in ('','checkpoint'):
        actual=load();checkpoint=relight_adoption_source_checkpoint(connection,tenant,task,ctx,canonical_bytes(params),actual,check_active=lambda:None)
        assert checkpoint()==actual
        if failure=='checkpoint':
            value['profile']['new_field']='changed'
            with pytest.raises(ValueError,match='RELIGHT_ADOPTION_SOURCE_CHANGED'):checkpoint()
    else:
        with pytest.raises(ValueError):load()
    assert calls[0][1]==(tenant,task,1,1,result,canonical_bytes(params).decode())
