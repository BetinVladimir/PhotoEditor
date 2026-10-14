"""Frozen V23 defect evidence is structural proof, not SQL review authority."""
import hashlib
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.defects import DefectReview
from photo_worker.operations.learning.defect_evidence import (
    DefectHoldoutExpected, FrozenDefectHoldoutEvidence,
    decode_frozen_defect_holdout_evidence, validate_holdout_defect_binding,
)
from photo_worker.operations.learning.contracts import RenderCase
from photo_worker.operations.session.contracts import VersionBinding


def _fixture():
    def ref(char):
        return ArtifactRef(artifact_id=uuid4(), sha256=char * 64)

    image, protection, params, proposal, labels = (
        ref(char) for char in 'abcde')
    selection = DefectReview(proposal=proposal, image=image, label_map=labels,
                             revision=2, accepted_candidate_ids=['f' * 64],
                             rejected_candidate_ids=[])
    tenant, event, asset_version, review, run, actor = (uuid4() for _ in range(6))
    decision_hash = canonical_hash(dict(version='defect_decision_v1',
        tenant_id=str(tenant), review_id=str(review), run_id=str(run),
        actor_id=str(actor), decision='accept',
        selection=selection.model_dump(mode='json')))
    frozen = FrozenDefectHoldoutEvidence(schema_version=1, tenant_id=tenant,
        event_id=event, asset_version_id=asset_version,
        review_id=review, run_id=run, actor_id=actor, decision='accept',
        route='repair', decision_hash=decision_hash, revision=2, image=image,
        protection=protection, params=params, selection=selection)
    raw = canonical_bytes(frozen)
    artifact = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(raw).hexdigest())
    expected = DefectHoldoutExpected(tenant_id=tenant, event_id=event,
        asset_version_id=asset_version, review_id=review,
        run_id=run, actor_id=actor, decision_hash=decision_hash,
        image=image, protection=protection, params=params)
    return raw, artifact, expected


def test_frozen_defect_evidence_exact_bytes_and_scope():
    raw, artifact, expected = _fixture()
    value = decode_frozen_defect_holdout_evidence(raw, artifact, expected)
    assert value.route == 'repair' and value.selection.accepted_candidate_ids == ['f' * 64]
    for altered, pin, scope in (
        (raw + b' ', artifact, expected),
        (raw, artifact.model_copy(update={'sha256': '0' * 64}), expected),
        (raw, artifact, expected.model_copy(update={'review_id': uuid4()})),
        (raw, artifact, expected.model_copy(update={'event_id': uuid4()})),
        (raw, artifact, expected.model_copy(update={'asset_version_id': uuid4()})),
        (raw, artifact, expected.model_copy(update={'image': ArtifactRef(
            artifact_id=uuid4(), sha256=expected.image.sha256)})),
    ):
        with pytest.raises(ValueError):
            decode_frozen_defect_holdout_evidence(altered, pin, scope)


def test_frozen_defect_evidence_rejects_duplicate_and_changed_decision():
    raw, _, expected = _fixture()
    duplicate = raw.replace(b'"revision":2', b'"revision":2,"revision":2', 1)
    changed = raw.replace(b'"decision":"accept"', b'"decision":"reject"', 1)
    for altered in (duplicate, changed):
        assert altered != raw
        pin = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(altered).hexdigest())
        with pytest.raises(ValueError):
            decode_frozen_defect_holdout_evidence(altered, pin, expected)


def test_frozen_defect_evidence_binds_exact_holdout_case():
    raw, artifact, expected = _fixture()
    evidence = decode_frozen_defect_holdout_evidence(raw, artifact, expected)
    case = RenderCase.model_construct(defect_evidence=artifact,
        binding=VersionBinding.model_construct(asset_version_id=expected.asset_version_id),
        working_image=expected.image, protected=expected.protection)
    validate_holdout_defect_binding(case, expected.event_id, evidence)
    with pytest.raises(ValueError):
        validate_holdout_defect_binding(case, uuid4(), evidence)
    for changed in (
        case.model_copy(update={'protected': ArtifactRef(
            artifact_id=uuid4(), sha256=expected.protection.sha256)}),
        case.model_copy(update={'working_image': ArtifactRef(
            artifact_id=uuid4(), sha256=expected.image.sha256)}),
        case.model_copy(update={'binding': VersionBinding.model_construct(
            asset_version_id=uuid4())}),
    ):
        with pytest.raises(ValueError):
            validate_holdout_defect_binding(changed, expected.event_id, evidence)
