"""The direct detector API honors an explicit revoked activity callback."""
import pytest

from photo_worker.algorithms.perspective_lines import detect_perspective_lines
from photo_worker.runtime.executor import TaskCancelled


def test_direct_detector_false_activity_prevents_proxy_creation():
    with pytest.raises(TaskCancelled):
        detect_perspective_lines(object(), check_active=lambda: False)
