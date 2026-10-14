from pathlib import Path
import pytest
from photo_worker.v2.perspective_lines_adoption_prepare import (
    PerspectiveLinesAdoptionInstallation,validate_perspective_lines_adoption_installation)
from photo_worker.operations.optical.perspective_lines_installation import PerspectiveLinesInstallation


def test_adoption_installation_refuses_invalid_budgets_and_scratch_before_runtime(tmp_path):
    runtime=PerspectiveLinesInstallation({},'a'*64,'b'*64)
    for scratch,maximum,ram in [(tmp_path,0,1),(tmp_path,1,True),(Path('relative'),1,1),
        (tmp_path/'absent',1,1),(tmp_path,2**53,1)]:
        with pytest.raises(ValueError):
            validate_perspective_lines_adoption_installation(PerspectiveLinesAdoptionInstallation(runtime,scratch,maximum,ram))
    with pytest.raises(ValueError):validate_perspective_lines_adoption_installation(None)
