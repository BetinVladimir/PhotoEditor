"""Malformed scheduler profiles fail as configuration errors before object IO."""
from types import SimpleNamespace

import pytest

from photo_worker.operations.optical.perspective_line_detection import execute
from photo_worker.runtime.executor import OperationFailure


@pytest.mark.parametrize('profile', [None, [], 'cpu', 1])
def test_detector_rejects_non_mapping_execution_profile(profile):
    class Artifacts:
        def params(self, _contract):
            raise AssertionError('malformed profile reached input objects')
    ctx = SimpleNamespace(check_active=lambda: None,
                          lease=SimpleNamespace(execution_profile=profile))
    with pytest.raises(OperationFailure, match='CONFIG_INVALID'):
        execute(ctx, Artifacts(), working_icc_sha='a'*64)
