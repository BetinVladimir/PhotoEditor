"""Registration boundaries, not execution or hardware qualification."""
from types import SimpleNamespace
import pytest
from photo_worker.operations.semantic.preview_background_cleanup_adapter import background_cleanup_preview_operations


@pytest.mark.parametrize('backend',['cpu','cuda','mps'])
def test_cleanup_preview_registration_is_explicit_and_does_not_run_factory(backend):
    def unexpected(*args,**kwargs):raise AssertionError('registration must not execute a task')
    pool=SimpleNamespace(acquire=unexpected)
    operations=background_cleanup_preview_operations(object(),installation_factory=unexpected,pool=pool,backend=backend)
    assert len(operations)==1
    assert operations[0].name=='semantic.preview_background_cleanup'
    assert operations[0].resource_class=='gpu'


@pytest.mark.parametrize('factory,pool,backend',[
    (None,SimpleNamespace(acquire=lambda:None),'cuda'),
    (object(),SimpleNamespace(acquire=lambda:None),'mps'),
    (lambda:None,object(),'cpu'),
    (lambda:None,SimpleNamespace(acquire=None),'cuda'),
    (lambda:None,SimpleNamespace(acquire=lambda:None),'metal'),
])
def test_cleanup_preview_registration_rejects_incomplete_installation(factory,pool,backend):
    with pytest.raises(ValueError,match='SEMANTIC_PREVIEW_INSTALLATION_REQUIRED'):
        background_cleanup_preview_operations(object(),installation_factory=factory,pool=pool,backend=backend)
