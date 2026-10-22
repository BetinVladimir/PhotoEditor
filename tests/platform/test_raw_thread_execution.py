"""Native OpenMP policy enforcement and restoration, not camera qualification."""
import builtins
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
import os
from pathlib import Path
from threading import Event
from types import SimpleNamespace
from uuid import uuid4
import sys
import numpy as np
import pytest
import threadpoolctl
from photo_worker.operations.asset import decode, adapters
from photo_worker.operations.asset.decode import decode_raw, RAWThreadControlError
from photo_worker.runtime.executor import OperationFailure
from photo_worker.contracts.assets import DecodeParams
from photo_worker.contracts.base import ArtifactRef
from test_raw_development import decoder, policy, source


@pytest.fixture
def native(decoder,monkeypatch):
    raw,calls=decoder
    module=sys.modules['rawpy'];module.flags={'OPENMP':True}
    state=dict(pools=[dict(user_api='openmp',num_threads=2,filepath='/fixture/libgomp.so')],applied=True,construction=0)
    class Limiter:
        def __init__(self,pools,limits):
            self.pools=pools;self.previous=[p['num_threads'] for p in pools]
            if state['applied']:
                for p in pools:p['num_threads']=limits
        def __enter__(self):return self
        def __exit__(self,*args):
            if state.get('restore_applied',True):
                for p,old in zip(self.pools,self.previous):p['num_threads']=old
    class Controller:
        def __init__(self):state['construction']+=1;self.pools=state['pools']
        def limit(self,*,limits,user_api):
            assert type(limits) is int and limits>=1 and user_api=='openmp'
            return Limiter(self.pools,limits)
        def info(self):return [dict(p) for p in self.pools]
        def select(self,*,filepath):
            result=object.__new__(Controller);result.pools=[p for p in self.pools if p['filepath']==filepath];return result
    monkeypatch.setattr(threadpoolctl,'ThreadpoolController',Controller)
    monkeypatch.setenv('OMP_NUM_THREADS','2')
    return raw,calls,module,state


def assert_outer_restored(state):
    assert os.environ['OMP_NUM_THREADS']=='2'
    assert all(p['num_threads']==2 for p in state['pools'])


def test_native_decoder_runs_at_one_and_restores_outer_two_without_option_change(native):
    raw,calls,_,state=native;original=raw.postprocess
    def process(**options):
        assert os.environ['OMP_NUM_THREADS']=='1'
        assert state['pools'][0]['num_threads']==1
        return original(**options)
    raw.postprocess=process
    result=decode_raw(source(),policy())
    assert result.shape==(12,16,3) and result.dtype==np.uint16
    assert calls[0]['demosaic_algorithm']=='ahd' and calls[0]['use_camera_wb'] is True
    assert calls[0]['exp_shift']==1.0 and calls[0]['highlight_mode']==0
    assert_outer_restored(state)


@pytest.mark.parametrize('failure',['imread','postprocess','cancel_after_native','layout'])
def test_failure_and_cancellation_restore_actual_native_pool_and_environment(native,failure):
    raw,_,module,state=native;original=raw.postprocess
    def fail():raise module.LibRawError('fixture failure')
    if failure=='imread':module.imread=lambda path:fail()
    elif failure=='postprocess':raw.postprocess=lambda **options:fail()
    elif failure=='layout':raw.postprocess=lambda **options:np.zeros((2,2,3),np.uint8)
    checks=0
    def active():
        nonlocal checks
        checks+=1
        if failure=='cancel_after_native' and checks==5:raise RuntimeError('task cancelled')
    with pytest.raises((decode.AssetError,RuntimeError)):
        decode_raw(source(),policy(),check_active=active)
    assert_outer_restored(state)
    if failure!='imread':assert raw.closed


def test_late_rawpy_import_happens_under_env_one_and_before_pool_discovery(native,monkeypatch):
    raw,_,module,state=native;state['pools']=[];previous_import=builtins.__import__
    def importing(name,*args,**kwargs):
        if name=='rawpy':
            assert os.environ['OMP_NUM_THREADS']=='1' and state['construction']==1
            state['pools'].append(dict(user_api='openmp',num_threads=1,filepath='/fixture/new-libgomp.so'))
        return previous_import(name,*args,**kwargs)
    monkeypatch.setattr(builtins,'__import__',importing)
    decode_raw(source())
    assert state['construction']==3
    assert_outer_restored(state)


@pytest.mark.parametrize('failure',['no_pool','unapplied','missing_build_flags'])
def test_uncontrollable_parallel_runtime_fails_before_imread(native,failure):
    raw,calls,module,state=native
    if failure=='no_pool':state['pools']=[]
    elif failure=='unapplied':state['applied']=False
    else:module.flags={}
    with pytest.raises(RAWThreadControlError):decode_raw(source())
    assert calls==[] and not raw.entered
    assert_outer_restored(state)


def test_serial_libraw_build_does_not_require_nonexistent_openmp_pool(native,monkeypatch):
    _,_,module,state=native;module.flags={'OPENMP':False};state['pools']=[]
    monkeypatch.delenv('OMP_NUM_THREADS')
    decode_raw(source())
    assert 'OMP_NUM_THREADS' not in os.environ


def test_import_failure_restores_environment_before_native_pool_setup(native,monkeypatch):
    _,_,_,state=native;previous_import=builtins.__import__
    def importing(name,*args,**kwargs):
        if name=='rawpy':raise ImportError('fixture unavailable')
        return previous_import(name,*args,**kwargs)
    monkeypatch.setattr(builtins,'__import__',importing)
    with pytest.raises(ImportError):decode_raw(source())
    assert state['construction']==2
    assert_outer_restored(state)


