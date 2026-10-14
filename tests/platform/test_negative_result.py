from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash,ArtifactRef
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.optical.negative_result import build_negative_metadata
from test_portrait_donor_manual import manual_case
from test_optical_negative_params import intent


def source_case():
    args,_=manual_case();source=WorkingImage.model_validate_json(args[1])
    from photo_worker.contracts.images import ColorSpace
    source.color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64)
    source.alpha=ArtifactRef(artifact_id=uuid4(),sha256='d'*64)
    raw=canonical_bytes(source);p=intent();p['base']['source']['sha256']=sha256(raw).hexdigest()
    p['base']['canvas_hash']=canonical_hash(source.canvas)
    return p,source,raw


@pytest.mark.parametrize('strength',[0,1])
def test_negative_metadata_always_reviews_and_preserves_geometry_alpha(strength):
    p,source,raw=source_case();p['strength']=strength
    pixels=ArtifactRef(artifact_id=uuid4(),sha256='e'*64)
    output,report=build_negative_metadata(p,raw,pixels,clipping_fraction=0.1)
    assert output.pixels==pixels and output.alpha==source.alpha
    assert output.canvas==source.canvas and output.transform_chain==source.transform_chain and output.color==source.color
    assert report.status=='review' and report.reason=='negative_review' and report.operation=='optical.negative'
    output.alpha.sha256='f'*64
    assert source.alpha.sha256=='d'*64


@pytest.mark.parametrize('mode',['hash','canvas','clipping'])
def test_negative_result_rejects_source_or_statistics_mismatch(mode):
    p,source,raw=source_case();fraction=0
    if mode=='hash':raw+=b' '
    if mode=='canvas':p['base']['canvas_hash']='0'*64
    if mode=='clipping':fraction=float('nan')
    with pytest.raises(ValueError):build_negative_metadata(p,raw,ArtifactRef(artifact_id=uuid4(),sha256='e'*64),clipping_fraction=fraction)


@pytest.mark.parametrize('kind',['alpha','source_pixels','document','calibration'])
def test_negative_rgb_output_cannot_alias_input_roles(kind):
    p,source,raw=source_case()
    pixels=source.alpha if kind=='alpha' else source.pixels if kind=='source_pixels' else \
        ArtifactRef.model_validate_json(canonical_bytes(p['base']['source'] if kind=='document' else p['calibration']))
    with pytest.raises(ValueError,match='NEGATIVE_RESULT_INVALID'):
        build_negative_metadata(p,raw,pixels,clipping_fraction=0)


def test_zero_strength_reuses_only_exact_original_pixels_pin():
    p,source,raw=source_case();p['strength']=0
    output,report=build_negative_metadata(p,raw,source.pixels,clipping_fraction=0)
    assert output==source and report.status=='review'
    changed=source.pixels.model_copy(deep=True);changed.sha256='f'*64
    with pytest.raises(ValueError,match='NEGATIVE_RESULT_INVALID'):
        build_negative_metadata(p,raw,changed,clipping_fraction=0)
