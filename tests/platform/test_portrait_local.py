import numpy as np
import pytest

from photo_worker.algorithms.portrait_local import rotate_chroma, hair_proposal, clothing_proposal, compose_local

LUMA = np.array([.2126, .7152, .0722])


def test_chroma_rotation_retains_luminance_and_hdr_without_negative_channels():
    source = np.array([[[4., .1, .2], [.1, 2., .01], [0., 0., 0.]]], np.float32)
    for hue in (-180, -90, 0, 90, 180):
        result = rotate_chroma(source, hue, 2)
        np.testing.assert_allclose(result @ LUMA, source @ LUMA, rtol=2e-7, atol=1e-7)
        assert np.all(result >= 0)
    assert rotate_chroma(np.full((2,2,3), 4., np.float32), 90, 2).max() == 4


def test_neutral_hair_and_clothing_preserve_linear_image():
    source = np.random.default_rng(42).uniform(.1, 3, (12,17,3)).astype(np.float32)
    np.testing.assert_allclose(hair_proposal(source, 0, 1, 0), source, atol=1e-6)
    np.testing.assert_allclose(clothing_proposal(source, 0, 1, 0), source, atol=1e-6)


def test_clothing_gain_is_bounded_and_hair_detail_nonnegative():
    source = np.random.default_rng(7).uniform(.01, 4, (13,19,3)).astype(np.float32)
    y = source @ LUMA
    result = clothing_proposal(source, 0, 1, 1)
    gain = (result @ LUMA) / y
    assert np.all(gain >= .8 - 1e-6) and np.all(gain <= 1.25 + 1e-6)
    assert np.all(hair_proposal(source, 180, 2, 1) >= 0)


def test_local_compose_preserves_protected_and_outside_pixels_exactly():
    source = np.arange(18,dtype=np.float32).reshape(2,3,3) / 7
    mask = np.array([[0,1,1],[1,.5,1]],np.float32)
    protected = np.array([[0,1,0],[0,0,0]],np.float32)
    result = compose_local(source,source*2,mask,protected,.5)
    np.testing.assert_array_equal(result[0,:2],source[0,:2])
    np.testing.assert_allclose(result[0,2],source[0,2]*1.5)
    np.testing.assert_allclose(result[1,1],source[1,1]*1.25)
    np.testing.assert_array_equal(compose_local(source,source*2,mask,protected,0),source)


def test_invalid_luminance_and_geometry_refuse():
    with pytest.raises(ValueError,match='INVALID_LUMINANCE'):
        rotate_chroma(np.full((2,2,3),-1.),0,1)
    with pytest.raises(ValueError,match='INPUT_INVALID'):
        compose_local(np.ones((2,2,3)),np.ones((2,2,3)),np.ones((1,2)),np.zeros((2,2)),1)


def test_local_params_match_documented_required_fields_and_bounds():
    import json
    from pathlib import Path
    from uuid import uuid4
    from pydantic import ValidationError
    from photo_worker.contracts.portrait_local import PortraitHairParams, PortraitClothingParams
    schemas = json.loads((Path(__file__).resolve().parents[2] / 'docs/implementation-v2/contracts/schemas.json').read_text())['$defs']
    ref = {'artifact_id': str(uuid4()), 'sha256': 'a'*64}
    common = {'schema_version':1, 'base':{'asset_version_id':str(uuid4()),'source':ref,'canvas_hash':'b'*64},'strength':1.,'mask':ref,'protected':ref,'hue_delta_degrees':0.,'saturation_factor':1.}
    for model, field in [(PortraitHairParams,'detail'),(PortraitClothingParams,'wrinkle_strength')]:
        body = dict(common, **{field:0.})
        assert set(model.model_fields) == set(schemas[model.__name__]['required'])
        model.model_validate_json(json.dumps(body))
        for key in body:
            missing = dict(body); del missing[key]
            with pytest.raises(ValidationError):
                model.model_validate_json(json.dumps(missing))
        for key in ('strength','hue_delta_degrees','saturation_factor',field):
            limits = schemas[model.__name__]['properties'][key]
            for value in (limits['minimum'],limits['maximum']):
                model.model_validate_json(json.dumps(dict(body, **{key:value})))
            for value in (limits['minimum']-.01,limits['maximum']+.01):
                with pytest.raises(ValidationError):
                    model.model_validate_json(json.dumps(dict(body, **{key:value})))
        with pytest.raises(ValidationError):
            model.model_validate_json(json.dumps(dict(body, extra=True)))


