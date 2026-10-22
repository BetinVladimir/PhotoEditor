from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.semantic_preview import RetouchPreviewPresentation
from photo_worker.operations.semantic.preview_presentation_stage import stage_retouch_preview_presentation
from photo_worker.operations.semantic.preview_media_stage import StagedPreviewMedia
from test_v27_retouch_preview_evidence import evidence


def test_retouch_presentation_stages_exact_evidence_and_distinct_media():
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
    result, size = stage_retouch_preview_presentation(Artifacts(), evidence_ref=ref,
        evidence_raw=raw, media=media, width=8, height=8,
        icc_lock_sha='b'*64, srgb_sha='c'*64, max_output_bytes=1048576,
        verify_admission=lambda: True, check_active=lambda: None)
    assert isinstance(saved[0], RetouchPreviewPresentation)
    assert saved[0].params.kind == 'retouch_region' and saved[0].numerical_evidence == ref
    assert result.sha256 == sha256(canonical_bytes(saved[0])).hexdigest()
    assert size == 100 + len(canonical_bytes(saved[0]))
    body = saved[0].model_dump(mode='json'); body['after'] = body['before']
    with pytest.raises(ValueError, match='PRESENTATION_ALIAS'):
        RetouchPreviewPresentation.model_validate_json(canonical_bytes(body))


@pytest.mark.parametrize('field,value',[('width',0),('height',2561),('width',True),('height',1.5)])
def test_retouch_presentation_rejects_invalid_display_bounds(field,value):
    data=evidence()
    fresh=lambda:dict(artifact_id=str(uuid4()),sha256='a'*64)
    presentation=dict(schema_version=1,params=data['params'],numerical_evidence=fresh(),
        working_image=data['working_image'],pixels=data['pixels'],before=fresh(),after=fresh(),
        affected=fresh(),protected=fresh(),width=8,height=8,icc_lock_sha='b'*64,srgb_sha='c'*64,
        resize='area_linear_premult_v1',overlay='max_coverage_ceil_alpha_v1')
    presentation[field]=value
    with pytest.raises(ValueError):RetouchPreviewPresentation.model_validate_json(canonical_bytes(presentation))


def test_retouch_presentation_refuses_noncanonical_evidence_before_stage():
    import json
    raw = json.dumps(evidence(), indent=2).encode()
    ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    def fresh():return ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    media = StagedPreviewMedia(fresh(),fresh(),fresh(),fresh(),100)
    class Artifacts:
        def stage_contract(self,*a):raise AssertionError('noncanonical evidence staged')
    with pytest.raises(ValueError,match='PRESENTATION_INVALID'):
        stage_retouch_preview_presentation(Artifacts(),evidence_ref=ref,evidence_raw=raw,media=media,
            width=8,height=8,icc_lock_sha='b'*64,srgb_sha='c'*64,max_output_bytes=1048576,
            verify_admission=lambda:True,check_active=lambda:None)
