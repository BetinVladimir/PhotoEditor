from hashlib import sha256
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.operations.learning.series_evidence import (
    FrozenSeriesHoldoutEvidence, decode_frozen_series_holdout_evidence,
)
from photo_worker.operations.learning.contracts import RenderCase
from photo_worker.operations.session.contracts import VersionBinding


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


def test_series_render_ref_is_distinct_and_exclusive():
    evidence = _fixture()
    def ref(char):
        return ArtifactRef(artifact_id=uuid4(), sha256=char * 64)
    render = RenderCase(
        series_evidence=ref('e'),
        binding=VersionBinding(asset_version_id=evidence.asset_version_id,
            source=evidence.source_image, canvas_hash='f' * 64),
        working_image=evidence.source_image, base_recipe=ref('a'),
        approved_recipe=ref('b'), base_document=ref('c'),
        approved_document=ref('d'), descriptor_shard=ref('1'),
        descriptor_analysis_id=uuid4(), descriptor_ordinal=0,
        protected=ref('2'), faces=ref('3'), approval_id=uuid4(),
        source_lineage_sha='4' * 64)
    assert render.series_evidence is not None
    with pytest.raises(ValueError, match='INVALID_HOLDOUT_SERIES_EVIDENCE'):
        RenderCase.model_validate(render.model_dump() | {
            'series_evidence': render.working_image.model_dump()})
    with pytest.raises(ValueError, match='INVALID_HOLDOUT_(DUST|SERIES)_EVIDENCE'):
        RenderCase.model_validate(render.model_dump() | {
            'dust_evidence': ref('5').model_dump()})