def test_local_inputs_require_full_closure_geometry_and_protected_role():
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef, canonical_hash
    from photo_worker.contracts.images import WorkingImage, MaskRef
    from photo_worker.contracts.portrait_local import PortraitHairParams
    from photo_worker.operations.portrait.local_inputs import verify_local_inputs
    from pathlib import Path
    import json
    # Reuse the independently established WorkingImage contract fixture.
    fixture = Path(__file__).resolve().parents[2] / 'backend/internal/review/testdata/working-images-python.json'
    data = json.loads(fixture.read_bytes())
    source = WorkingImage.model_validate_json(json.dumps(data[0] if isinstance(data,list) else data))
    ref = lambda: ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    source_ref, target_ref, guard_ref = ref(), ref(), ref()
    params = PortraitHairParams.model_validate_json(json.dumps({'schema_version':1,'base':{'asset_version_id':str(uuid4()),'source':source_ref.model_dump(mode='json'),'canvas_hash':canonical_hash(source.canvas)},'strength':1,'mask':target_ref.model_dump(mode='json'),'protected':guard_ref.model_dump(mode='json'),'hue_delta_degrees':0,'saturation_factor':1,'detail':0}))
    target = MaskRef(artifact=ref(),canvas=source.canvas,transform_chain=source.transform_chain,object_id=None,selector='hair',confidence=1.,model_lock_id=None,protected=False)
    guard = target.model_copy(update={'artifact':ref(),'protected':True})
    inputs = [source_ref,source.pixels,target_ref,guard_ref,target.artifact,guard.artifact,*([source.alpha] if source.alpha else [])]
    verify_local_inputs(params,source,target,guard,inputs)
    for missing in inputs:
        with pytest.raises(ValueError,match='PORTRAIT_INPUT_UNDECLARED'):
            verify_local_inputs(params,source,target,guard,[r for r in inputs if r != missing])
    with pytest.raises(ValueError,match='PORTRAIT_MASK_ROLE_MISMATCH'):
        verify_local_inputs(params,source,target,target,inputs)
    foreign = target.model_copy(update={'canvas':source.canvas.model_copy(update={'width':source.canvas.width+1})})
    with pytest.raises(ValueError,match='PORTRAIT_MASK_GEOMETRY_MISMATCH'):
        verify_local_inputs(params,source,foreign,guard,inputs)


def test_local_portrait_resource_admission_refuses_each_insufficient_budget():
    from photo_worker.operations.portrait.local_resources import local_portrait_resources, admit_local_portrait
    from photo_worker.runtime.executor import OperationFailure
    required = local_portrait_resources(6000,4000)
    profile = {'renderer_version':'v2.2','backend':'cpu','resource_estimate':required}
    assert admit_local_portrait(profile,6000,4000) == required
    assert required['ram_bytes'] > 24_000_000*12
    for key, code in [('ram_bytes','RAM_ADMISSION_EXCEEDED'),('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),('output_bytes','OUTPUT_ADMISSION_EXCEEDED')]:
        changed = dict(required); changed[key] -= 1
        with pytest.raises(OperationFailure,match=code):
            admit_local_portrait(dict(profile,resource_estimate=changed),6000,4000)
    for estimate in (None,dict(required,pixel_count=1),dict(required,vram_bytes=1),dict(required,ram_bytes=True)):
        with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
            admit_local_portrait(dict(profile,resource_estimate=estimate),6000,4000)
    for width,height in [(0,1),(True,1),(200000001,1)]:
        with pytest.raises(ValueError):
            local_portrait_resources(width,height)


def test_prepare_local_scratch_lifetime_and_exact_zero_strength(tmp_path):
    from uuid import uuid4
    from photo_worker.runtime.scratch import ScratchSpace
    from photo_worker.contracts.portrait_local import PortraitHairParams
    from photo_worker.operations.portrait.prepare_local import prepare_local
    from photo_worker.operations.portrait.local_resources import local_portrait_resources
    import json
    ref = {'artifact_id':str(uuid4()),'sha256':'a'*64}
    params = PortraitHairParams.model_validate_json(json.dumps({'schema_version':1,'base':{'asset_version_id':str(uuid4()),'source':ref,'canvas_hash':'b'*64},'strength':0,'mask':ref,'protected':ref,'hue_delta_degrees':30,'saturation_factor':1.3,'detail':1}))
    from pathlib import Path
    from photo_worker.contracts.images import WorkingImage
    data = json.loads((Path(__file__).resolve().parents[2]/'backend/internal/review/testdata/working-images-python.json').read_bytes())
    color = WorkingImage.model_validate_json(json.dumps(data[0])).color
    rgb = np.random.default_rng(1).uniform(0,2,(4,5,3)).astype(np.float32)
    mask = np.ones((4,5),np.float32)
    profile = {'renderer_version':'v2.2','backend':'cpu','resource_estimate':local_portrait_resources(5,4)}
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:
        with prepare_local(params,rgb,mask,mask,color,scratch,profile) as files:
            np.testing.assert_array_equal(np.load(files.rgb),rgb)
            assert not np.load(files.changed_mask).any()
            path = files.rgb
        assert not path.exists()

        active = params.model_copy(update={'strength':1.})
        target = mask.copy(); target[0] = 0
        guard = np.zeros_like(mask); guard[:,0] = 1; guard[1,2] = .01
        with prepare_local(active,rgb,target,guard,color,scratch,profile) as files:
            output = np.load(files.rgb)
            changed = np.load(files.changed_mask)
            np.testing.assert_array_equal(output[0],rgb[0])
            np.testing.assert_array_equal(output[:,0],rgb[:,0])
            np.testing.assert_array_equal(output[1,2],rgb[1,2])
            assert changed[1,2] == 0
            assert np.any(output[1:,1:] != rgb[1:,1:])
            np.testing.assert_array_equal(changed,np.any(output!=rgb,axis=-1).astype(np.float32))
        before = set(scratch.directory.iterdir())
        calls = 0
        def cancel():
            nonlocal calls
            calls += 1
            if calls == 3:
                raise RuntimeError('cancelled')
        with pytest.raises(RuntimeError,match='cancelled'):
            with prepare_local(active,rgb,target,guard,color,scratch,profile,check_active=cancel):
                pytest.fail('cancelled preparation yielded output')
        assert set(scratch.directory.iterdir()) == before



def test_backend_worker_local_resource_parity():
    import json
    from pathlib import Path
    from photo_worker.operations.portrait.local_resources import local_portrait_resources
    fixtures=json.loads((Path(__file__).resolve().parents[2]/'backend/internal/v2portrait/testdata/local-resource-parity.json').read_text())
    for fixture in fixtures:
        assert local_portrait_resources(fixture['width'],fixture['height'])==fixture['expected']
