"""Explicit native learning composition; no qualification inferred from models."""
from types import SimpleNamespace

import pytest

from photo_worker.operations.learning.installation import (
    VerifiedLearningProvider, pinned_learning_evaluation_installation,
)
from photo_worker.operations.learning.pinned_provider import PinnedColorHoldoutProvider


def dependencies():
    pool = SimpleNamespace(lock=SimpleNamespace(lock_id='a'*64, models=[]))
    registry = SimpleNamespace(resolve=lambda name: SimpleNamespace(sha256='b'*64))
    return pool, registry


def test_native_learning_factory_retains_explicit_runtime_and_decoder_verifiers():
    pool, registry = dependencies()
    calls = []
    def verify():
        calls.append('runtime')
    def decoder():
        calls.append('decoder')
    installation = pinned_learning_evaluation_installation(pool, registry,
        verify_runtime=verify, background_verify_runtime=decoder)
    assert isinstance(installation.provider, PinnedColorHoldoutProvider)
    assert installation.provider.pool is pool
    assert installation.provider.registry is registry
    assert installation.provider.background_verify_runtime is decoder
    assert calls == ['runtime']
    bound = VerifiedLearningProvider(installation)
    bound.verify()
    assert calls == ['runtime', 'runtime', 'runtime']


@pytest.mark.parametrize('mode', ['missing', 'unqualified', 'bad_decoder', 'changed_pins'])
def test_native_learning_factory_refuses_invalid_installation(mode):
    pool, registry = dependencies()
    def verify():
        if mode == 'unqualified':
            return False
        if mode == 'changed_pins':
            pool.lock.lock_id = 'c'*64
            # The provider pins are immutable copies of deployment identity. A
            # verifier must reject a live installation whose model lock changed.
            raise ValueError('LIVE_MODEL_LOCK_CHANGED')
    with pytest.raises(ValueError):
        pinned_learning_evaluation_installation(pool, registry,
            verify_runtime=None if mode == 'missing' else verify,
            background_verify_runtime=object() if mode == 'bad_decoder' else None)


def test_native_learning_factory_does_not_infer_background_qualification():
    pool, registry = dependencies()
    installation = pinned_learning_evaluation_installation(pool, registry,
        verify_runtime=lambda: None)
    assert installation.provider.background_verify_runtime is None


@pytest.mark.parametrize('mutation', ['pool', 'registry', 'lock', 'icc', 'decoder'])
def test_native_learning_live_dependency_replacement_refuses(mutation):
    pool, registry = dependencies()
    installation = pinned_learning_evaluation_installation(pool, registry,
        verify_runtime=lambda: None)
    bound = VerifiedLearningProvider(installation)
    if mutation == 'pool':
        installation.provider.pool = object()
    elif mutation == 'registry':
        installation.provider.registry = object()
    elif mutation == 'lock':
        pool.lock.lock_id = 'c'*64
    elif mutation == 'icc':
        registry.resolve = lambda name: SimpleNamespace(sha256='c'*64)
    else:
        installation.provider.background_verify_runtime = lambda: None
    with pytest.raises(ValueError, match='LEARNING_INSTALLATION_CHANGED'):
        bound.verify()


def test_native_learning_dependency_change_during_verification_refuses():
    pool, registry = dependencies()
    armed = []
    def verify():
        if armed:
            pool.lock.lock_id = 'c'*64
    installation = pinned_learning_evaluation_installation(pool, registry,
        verify_runtime=verify)
    armed.append(True)
    with pytest.raises(ValueError, match='LEARNING_INSTALLATION_CHANGED'):
        VerifiedLearningProvider(installation)


@pytest.mark.parametrize('cancelled', [False, True])
def test_task_runtime_failure_preserves_concurrent_cancellation(cancelled):
    from photo_worker.runtime.executor import OperationFailure
    pool, registry = dependencies()
    state = {'armed': False, 'cancelled': False}
    class TaskCancelled(RuntimeError):
        pass
    def verify():
        if state['armed']:
            state['cancelled'] = cancelled
            raise ValueError('RUNTIME_REVOKED')
    def check_active():
        if state['cancelled']:
            raise TaskCancelled('cancelled')
    installation = pinned_learning_evaluation_installation(pool, registry,
        verify_runtime=verify)
    bound = VerifiedLearningProvider(installation)
    state['armed'] = True
    expected = TaskCancelled if cancelled else OperationFailure
    with pytest.raises(expected):
        bound.verify_for_task(SimpleNamespace(check_active=check_active))
