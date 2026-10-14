"""Reviewed repair admission requires one identical effect for all arms."""
from types import SimpleNamespace

import pytest

from photo_worker.operations.learning.pipeline_admission import validate_holdout_pipeline


def _styles(*, evidence, changed=False, restoration=None):
    defect = SimpleNamespace(enabled=True, algorithm='defectrepair_telea_v1',
                             strength=.5)
    extension = SimpleNamespace(culling=None, series=None, composition=None,
        portrait_detail=None, inpainting=None, generation=None,
        restoration=restoration, background=None)
    styles = [SimpleNamespace(defects=defect, geometry=SimpleNamespace(enabled=False),
        extensions=extension, strength=.7) for _ in range(3)]
    if changed:
        styles[1].defects = SimpleNamespace(enabled=True,
            algorithm='defectrepair_telea_v1', strength=.6)
    return SimpleNamespace(baseline=styles[0], candidate=styles[1],
                           approved=styles[2]), evidence


def test_reviewed_defect_pipeline_requires_same_effect_and_evidence():
    styles, _ = _styles(evidence=True)
    validate_holdout_pipeline(styles, reviewed_defect=True)
    with pytest.raises(ValueError, match='HOLDOUT_DEFECT_EVIDENCE_REQUIRED'):
        validate_holdout_pipeline(styles)
    changed, _ = _styles(evidence=True, changed=True)
    with pytest.raises(ValueError, match='HOLDOUT_DEFECT_REPLAY_BINDING_MISMATCH'):
        validate_holdout_pipeline(changed, reviewed_defect=True)
    restoration, _ = _styles(evidence=True,
        restoration=SimpleNamespace(upscale='off', sensor_dust='off'))
    with pytest.raises(ValueError, match='HOLDOUT_DEFECT_REPLAY_BINDING_MISMATCH'):
        validate_holdout_pipeline(restoration, reviewed_defect=True)
