from contextlib import contextmanager
import pytest
from test_multi_admission import installed
from photo_worker.operations.optical import multi_admitted_prepare as module
from photo_worker.runtime.executor import OperationFailure


def test_admit_before_prepare_recheck_before_yield_and_cleanup(monkeypatch):
    admission,ctx,params,images,records,calls,_=installed(monkeypatch)
    events=[];sentinel=object()
    @contextmanager
    def prepare(request,sources,rgbs,alphas,scratch,**kwargs):
        assert len(calls)==2
        assert sources==tuple(images) and sources[0] is not images[0]
        assert kwargs['ram_max_bytes']==1<<20
        events.append('prepare')
        try:yield sentinel
        finally:events.append('cleanup')
    monkeypatch.setattr(module,'prepare_frozen_multi_request',prepare)
    with module.prepare_admitted_multi(ctx,admission,params,images,[],[],None,ram_max_bytes=1<<20) as result:
        assert result is sentinel and len(calls)==4
        events.append('yield')
    assert events==['prepare','yield','cleanup']


def test_revocation_after_preparation_cleans_without_yield(monkeypatch):
    from dataclasses import replace
    admission,ctx,params,images,records,calls,_=installed(monkeypatch)
    cleaned=[]
    @contextmanager
    def prepare(*args,**kwargs):
        try:
            records[0]=replace(records[0],params_jcs=b'{}')
            yield object()
        finally:cleaned.append(True)
    monkeypatch.setattr(module,'prepare_frozen_multi_request',prepare)
    with pytest.raises(OperationFailure,match='SCOPE_STALE'):
        with module.prepare_admitted_multi(ctx,admission,params,images,[],[],None,ram_max_bytes=1<<20):
            raise AssertionError('revoked result must never be handed off')
    assert cleaned==[True]


def test_prepare_cannot_exceed_leased_ram(monkeypatch):
    admission,ctx,params,images,records,calls,_=installed(monkeypatch)
    def forbidden(*args,**kwargs):raise AssertionError('must reject before allocation')
    monkeypatch.setattr(module,'prepare_frozen_multi_request',forbidden)
    with pytest.raises(OperationFailure,match='RESOURCE_EXCEEDED'):
        with module.prepare_admitted_multi(ctx,admission,params,images,[],[],None,ram_max_bytes=(1<<30)+1):
            pass
