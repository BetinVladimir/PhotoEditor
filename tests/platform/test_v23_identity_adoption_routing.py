"""identity.compare is owned by the Go physical-verification adopter."""

from types import SimpleNamespace
from uuid import UUID

from photo_worker.v2.adoption_service import AdoptionCoordinator


def test_python_coordinator_does_not_enter_generic_identity_adoption():
    coordinator = object.__new__(AdoptionCoordinator)
    coordinator.check_active = lambda: None
    coordinator.signing_operator = None
    coordinator.perspective_lines_installation = None
    coordinator.context = lambda tenant, task: SimpleNamespace(operation='identity.compare')
    assert coordinator.adopt(UUID(int=1), UUID(int=2)) is False
