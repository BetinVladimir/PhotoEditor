from contextlib import contextmanager
from types import SimpleNamespace
import pytest
from photo_worker.runtime.thread_budget import worker_thread_budget


def test_native_thread_limits_are_applied_and_restored_on_failure(monkeypatch):
    monkeypatch.setattr('os.cpu_count',lambda:8)
    values={'torch':8,'cv2':4};calls=[]
    def module(name,getter,setter):
        def set_value(value):
            calls.append((name,value));values[name]=value
        return SimpleNamespace(**{getter:lambda:values[name],setter:set_value})
    modules={'torch':module('torch','get_num_threads','set_num_threads'),
             'cv2':module('cv2','getNumThreads','setNumThreads')}
    @contextmanager
    def pools(**kwargs):
        calls.append(('pools',kwargs['limits']))
        try:yield
        finally:calls.append(('pools','restored'))
    with pytest.raises(RuntimeError,match='task failed'):
        with worker_thread_budget(2,modules=modules,pool_limits=pools) as effective:
            assert effective==2 and values=={'torch':2,'cv2':2}
            raise RuntimeError('task failed')
    assert values=={'torch':8,'cv2':4}
    assert calls[-1]==('pools','restored')


@pytest.mark.parametrize('threads',[True,0,-1,1.5,2**53])
def test_invalid_thread_limit_refuses_before_native_io(threads):
    with pytest.raises(ValueError,match='THREAD_BUDGET_INVALID'):
        with worker_thread_budget(threads,pool_limits=lambda **kwargs:pytest.fail('native reached')):
            pytest.fail('scope entered')


def test_thread_limit_never_exceeds_host_cpu_count(monkeypatch):
    monkeypatch.setattr('os.cpu_count',lambda:2)
    @contextmanager
    def pools(**kwargs):
        assert kwargs['limits']==2
        yield
    with worker_thread_budget(16,modules={},pool_limits=pools) as effective:
        assert effective==2


def test_future_native_pool_environment_is_scoped_and_restored(monkeypatch):
    import os
    monkeypatch.setattr(os,'cpu_count',lambda:8)
    names=('OMP_NUM_THREADS','OPENBLAS_NUM_THREADS','MKL_NUM_THREADS','NUMEXPR_NUM_THREADS','VECLIB_MAXIMUM_THREADS')
    for name in names:monkeypatch.delenv(name,raising=False)
    monkeypatch.setenv('OMP_NUM_THREADS','7')
    @contextmanager
    def pools(**kwargs):
        assert all(os.environ[name]=='2' for name in names)
        yield
    with worker_thread_budget(2,modules={},pool_limits=pools):
        assert all(os.environ[name]=='2' for name in names)
    assert os.environ['OMP_NUM_THREADS']=='7'
    assert all(name not in os.environ for name in names[1:])


def test_nested_thread_scopes_restore_parent_budget(monkeypatch):
    import os
    monkeypatch.setattr(os,'cpu_count',lambda:8)
    monkeypatch.setenv('OMP_NUM_THREADS','8')
    @contextmanager
    def pools(**kwargs):yield
    with worker_thread_budget(2,modules={},pool_limits=pools):
        with worker_thread_budget(1,modules={},pool_limits=pools):
            assert os.environ['OMP_NUM_THREADS']=='1'
        assert os.environ['OMP_NUM_THREADS']=='2'
    assert os.environ['OMP_NUM_THREADS']=='8'
