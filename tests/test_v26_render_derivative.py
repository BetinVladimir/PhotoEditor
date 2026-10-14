from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.media import MediaParams
from photo_worker.operations.optical.contracts import ImageResult
from photo_worker.v2.render_derivative import compile_render_derivative
from test_v26_render_authored import fixture


def ref(raw):
    return ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())

@pytest.mark.parametrize('alpha',[False,True])
def test_render_result_preview_plan_preserves_exact_image_and_alpha(tmp_path,alpha):
    _,inputs,*_=fixture(tmp_path)
    image=inputs.source.model_copy(deep=True)
    image.alpha=ref(b'alpha') if alpha else None
    image_raw=canonical_bytes(image);image_ref=ref(image_raw)
    result=ImageResult(schema_version=1,working_image=image_ref,report=ref(b'report'),changed_mask=ref(b'mask'),generated_mask=None)
    raw=canonical_bytes(result);root=ref(raw)
    plan=compile_render_derivative(root,raw,image_raw,'b'*64)
    params=MediaParams.model_validate_json(plan.params_bytes)
    assert params.image==image_ref and params.variant=='preview1600'
    assert params.source_kind=='working' and params.suffix is None
    assert plan.inputs==(image_ref,image.pixels,*((image.alpha,) if alpha else ()))
    assert plan.params_sha256==sha256(plan.params_bytes).hexdigest()
    for wrong in [dict(result_bytes=raw+b' '),dict(image_bytes=image_raw+b' '),dict(srgb_sha256='B'*64),dict(variant='thumb320')]:
        args=dict(result_ref=root,result_bytes=raw,image_bytes=image_raw,srgb_sha256='b'*64);args.update(wrong)
        with pytest.raises(ValueError):compile_render_derivative(**args)
    with pytest.raises(ValueError):compile_render_derivative(ref(b'other'),raw,image_raw,'b'*64)

    aliased=root.model_copy(update={'artifact_id':image_ref.artifact_id})
    with pytest.raises(ValueError):compile_render_derivative(aliased,raw,image_raw,'b'*64)
