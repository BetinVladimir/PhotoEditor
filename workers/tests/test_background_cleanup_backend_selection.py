"""Registration admission for the already backend-aware background preview."""
from types import SimpleNamespace
import pytest
from photo_worker.runtime.service import (
    WorkerConfigurationError, _select_background_cleanup_preview_installation,
    _select_removal_preview_installation,
)


def pool(status='passed', backend='mps', *, approved=True, report='a'*64, installed=True):
    entry=SimpleNamespace(id='big_lama',approved_for_install=approved,
        backends={backend:SimpleNamespace(status=status,report_sha256=report)})
    return SimpleNamespace(lock=SimpleNamespace(models=[entry]),
        factories={'big_lama':object()} if installed else {})


@pytest.mark.parametrize('backend',['cpu','cuda','mps'])
def test_background_keeps_qualified_compatible_factory(backend):
    factory=lambda *args: None
    assert _select_background_cleanup_preview_installation(factory,pool(backend=backend),backend) is factory


def test_background_omits_only_explicitly_unsupported_metal_dependency():
    factory=lambda *args: None
    assert _select_background_cleanup_preview_installation(factory,pool('unsupported'),'mps') is None
    assert _select_removal_preview_installation(factory,pool(),'mps') is factory


@pytest.mark.parametrize('change',[{'status':'untested'},{'approved':False},{'report':None},{'installed':False}])
def test_background_misconfigured_installation_fails_instead_of_claiming_unavailable(change):
    with pytest.raises(WorkerConfigurationError):
        _select_background_cleanup_preview_installation(lambda *args:None,pool(**change),'mps')


def test_missing_backend_evidence_is_not_treated_as_unsupported():
    with pytest.raises(WorkerConfigurationError):
        _select_background_cleanup_preview_installation(lambda *args:None,pool(backend='cuda'),'mps')


def test_no_factory_never_advertises_background_preview():
    assert _select_background_cleanup_preview_installation(None,None,'mps') is None


def test_duplicate_model_lock_entry_is_rejected():
    installation=pool();installation.lock.models*=2
    with pytest.raises(WorkerConfigurationError):
        _select_background_cleanup_preview_installation(lambda *args:None,installation,'mps')
