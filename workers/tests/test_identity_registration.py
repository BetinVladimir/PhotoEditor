"""An installed model ID alone must not advertise identity.compare."""
from types import SimpleNamespace

from photo_worker.operations.identity.adapter import identity_compare_operations


def _pool(*, missing=None, unapproved=None, unqualified=None):
    names = ('sfd', 'fan2d', 'identity512')
    entries = [SimpleNamespace(id=name, approved_for_install=name != unapproved,
                backends={'cpu': SimpleNamespace(status='untested' if name == unqualified else 'passed'),
                          'cuda': SimpleNamespace(status='untested' if name == unqualified else 'passed'),
                          'mps': SimpleNamespace(status='passed')})
               for name in names if name != missing]
    return SimpleNamespace(lock=SimpleNamespace(models=entries))


def test_identity_registration_requires_every_qualified_model():
    assert len(identity_compare_operations(None, _pool(), 'cuda')) == 1
    assert len(identity_compare_operations(None, _pool(), 'cpu')) == 1
    for name in ('sfd', 'fan2d', 'identity512'):
        assert identity_compare_operations(None, _pool(missing=name), 'cuda') == []
        assert identity_compare_operations(None, _pool(unapproved=name), 'cuda') == []
        assert identity_compare_operations(None, _pool(unqualified=name), 'cuda') == []
    assert identity_compare_operations(None, _pool(), 'mps') == []
