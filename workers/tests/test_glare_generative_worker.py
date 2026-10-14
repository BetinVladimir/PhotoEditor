"""V28 generative glare worker requires model admission and eight fresh outputs."""

from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.portrait.glare_generative_worker import _handoff, execute_generative
from photo_worker.operations.portrait.local_result import StagedLocalPortrait
from photo_worker.runtime.executor import OperationFailure


def ref():
    return ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)


def test_generative_worker_denies_uninstalled_model_before_artifact_io():
    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile={}))
    with pytest.raises(OperationFailure, match='GLARE_GENERATIVE_MODEL_UNAVAILABLE'):
        execute_generative(ctx, object(), object(), object())


def test_generative_handoff_requires_eight_distinct_new_outputs():
    outputs = tuple(ref() for _ in range(8))
    staged = StagedLocalPortrait(outputs[-1], outputs, True, 400)
    result = _handoff(staged, (ref(),), 400, lambda: None)
    assert result.review_required and result.outputs == [outputs[-1]]
    with pytest.raises(OperationFailure, match='GLARE_GENERATIVE_RESULT_INVALID'):
        _handoff(staged, (outputs[0],), 400, lambda: None)
