"""Direct detector evidence cannot run after an activity callback denies it."""
import pytest

from photo_worker.operations.optical.perspective_line_evidence import prepare_detected_perspective_lines
from photo_worker.runtime.executor import TaskCancelled


def test_detector_evidence_false_activity_prevents_source_decode():
    with pytest.raises(TaskCancelled):
        prepare_detected_perspective_lines(b'not-json', 'a'*64, object(), None,
                                           working_icc_sha='b'*64, check_active=lambda: False)
