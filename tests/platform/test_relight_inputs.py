import json
from pathlib import Path
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_hash
from photo_worker.contracts.images import WorkingImage, RelativeDepth, MaskRef
from photo_worker.contracts.relight import ImageRelightParams
from photo_worker.operations.session.contracts import Calibration
from photo_worker.operations.portrait.relight_inputs import verify_relight_inputs


def inputs():
    root = Path(__file__).resolve().parents[2]
    source = WorkingImage.model_validate_json(json.dumps(json.loads(
        (root/'backend/internal/review/testdata/working-images-python.json').read_text())[0]))
    ref = lambda digest='a'*64: ArtifactRef(artifact_id=uuid4(), sha256=digest)
    source = source.model_copy(update={'alpha': ref()})
    depth = RelativeDepth(artifact=ref(), float32_shape=[source.canvas.height, source.canvas.width],
        invalid_mask=ref(), canvas=source.canvas, transform_chain=source.transform_chain,
        normalization='minmax_valid')
    subject = MaskRef(artifact=ref(), canvas=source.canvas, transform_chain=source.transform_chain,
        selector='subject', confidence=1., protected=False, model_lock_id=None)
    protected = subject.model_copy(update={'artifact': ref(), 'selector': 'protected', 'protected': True})
    calibration = Calibration(**dict(schema_version=1, model_lock_sha='a'*64, dataset_sha='d'*64,
        qualified=True, scene_cosine_min=.3, scene_log_luma_max=1., cluster_diameter_max=1.,
        sharpness_scale=1., clipping_penalty=1., match_inlier_min=.3, match_error_px_max=5.,
        identity_similarity_min=.4, landmark_drift_max=.2, normalization_scales=[1.]*6,
        loss_weights=[.4,.1,.1,.1,.2,.1], focus_warning=.5, focus_critical=.2, eye_open_min=.5,
        glare_warning_fraction=.1, clipping_warning_fraction=.1, clipping_critical_fraction=.3,
        min_face_edge_px=64, depth_sigma=.1, registration_error_px=2., motion_threshold=.1))
    params = ImageRelightParams.model_validate_json(json.dumps(dict(schema_version=1,
        base=dict(asset_version_id=str(uuid4()), source=ref(canonical_hash(source)).model_dump(mode='json'),
                  canvas_hash=canonical_hash(source.canvas)), strength=.7,
        **{name: ref(canonical_hash(doc)).model_dump(mode='json') for name, doc in
           [('depth',depth), ('subject',subject), ('protected',protected), ('calibration',calibration)]},
        lights=[dict(x=.5,y=.5,radius=.2,ev=1.)])))
    declared = [params.base.source, params.depth, params.subject, params.protected, params.calibration,
                source.pixels, source.alpha, depth.artifact, depth.invalid_mask,
                subject.artifact, protected.artifact]
    return [params, source, depth, subject, protected, calibration, declared]


def test_exact_closure_and_each_missing_dependency():
    args = inputs()
    verify_relight_inputs(*args)
    for ref in args[-1]:
        with pytest.raises(ValueError, match='RELIGHT_INPUT_UNDECLARED'):
            verify_relight_inputs(*args[:-1], [r for r in args[-1] if r != ref])
    # A matching SHA with another locator is not an authorized declared input.
    replaced = [r.model_copy(update={'artifact_id':uuid4()}) for r in args[-1]]
    with pytest.raises(ValueError, match='RELIGHT_INPUT_UNDECLARED'):
        verify_relight_inputs(*args[:-1], replaced)


@pytest.mark.parametrize('index', [1,2,3,4,5])
def test_document_substitution(index):
    args = inputs()
    changes = {1:{'alpha':None}, 2:{'invalid_mask':None}, 3:{'confidence':.5},
               4:{'confidence':.5}, 5:{'depth_sigma':.2}}
    args[index] = args[index].model_copy(update=changes[index])
    with pytest.raises(ValueError, match='RELIGHT_DOCUMENT_MISMATCH'):
        verify_relight_inputs(*args)


@pytest.mark.parametrize('mode', ['geometry','role','calibration','canvas','dtype'])
def test_bound_but_incompatible_inputs(mode):
    args = inputs(); params = args[0]
    if mode == 'geometry':
        args[3] = args[3].model_copy(update={'canvas':args[1].canvas.model_copy(update={'space_id':'other'})})
    if mode == 'role': args[3] = args[3].model_copy(update={'protected':True})
    if mode == 'calibration': args[5] = args[5].model_copy(update={'qualified':False})
    if mode == 'canvas': params = params.model_copy(update={'base':params.base.model_copy(update={'canvas_hash':'f'*64})})
    if mode == 'dtype': args[1] = args[1].model_copy(update={'dtype':'uint16'})
    params = params.model_copy(update={name:getattr(params,name).model_copy(update={'sha256':canonical_hash(args[i])})
        for name,i in [('depth',2),('subject',3),('protected',4),('calibration',5)]})
    params = params.model_copy(update={'base':params.base.model_copy(update={
        'source':params.base.source.model_copy(update={'sha256':canonical_hash(args[1])})})})
    args[0] = params
    with pytest.raises(ValueError, match={'geometry':'RELIGHT_GEOMETRY_MISMATCH',
        'role':'RELIGHT_MASK_ROLE_MISMATCH','calibration':'RELIGHT_CALIBRATION_INVALID',
        'canvas':'RELIGHT_SOURCE_MISMATCH','dtype':'RELIGHT_SOURCE_MISMATCH'}[mode]):
        verify_relight_inputs(*args)
