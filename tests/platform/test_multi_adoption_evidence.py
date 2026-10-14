from types import SimpleNamespace
import pytest
from photo_worker.v2.multi_adoption_evidence import load_multi_adoption_evidence,multi_adoption_checkpoint
from photo_worker.v2.adoption import AdoptionInvalid


def test_reader_requires_autocommit_before_database_access():
    with pytest.raises(ValueError,match='READ_CONFIGURATION'):
        load_multi_adoption_evidence(SimpleNamespace(autocommit=False),None,None,None,b'{}',
            operation='optical.panorama',check_active=lambda:None)


def test_missing_context_is_refused_with_exact_coordinates():
    from test_multi_request import fixture
    from photo_worker.contracts.base import canonical_bytes
    params,_,_=fixture();raw=canonical_bytes(params);calls=[]
    context=SimpleNamespace(attempt=2,fence=7,result_id='result')
    class Connection:
        autocommit=True
        def execute(self,sql,args):
            calls.append((sql,args));return SimpleNamespace(fetchone=lambda:None)
    with pytest.raises(AdoptionInvalid,match='CONTEXT_STALE'):
        load_multi_adoption_evidence(Connection(),'tenant','task',context,raw,
            operation='optical.panorama',check_active=lambda:None)
    assert calls[0][1]==('tenant','task',2,7,'result',raw.decode())


def test_checkpoint_refuses_any_evidence_change(monkeypatch):
    import photo_worker.v2.multi_adoption_evidence as module
    current={'profile':{'revision':1}}
    monkeypatch.setattr(module,'load_multi_adoption_evidence',lambda *args,**kwargs:current)
    checkpoint=multi_adoption_checkpoint(None,None,None,SimpleNamespace(),b'{}',current,
        operation='optical.focus_stack',check_active=lambda:None)
    assert checkpoint()==current
    current['profile']['revision']=2
    with pytest.raises(AdoptionInvalid,match='CONTEXT_CHANGED'):checkpoint()
