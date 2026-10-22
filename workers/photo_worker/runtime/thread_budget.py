"""Process-local native CPU limits; one heavy task runs per worker process.

This does not limit separate processes or attest deterministic image quality.
"""
from contextlib import ExitStack, contextmanager
import os
import sys
from threading import current_thread, main_thread


@contextmanager
def worker_thread_budget(threads, *, modules=None, pool_limits=None):
    if current_thread() is not main_thread():
        raise ValueError('THREAD_BUDGET_MAIN_THREAD')
    if type(threads) is not int or not 1 <= threads <= 2**53-1:
        raise ValueError('THREAD_BUDGET_INVALID')
    effective = min(threads, max(1, os.cpu_count() or 1))
    if pool_limits is None:
        from threadpoolctl import threadpool_limits as pool_limits
    modules = sys.modules if modules is None else modules
    with ExitStack() as scope:
        # Native pools imported later inherit the same process deployment cap.
        def restore_environment(name, previous):
            if previous is None:
                os.environ.pop(name,None)
            else:
                os.environ[name]=previous
        for name in ('OMP_NUM_THREADS','OPENBLAS_NUM_THREADS','MKL_NUM_THREADS',
                     'NUMEXPR_NUM_THREADS','VECLIB_MAXIMUM_THREADS'):
            scope.callback(restore_environment,name,os.environ.get(name))
            os.environ[name]=str(effective)
        scope.enter_context(pool_limits(limits=effective))
        for name, getter, setter in [('torch','get_num_threads','set_num_threads'),
                                     ('cv2','getNumThreads','setNumThreads')]:
            module = modules.get(name)
            if module is None:
                continue
            get, set_value = getattr(module,getter,None), getattr(module,setter,None)
            if not callable(get) or not callable(set_value):
                raise ValueError('THREAD_BUDGET_RUNTIME_INVALID')
            previous = get()
            if type(previous) is not int or previous < 0:
                raise ValueError('THREAD_BUDGET_RUNTIME_INVALID')
            def restore_native(setter=set_value,getter=get,old=previous,runtime=name):
                setter(old)
                # A nested GCD scope may have inherited sequential mode. A
                # positive one is ignored by GCD just like any positive cap.
                if runtime=='cv2' and old==1 and getter()!=1:
                    setter(0)
            scope.callback(restore_native)
            set_value(effective)
            applied=get()
            # OpenCV's GCD backend (macOS) ignores positive thread counts.
            # Its documented zero setting disables parallel execution instead;
            # verify that fallback rather than silently exceeding the budget.
            if name=='cv2' and type(applied) is int and applied>effective:
                set_value(0)
                applied=get()
            if type(applied) is not int or not 1<=applied<=effective:
                raise ValueError('THREAD_BUDGET_NOT_APPLIED')
        yield effective
