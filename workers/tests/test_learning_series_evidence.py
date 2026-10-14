from hashlib import sha256
from uuid import uuid4, uuid5

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.series import (ItemCorrection, SeriesCorrections,
                                           SeriesParams)
from photo_worker.operations.learning.series_evidence import (
    FrozenSeriesHoldoutEvidence, decode_frozen_series_holdout_evidence,
)
from photo_worker.operations.learning.contracts import RenderCase
from photo_worker.operations.learning.fit import validate_fit_series_inputs
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


def test_fit_series_inputs_require_exact_manifest_binding_and_contracts():
    evidence = _fixture()
    training_run = uuid4()
    raw = canonical_bytes(evidence)
    artifact = ArtifactRef(artifact_id=uuid5(training_run,
        f'v23-series/{evidence.event_id}'), sha256=sha256(raw).hexdigest())
    def ref(char):
        return ArtifactRef(artifact_id=uuid4(), sha256=char * 64)
    case = RenderCase(
        series_evidence=artifact,
        binding=VersionBinding(asset_version_id=evidence.asset_version_id,
            source=evidence.source_image, canvas_hash='f' * 64),
        working_image=evidence.source_image, base_recipe=ref('a'),
        approved_recipe=ref('b'), base_document=ref('c'),
        approved_document=ref('d'), descriptor_shard=ref('1'),
        descriptor_analysis_id=uuid4(), descriptor_ordinal=0,
        protected=ref('2'), faces=ref('3'), approval_id=uuid4(),
        source_lineage_sha='4' * 64)
    declared = {
        artifact.artifact_id: 'FrozenSeriesHoldoutEvidence',
        evidence.source_image.artifact_id: 'WorkingImage',
        evidence.stage_image.artifact_id: 'WorkingImage',
        evidence.params.artifact_id: 'SeriesParams',
        evidence.series.artifact_id: 'SeriesCorrections',
    }
    reference_item = uuid4()
    params = SeriesParams(scene_id='studio', stats=[ref('6'), ref('7')],
                          reference_item_id=reference_item)
    def correction(item_id):
        return ItemCorrection(item_id=item_id, raw_ev=0, raw_gains=[1, 1, 1],
            applied_ev=0, applied_gains=[1, 1, 1], confidence=1,
            accepted=True, reason='reference')
    series = SeriesCorrections(scene_id='studio',
        reference_item_id=reference_item, common_region_ids=[],
        items=[correction(reference_item), correction(evidence.item_id)],
        warnings=[])

    class Artifacts:
        def contract_bytes(self, value, model):
            assert value == artifact and model is FrozenSeriesHoldoutEvidence
            return raw

        def require_declared_contract(self, value, contract):
            if declared.get(value.artifact_id) != contract:
                raise ValueError('TASK_INPUT_CONTRACT_MISMATCH')

        def contract(self, value, model):
            if value == evidence.params and model is SeriesParams:
                return params
            if value == evidence.series and model is SeriesCorrections:
                return series
            raise ValueError('TASK_INPUT_CONTRACT_MISMATCH')

    validate_fit_series_inputs(Artifacts(), case, evidence.event_id,
                               training_run, evidence.tenant_id)
    declared[evidence.params.artifact_id] = 'Params'
    with pytest.raises(ValueError, match='TASK_INPUT_CONTRACT_MISMATCH'):
        validate_fit_series_inputs(Artifacts(), case, evidence.event_id,
                                   training_run, evidence.tenant_id)
    declared[evidence.params.artifact_id] = 'SeriesParams'
    with pytest.raises(ValueError, match='INVALID_HOLDOUT_SERIES_EVIDENCE'):
        validate_fit_series_inputs(Artifacts(), case, uuid4(), training_run,
                                   evidence.tenant_id)
    with pytest.raises(ValueError, match='INVALID_HOLDOUT_SERIES_EVIDENCE'):
        validate_fit_series_inputs(Artifacts(), case, evidence.event_id,
                                   uuid4(), evidence.tenant_id)
    series = series.model_copy(update={'items': [
        correction(reference_item), correction(uuid4())]})
    with pytest.raises(ValueError, match='INVALID_HOLDOUT_SERIES_MATERIAL'):
        validate_fit_series_inputs(Artifacts(), case, evidence.event_id,
                                   training_run, evidence.tenant_id)
