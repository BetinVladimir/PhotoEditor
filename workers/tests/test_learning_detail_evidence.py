"""Frozen detail evidence refuses changed producer and mask inventories."""
from hashlib import sha256
from uuid import UUID, uuid5

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.operations.learning.detail_evidence import (
    FrozenPortraitDetailHoldoutEvidence, PortraitDetailHoldoutExpected,
    decode_frozen_detail_evidence,
    validate_holdout_detail_binding,
)
from photo_worker.operations.learning.contracts import RenderCase
from photo_worker.operations.learning.fit import validate_fit_detail_inputs
from photo_worker.operations.session.contracts import VersionBinding


def _id(number):
    return UUID(int=number)


def _ref(number):
    return ArtifactRef(artifact_id=_id(number), sha256=f'{number:064x}')


def _evidence():
    return FrozenPortraitDetailHoldoutEvidence(
        schema_version=1, tenant_id=_id(1), event_id=_id(2),
        asset_version_id=_id(3), run_id=_id(4), task_id=_id(5),
        result_id=_id(6), attempt_id=_id(7), source=_ref(16), image=_ref(8),
        protection=_ref(9), params=_ref(10), resolved=_ref(11),
        mask_artifacts=[_ref(12), _ref(13)])


def test_frozen_detail_document_binds_exact_producer_and_original_bytes():
    evidence = _evidence()
    raw = canonical_bytes(evidence)
    artifact = ArtifactRef(artifact_id=_id(14), sha256=sha256(raw).hexdigest())
    expected = PortraitDetailHoldoutExpected(**evidence.model_dump(
        include=set(PortraitDetailHoldoutExpected.model_fields)))
    assert decode_frozen_detail_evidence(raw, artifact, expected) == evidence
    with pytest.raises(ValueError, match='STALE_FROZEN_DETAIL_HOLDOUT'):
        decode_frozen_detail_evidence(raw, artifact,
            expected.model_copy(update={'result_id': _id(15)}))
    with pytest.raises(ValueError, match='STALE_FROZEN_DETAIL_HOLDOUT'):
        decode_frozen_detail_evidence(raw, artifact,
            expected.model_copy(update={'source': _ref(17)}))
    with pytest.raises(ValueError, match='STALE_FROZEN_DETAIL_HOLDOUT'):
        decode_frozen_detail_evidence(raw, artifact,
            expected.model_copy(update={'mask_artifacts': [_ref(12), _ref(15)]}))
    with pytest.raises(ValueError, match='INVALID_FROZEN_DETAIL_HOLDOUT'):
        decode_frozen_detail_evidence(raw + b' ', artifact, expected)


def test_frozen_detail_document_refuses_aliased_or_reordered_masks():
    evidence = _evidence()
    with pytest.raises(ValueError, match='INVALID_DETAIL_HOLDOUT_IDENTITY'):
        FrozenPortraitDetailHoldoutEvidence.model_validate(
            {**evidence.model_dump(), 'mask_artifacts': [_ref(13), _ref(12)]})
    with pytest.raises(ValueError, match='INVALID_DETAIL_HOLDOUT_IDENTITY'):
        FrozenPortraitDetailHoldoutEvidence.model_validate(
            {**evidence.model_dump(), 'mask_artifacts': [_ref(12), evidence.image]})


def test_detail_binding_requires_training_run_event_and_original_source():
    evidence = _evidence()
    training_run = _id(30)
    raw = canonical_bytes(evidence)
    artifact = ArtifactRef(artifact_id=uuid5(training_run,
        f'v23-detail/{evidence.event_id}'), sha256=sha256(raw).hexdigest())
    case = RenderCase(
        detail_evidence=artifact,
        binding=VersionBinding(asset_version_id=evidence.asset_version_id,
            source=evidence.source, canvas_hash='a' * 64),
        working_image=evidence.source,
        base_recipe=_ref(18), approved_recipe=_ref(19),
        base_document=_ref(20), approved_document=_ref(21),
        descriptor_shard=_ref(22), descriptor_analysis_id=_id(23),
        descriptor_ordinal=0, protected=evidence.protection, faces=_ref(24),
        approval_id=_id(25), source_lineage_sha='b' * 64,
    )
    validate_holdout_detail_binding(case, evidence.event_id, training_run,
                                    evidence.tenant_id, artifact, evidence)
    with pytest.raises(ValueError, match='HOLDOUT_DETAIL_BINDING_MISMATCH'):
        validate_holdout_detail_binding(case, evidence.event_id, _id(31),
                                        evidence.tenant_id, artifact, evidence)
    with pytest.raises(ValueError, match='HOLDOUT_DETAIL_BINDING_MISMATCH'):
        validate_holdout_detail_binding(case, evidence.event_id, training_run,
                                        _id(32), artifact, evidence)


def test_fit_rejects_wrong_nested_detail_contract_before_model_fit():
    evidence = _evidence()
    training_run = _id(30)
    raw = canonical_bytes(evidence)
    artifact = ArtifactRef(artifact_id=uuid5(training_run,
        f'v23-detail/{evidence.event_id}'), sha256=sha256(raw).hexdigest())
    case = RenderCase(detail_evidence=artifact,
        binding=VersionBinding(asset_version_id=evidence.asset_version_id,
            source=evidence.source, canvas_hash='a'*64),
        working_image=evidence.source, base_recipe=_ref(18),
        approved_recipe=_ref(19), base_document=_ref(20),
        approved_document=_ref(21), descriptor_shard=_ref(22),
        descriptor_analysis_id=_id(23), descriptor_ordinal=0,
        protected=evidence.protection, faces=_ref(24), approval_id=_id(25),
        source_lineage_sha='b'*64)
    declared = {artifact.artifact_id: 'FrozenPortraitDetailHoldoutEvidence',
        evidence.source.artifact_id: 'WorkingImage',
        evidence.image.artifact_id: 'WorkingImage',
        evidence.protection.artifact_id: 'MaskRef',
        evidence.params.artifact_id: 'PortraitParams',
        evidence.resolved.artifact_id: 'ResolvedStyle',
        **{ref.artifact_id: 'NpyArray' for ref in evidence.mask_artifacts}}

    class Artifacts:
        def contract_bytes(self, ref, model):
            assert ref == artifact and model is FrozenPortraitDetailHoldoutEvidence
            return raw

        def require_declared_contract(self, ref, contract):
            if declared.get(ref.artifact_id) != contract:
                raise ValueError('TASK_INPUT_CONTRACT_MISMATCH')

    validate_fit_detail_inputs(Artifacts(), case, evidence.event_id,
                               training_run, evidence.tenant_id)
    declared[evidence.params.artifact_id] = 'Params'
    with pytest.raises(ValueError, match='TASK_INPUT_CONTRACT_MISMATCH'):
        validate_fit_detail_inputs(Artifacts(), case, evidence.event_id,
                                   training_run, evidence.tenant_id)
