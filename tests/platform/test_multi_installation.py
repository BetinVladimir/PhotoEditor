from types import SimpleNamespace
import pytest
from photo_worker.operations.optical.multi_installation import multi_operations,MultiImageInstallation


def test_missing_options_refused_before_database_io():
    class NoDatabase:
        def multi_qualification_evidence(self,*args):raise AssertionError('unexpected IO')
    with pytest.raises(ValueError,match='INSTALLATION_INVALID'):
        multi_operations(SimpleNamespace(),NoDatabase(),MultiImageInstallation(None,'photos',None),verify_color=lambda _:True)


def test_missing_dedicated_authority_cannot_use_generic_artifact_lookup():
    database=SimpleNamespace(artifact_locator=lambda *args:None)
    with pytest.raises(ValueError,match='INSTALLATION_INVALID'):
        multi_operations(SimpleNamespace(),database,MultiImageInstallation(None,'photos',None),verify_color=lambda _:True)
