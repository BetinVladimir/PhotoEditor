"""Task-local cancellation for native callbacks, including foreign threads."""
from contextlib import contextmanager
from contextvars import ContextVar
from functools import wraps


_guard=ContextVar('photo_signing_task_guard',default=None)


def check_signing_active():
    guard=_guard.get()
    if guard is not None and guard() is False:
        raise RuntimeError('SIGNER_TASK_INACTIVE')


@contextmanager
def signing_task_scope(guard):
    if not callable(guard):raise ValueError('SIGNER_CANCELLATION_GUARD')
    parent=_guard.get()
    live=True
    def combined():
        if not live:raise RuntimeError('SIGNER_TASK_SCOPE_CLOSED')
        if parent is not None and parent() is False:
            raise RuntimeError('SIGNER_TASK_INACTIVE')
        if guard() is False:
            raise RuntimeError('SIGNER_TASK_INACTIVE')
    token=_guard.set(combined)
    try:
        combined()
        yield
        combined()
    finally:
        live=False
        _guard.reset(token)


def with_signing_task(function):
    @wraps(function)
    def wrapped(*args,**kwargs):
        with signing_task_scope(kwargs.get('check_active',lambda:None)):
            return function(*args,**kwargs)
    return wrapped


def bind_current_signing_task(function):
    # Capture the guard itself. ContextVar state does not automatically follow
    # a callback invoked by the native SDK on a different OS/Python thread.
    captured=_guard.get()
    @wraps(function)
    def wrapped(*args,**kwargs):
        with signing_task_scope(captured or (lambda:None)):
            return function(*args,**kwargs)
    return wrapped
