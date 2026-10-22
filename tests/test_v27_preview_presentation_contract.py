from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticExposurePreviewInput,ExposurePreviewPresentation
from test_v27_exposure_preview_binding import fixture


def document():
    params,_,_=fixture()
    ref=lambda:ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    return ExposurePreviewPresentation(schema_version=1,
        params=SemanticExposurePreviewInput.model_validate_json(canonical_bytes(params)),
        numerical_evidence=ref(),working_image=ref(),pixels=ref(),before=ref(),after=ref(),
        affected=ref(),protected=ref(),width=2,height=1,icc_lock_sha='b'*64,srgb_sha='c'*64,
        resize='area_linear_premult_v1',overlay='max_coverage_ceil_alpha_v1')


def test_presentation_roundtrip_requires_complete_distinct_media():
    value=document()
    assert ExposurePreviewPresentation.model_validate_json(canonical_bytes(value))==value
    for name in ('numerical_evidence','working_image','pixels','before','after','affected','protected'):
        body=value.model_dump(mode='json');body[name]=body['params']['candidate']
        with pytest.raises(ValueError):ExposurePreviewPresentation.model_validate_json(canonical_bytes(body))


@pytest.mark.parametrize('dimension',[0,4097,True])
def test_presentation_dimensions_are_strict_bounded(dimension):
    body=document().model_dump(mode='json');body['width']=dimension
    with pytest.raises(ValueError):ExposurePreviewPresentation.model_validate_json(canonical_bytes(body))