def test_supported_background_caller_is_not_rejected_by_main_thread_constraint(native):
    _,_,_,state=native
    with ThreadPoolExecutor(max_workers=1) as pool:result=pool.submit(decode_raw,source()).result(timeout=5)
    assert result.shape==(12,16,3)
    assert_outer_restored(state)


def test_concurrent_raw_calls_do_not_interleave_process_environment_scopes(native):
    raw,_,_,state=native;original=raw.postprocess;first=Event();release=Event();second=Event();count=0
    def processing(**options):
        nonlocal count
        count+=1
        if count==1:
            first.set();assert release.wait(5)
        else:second.set()
        assert state['pools'][0]['num_threads']==1 and os.environ['OMP_NUM_THREADS']=='1'
        return original(**options)
    raw.postprocess=processing
    with ThreadPoolExecutor(max_workers=2) as pool:
        a=pool.submit(decode_raw,source());assert first.wait(5)
        b=pool.submit(decode_raw,source())
        try:assert not second.wait(.05)
        finally:release.set()
        a.result(timeout=5);b.result(timeout=5)
    assert count==2 and second.is_set()
    assert_outer_restored(state)


def test_adapter_classifies_native_control_failure_as_configuration_not_bad_photo(monkeypatch,tmp_path):
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    params=DecodeParams(source=ref,expected_bytes=123,suffix='.nef')
    context=SimpleNamespace(lease=SimpleNamespace(execution_profile={'backend':'cpu'},command=SimpleNamespace(inputs=[ref])),progress=lambda *a:None,check_active=lambda:None)
    @contextmanager
    def local(*args,**kwargs):yield SimpleNamespace(path=Path('fixture.nef'))
    @contextmanager
    def reserve(*args):yield SimpleNamespace(planned_path=lambda *args:tmp_path/'working.npy')
    artifacts=SimpleNamespace(params=lambda model:params,input=local,scratch=SimpleNamespace(reserve=reserve))
    monkeypatch.setattr(adapters,'verify_size',lambda *a,**k:(123,'a'*64))
    monkeypatch.setattr(adapters,'verify_magic',lambda *a:'raw')
    monkeypatch.setattr(adapters,'verify_suffix',lambda *a:None)
    monkeypatch.setattr(adapters,'read_metadata',lambda *a:SimpleNamespace(width=2,height=2,has_alpha=False))
    failure=RAWThreadControlError('RAW_THREAD_CONTROL_NOT_APPLIED')
    def convert(*a,**k):raise failure
    monkeypatch.setattr(adapters,'icc_to_work',convert)
    with pytest.raises(OperationFailure) as raised:adapters._decode(context,artifacts,None)
    assert raised.value.error.code=='CONFIG_INVALID' and raised.value.__cause__ is failure


@pytest.mark.parametrize('outer,expected',[(None,1),('auto',1),('2,2',1),('2',2),('0',1),('99999999999999',1)])
def test_newly_loaded_pool_restores_explicit_outer_or_conservative_serial(native,monkeypatch,outer,expected):
    _,_,_,state=native;state['pools']=[];original_import=builtins.__import__
    if outer is None:monkeypatch.delenv('OMP_NUM_THREADS')
    else:monkeypatch.setenv('OMP_NUM_THREADS',outer)
    def importing(name,*args,**kwargs):
        if name=='rawpy':
            assert os.environ['OMP_NUM_THREADS']=='1'
            state['pools'].append(dict(user_api='openmp',num_threads=1,filepath='/fixture/new-libgomp.so'))
        return original_import(name,*args,**kwargs)
    monkeypatch.setattr(builtins,'__import__',importing)
    decode_raw(source())
    assert state['pools'][0]['num_threads']==expected
    assert os.environ.get('OMP_NUM_THREADS')==outer


def test_partial_import_new_pool_restores_outer_budget_on_import_error(native,monkeypatch):
    _,_,_,state=native;state['pools']=[];original_import=builtins.__import__
    def importing(name,*args,**kwargs):
        if name=='rawpy':
            state['pools'].append(dict(user_api='openmp',num_threads=1,filepath='/fixture/new-libgomp.so'))
            raise ImportError('partial native load')
        return original_import(name,*args,**kwargs)
    monkeypatch.setattr(builtins,'__import__',importing)
    with pytest.raises(ImportError):decode_raw(source())
    assert_outer_restored(state)


def test_preexisting_pools_restore_exact_counts_not_just_outer_environment(native):
    raw,_,_,state=native
    state['pools']=[dict(user_api='openmp',num_threads=3,filepath='/fixture/libgomp.so'),dict(user_api='openmp',num_threads=5,filepath='/fixture/libomp.so')]
    old=raw.postprocess
    def process(**options):
        assert [p['num_threads'] for p in state['pools']]==[1,1]
        return old(**options)
    raw.postprocess=process;decode_raw(source())
    assert [p['num_threads'] for p in state['pools']]==[3,5]
    assert os.environ['OMP_NUM_THREADS']=='2'


def test_failed_native_restoration_is_not_reported_as_success(native):
    raw,_,_,state=native;old=raw.postprocess
    def process(**options):
        value=old(**options);state['restore_applied']=False;state['applied']=False;return value
    raw.postprocess=process
    with pytest.raises(RAWThreadControlError,match='RAW_THREAD_RESTORE_NOT_APPLIED'):decode_raw(source())
    assert os.environ['OMP_NUM_THREADS']=='2'
