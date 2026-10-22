from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.semantic_preview import RemovalPreviewPresentation
from photo_worker.operations.semantic.preview_presentation_stage import stage_removal_preview_presentation
from photo_worker.operations.semantic.preview_media_stage import StagedPreviewMedia
from test_v27_removal_preview_evidence import evidence


def test_removal_presentation_stages_exact_evidence_and_distinct_media():
    raw = canonical_bytes(evidence())
    ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    def fresh():
        return ArtifactRef(artifact_id=uuid4(), sha256='a'*64)
    media = StagedPreviewMedia(fresh(), fresh(), fresh(), fresh(), 100)
    saved = []
    class Artifacts:
        def stage_contract(self, value, role):
            assert role == 'preview_presentation'
            saved.append(value)
            return ArtifactRef(artifact_id=uuid4(), sha256=sha256(canonical_bytes(value)).hexdigest())
    result, size = stage_removal_preview_presentation(Artifacts(), evidence_ref=ref,
        evidence_raw=raw, media=media, width=8, height=8,
        icc_lock_sha='b'*64, srgb_sha='c'*64, max_output_bytes=1048576,
        verify_admission=lambda: True, check_active=lambda: None)
    assert isinstance(saved[0], RemovalPreviewPresentation)
    assert saved[0].params.kind == 'remove_object' and saved[0].numerical_evidence == ref
    assert result.sha256 == sha256(canonical_bytes(saved[0])).hexdigest()
    assert size == 100 + len(canonical_bytes(saved[0]))
    body = saved[0].model_dump(mode='json'); body['after'] = body['before']
    with pytest.raises(ValueError, match='PRESENTATION_ALIAS'):
        RemovalPreviewPresentation.model_validate_json(canonical_bytes(body))


@pytest.mark.parametrize('failure', ['noncanonical', 'evidence_sha', 'media_alias',
    'media_input_alias', 'output_alias', 'output_sha', 'mutated_document',
    'budget', 'denied', 'late_denied'])
def test_removal_presentation_rejects_changed_linkage_and_late_authority(failure):
    value = evidence()
    raw = canonical_bytes(value)
    if failure == 'noncanonical':
        raw += b' '
    ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    if failure == 'evidence_sha':
        ref = ref.model_copy(update={'sha256': '0'*64})
    def fresh():
        return ArtifactRef(artifact_id=uuid4(), sha256='a'*64)
    before, after, affected, protected = fresh(), fresh(), fresh(), fresh()
    if failure == 'media_alias':
        after = before
    if failure == 'media_input_alias':
        before = ArtifactRef.model_validate_json(canonical_bytes(value['params']['source']['source']))
    media = StagedPreviewMedia(before, after, affected, protected, 100)
    saved = []
    allowed = failure != 'denied'

    class Artifacts:
        def stage_contract(self, document, role):
            nonlocal allowed
            saved.append(role)
            digest = sha256(canonical_bytes(document)).hexdigest()
            if failure == 'mutated_document':
                document.width = 7
            if failure == 'late_denied':
                allowed = False
            return ArtifactRef(
                artifact_id=ref.artifact_id if failure == 'output_alias' else uuid4(),
                sha256='0'*64 if failure == 'output_sha' else digest)

    with pytest.raises(ValueError):
        stage_removal_preview_presentation(Artifacts(), evidence_ref=ref,
            evidence_raw=raw, media=media, width=8, height=8,
            icc_lock_sha='b'*64, srgb_sha='c'*64,
            max_output_bytes=100 if failure == 'budget' else 1048576,
            verify_admission=lambda: allowed, check_active=lambda: None)
    if failure in ('noncanonical', 'evidence_sha', 'media_alias',
            'media_input_alias', 'budget', 'denied'):
        assert saved == []
    else:
        assert saved == ['preview_presentation']
