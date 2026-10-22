from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.semantic.preview_retouch_inputs import retouch_declared_inputs


def ref():return ArtifactRef(artifact_id=uuid4(),sha256='a'*64)


def test_retouch_uses_owned_verified_index_without_command_mutation():
    dependency = ref()
    command = SimpleNamespace(inputs=[ref()])
    artifacts = SimpleNamespace(declared_input_refs=lambda maximum:(dependency,))
    result = retouch_declared_inputs(artifacts,check_active=lambda:None)
    assert result == (dependency,) and result[0] is not dependency
    assert dependency not in command.inputs
    dependency.sha256 = 'b'*64
    assert result[0].sha256 == 'a'*64


@pytest.mark.parametrize('values',[[],(),None,(ref(),)*2])
def test_retouch_refuses_missing_or_invalid_index(values):
    with pytest.raises(ValueError):
        retouch_declared_inputs(SimpleNamespace(declared_input_refs=lambda maximum:values),check_active=lambda:None)
    with pytest.raises(ValueError,match='INDEX_REQUIRED'):
        retouch_declared_inputs(SimpleNamespace(),check_active=lambda:None)


def test_retouch_refuses_cancellation_after_index_discovery():
    lost = False
    def discover(maximum):
        nonlocal lost
        lost = True
        return (ref(),)
    def active():
        if lost:raise InterruptedError('cancelled')
    with pytest.raises(InterruptedError):
        retouch_declared_inputs(SimpleNamespace(declared_input_refs=discover),check_active=active)


@pytest.mark.parametrize('failure', ['nil', 'overflow', 'conflicting_sha'])
def test_retouch_index_refuses_invalid_identity_or_unbounded_inventory(failure):
    from uuid import UUID
    first = ref()
    if failure == 'nil':
        first.artifact_id = UUID(int=0)
        values = (first,)
    elif failure == 'overflow':
        values = tuple(ref() for _ in range(513))
    else:
        other = first.model_copy(deep=True)
        other.sha256 = 'b' * 64
        values = (first, other)
    with pytest.raises(ValueError, match='INPUTS_INVALID'):
        retouch_declared_inputs(SimpleNamespace(declared_input_refs=lambda maximum: values),
            check_active=lambda: None)
