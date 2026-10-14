"""Task-local/native-thread guard propagation; no actual SDK/HSM execution."""
from concurrent.futures import ThreadPoolExecutor

import pytest

from photo_worker.operations.provenance.cancellation import (
    signing_task_scope,check_signing_active,bind_current_signing_task,with_signing_task)


def test_guard_reset_after_error_and_nested_guards_preserved():
    calls=[]
    with signing_task_scope(lambda:calls.append('outer')):
        with pytest.raises(RuntimeError):
            with signing_task_scope(lambda:(_ for _ in ()).throw(RuntimeError('cancelled'))):
                pytest.fail('cancelled scope entered')
        before=len(calls)
        check_signing_active()
        assert len(calls)==before+1
    before=len(calls)
    check_signing_active()
    assert len(calls)==before


def test_foreign_thread_callback_retains_task_cancellation():
    live=True
    called=[]
    def active():
        if not live:raise RuntimeError('cancelled')
    with signing_task_scope(active):
        callback=bind_current_signing_task(lambda:called.append('key'))
        live=False
        with ThreadPoolExecutor(max_workers=1) as pool:
            with pytest.raises(RuntimeError,match='cancelled'):pool.submit(callback).result()
        live=True
    assert called==[]
    check_signing_active() # callback must not leak guard into caller context


def test_wrapper_restores_guard_when_operation_fails():
    @with_signing_task
    def operation(*,check_active):
        check_signing_active()
        raise RuntimeError('failed')
    calls=[]
    with pytest.raises(RuntimeError):operation(check_active=lambda:calls.append(True))
    assert len(calls)==2
    check_signing_active()
    assert len(calls)==2



def test_foreign_thread_callback_cannot_outlive_scope_even_if_guard_stays_active():
    called=[]
    with signing_task_scope(lambda:None):
        callback=bind_current_signing_task(lambda:called.append('key'))
    with ThreadPoolExecutor(max_workers=1) as pool:
        with pytest.raises(RuntimeError,match='SIGNER_TASK_SCOPE_CLOSED'):
            pool.submit(callback).result()
    assert called==[]
    check_signing_active()


def test_callback_result_is_refused_if_task_cancelled_during_invocation():
    live=True
    def active():
        if not live:raise RuntimeError('cancelled')
    def invoke():
        nonlocal live
        live=False
        return b'signature'
    with signing_task_scope(active):
        callback=bind_current_signing_task(invoke)
        with pytest.raises(RuntimeError,match='cancelled'):callback()
        live=True


def test_closed_nested_scope_does_not_revoke_parent():
    with signing_task_scope(lambda:None):
        parent_callback=bind_current_signing_task(lambda:'parent')
        with signing_task_scope(lambda:None):
            nested_callback=bind_current_signing_task(lambda:'nested')
        with pytest.raises(RuntimeError,match='SIGNER_TASK_SCOPE_CLOSED'):nested_callback()
        assert parent_callback()=='parent'


def test_explicit_false_guard_refuses_scope_before_operation():
    called=[]
    with pytest.raises(RuntimeError, match='SIGNER_TASK_INACTIVE'):
        with signing_task_scope(lambda: False):
            called.append('operation')
    assert called==[]
    check_signing_active()


def test_explicit_false_guard_refuses_native_callback_from_foreign_thread():
    active=True
    called=[]
    def guard():
        return active
    with signing_task_scope(guard):
        callback=bind_current_signing_task(lambda:called.append('signed'))
        active=False
        with ThreadPoolExecutor(max_workers=1) as pool:
            with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
                pool.submit(callback).result()
        active=True
    assert called==[]


def test_explicit_false_parent_guard_refuses_nested_scope():
    active=True
    with signing_task_scope(lambda: active):
        active=False
        with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
            with signing_task_scope(lambda: None):
                pytest.fail('nested scope entered after cancellation')
        active=True
