"""Authored startup boundary; no operation/model/SQL execution is required."""
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.runtime.executor import Executor, Operation


@pytest.mark.parametrize("operation", [
    SimpleNamespace(name='image.generate', resource_class='gpu', version='1', execute=lambda ctx: None),
    Operation('', 'cpu', lambda ctx: None),
    Operation('x' * 121, 'cpu', lambda ctx: None),
    Operation('image.generate', 'gpu', None),
    Operation('image.generate', 'gpu', lambda ctx: None, version='2'),
    Operation('image.generate', 'unregistered-class', lambda ctx: None),
])
def test_invalid_factory_output_is_rejected_before_worker_can_advertise(operation):
    with pytest.raises(ValueError, match='Invalid operation installation'):
        Executor(object(), uuid4(), [operation])


def test_valid_installation_is_retained_without_executing_or_claiming():
    calls = []
    def execute(context):
        calls.append(context)
    operation = Operation('image.generate', 'gpu', execute)
    executor = Executor(object(), uuid4(), [operation])
    assert executor.operations == {'image.generate': operation}
    assert calls == []


def test_duplicate_factory_operations_cannot_replace_an_installed_executor():
    first = Operation('image.generate', 'gpu', lambda ctx: None)
    second = Operation('image.generate', 'gpu', lambda ctx: None)
    with pytest.raises(ValueError, match='Duplicate operation'):
        Executor(object(), uuid4(), [first, second])
