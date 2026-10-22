"""Retouch evidence contracts describe preservation, not a quality PASS."""
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import RetouchPreviewNumericalEvidence,RetouchPreviewPresentation
from test_v27_retouch_preview_contract import retouch_params


def ref():return dict(artifact_id=str(uuid4()),sha256='a'*64)


def evidence():
    return dict(schema_version=1,params=retouch_params(),working_image=ref(),pixels=ref(),
        algorithm='beautyfilter_v1',changed_pixels=4,protected_pixels_unchanged=True,
        outside_mask_unchanged=True,alpha_unchanged=True,roi=[0,0,8,8])


def test_retouch_evidence_roundtrip_keeps_exact_operation():
    owned=RetouchPreviewNumericalEvidence.model_validate_json(canonical_bytes(evidence()))
    assert owned.params.kind=='retouch_region'
    assert RetouchPreviewNumericalEvidence.model_validate_json(canonical_bytes(owned))==owned


@pytest.mark.parametrize('field',['protected_pixels_unchanged','outside_mask_unchanged','alpha_unchanged'])
@pytest.mark.parametrize('value',[False,1,None])
def test_retouch_evidence_requires_literal_preservation(field,value):
    raw=evidence();raw[field]=value
    with pytest.raises(ValueError,match='PROTECTION_INVALID'):
        RetouchPreviewNumericalEvidence.model_validate_json(canonical_bytes(raw))


@pytest.mark.parametrize('failure',['algorithm','kind','alias','roi','count','approval','global_pass'])
def test_retouch_evidence_refuses_foreign_operation_and_invalid_binding(failure):
    raw=evidence()
    if failure=='algorithm':raw['algorithm']='big_lama_work_srgb_delta_v1'
    if failure=='kind':raw['params']['kind']='background_cleanup'
    if failure=='alias':raw['pixels']=raw['working_image']
    if failure=='roi':raw['roi']=[8,0,8,8]
    if failure=='count':raw['changed_pixels']=65
    if failure=='approval':raw['approval_id']=str(uuid4())
    if failure=='global_pass':raw['global_pass']=True
    with pytest.raises(ValueError):RetouchPreviewNumericalEvidence.model_validate_json(canonical_bytes(raw))


def presentation():
    return dict(schema_version=1,params=retouch_params(),numerical_evidence=ref(),working_image=ref(),pixels=ref(),
        before=ref(),after=ref(),affected=ref(),protected=ref(),width=2560,height=1,
        icc_lock_sha='b'*64,srgb_sha='c'*64,resize='area_linear_premult_v1',overlay='max_coverage_ceil_alpha_v1')


def test_retouch_presentation_roundtrip_and_media_alias_refusal():
    raw=presentation();owned=RetouchPreviewPresentation.model_validate_json(canonical_bytes(raw))
    assert RetouchPreviewPresentation.model_validate_json(canonical_bytes(owned))==owned
    raw['after']=raw['before']
    with pytest.raises(ValueError,match='PRESENTATION_ALIAS'):RetouchPreviewPresentation.model_validate_json(canonical_bytes(raw))


@pytest.mark.parametrize('edge',[0,True,2561])
def test_retouch_presentation_refuses_invalid_display_edge(edge):
    raw=presentation();raw['width']=edge
    with pytest.raises(ValueError):RetouchPreviewPresentation.model_validate_json(canonical_bytes(raw))


@pytest.mark.parametrize('name,model',[
    ('retouch_preview_evidence.json',RetouchPreviewNumericalEvidence),
    ('retouch_preview_presentation.json',RetouchPreviewPresentation),
])
def test_retouch_result_models_share_exact_go_wire(name,model):
    from pathlib import Path
    raw=(Path(__file__).resolve().parents[1]/'backend/internal/semanticreview/testdata'/name).read_bytes()
    value=model.model_validate_json(raw)
    assert value.params.kind=='retouch_region' and canonical_bytes(value)==raw
