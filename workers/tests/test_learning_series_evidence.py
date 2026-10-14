from hashlib import sha256
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.operations.learning.series_evidence import (
    FrozenSeriesHoldoutEvidence, decode_frozen_series_holdout_evidence,
)


def _fixture():
    def ref(char):
        return ArtifactRef(artifact_id=uuid4(), sha256=char * 64)
    return FrozenSeriesHoldoutEvidence(
        schema_version=1, tenant_id=uuid4(), event_id=uuid4(),
        asset_version_id=uuid4(), apply_run_id=uuid4(), item_id=uuid4(),
        producer_task_id=uuid4(), source_image=ref('a'), stage_image=ref('b'),
        params=ref('c'), series=ref('d'))


def test_frozen_series_evidence_requires_exact_canonical_sql_expected_identity():
    evidence = _fixture()
    raw = canonical_bytes(evidence)
    artifact = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    assert decode_frozen_series_holdout_evidence(raw, artifact, evidence) == evidence

    with pytest.raises(ValueError):
        decode_frozen_series_holdout_evidence(
            raw, artifact, evidence.model_copy(update={'item_id': uuid4()}))
    with pytest.raises(ValueError):
        decode_frozen_series_holdout_evidence(
            raw, artifact.model_copy(update={'sha256': 'e' * 64}), evidence)
    with pytest.raises(ValueError):
        FrozenSeriesHoldoutEvidence.model_validate(evidence.model_dump() | {
            'series': evidence.params.model_dump()})
