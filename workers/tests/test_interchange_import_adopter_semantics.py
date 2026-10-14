"""The built-in import policy never approves unknown or review-sensitive work."""

from types import SimpleNamespace
from uuid import uuid4

from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import (
    CorrectionItem, CorrectionPlan, CorrectionVector)
from photo_worker.contracts.restoration import DenoiseParams, UpscaleParams
from photo_worker.contracts.override import OverrideModules, OverrideV1
from photo_worker.v2.render_layers import RenderLayer, RenderLayerStack
from photo_worker.v2.render_recipe import EditingRecipeV3
from photo_worker.v2.interchange_import_adopter_semantics import SupportedImportSemanticPolicy
from test_interchange_person_mask_confirmation import _mask_and_binding
from test_interchange_import_stage import adopted_fixture


def test_import_semantics_requires_selected_model_and_current_scope():
    mask, binding = _mask_and_binding()
    scope = (uuid4(), uuid4(), 1, 7, uuid4())
    context = SimpleNamespace(operation='v2.interchange.import', version='1',
                              attempt=1, fence=7, result_id=scope[4])
    policy = SupportedImportSemanticPolicy({'image.denoise': object(),
                                            ('image.upscale', 2): object(),
                                            'mask.person': object()})
    denoise = DenoiseParams(image=binding.source, strength=0.5,
                            model_lock_id='a' * 64)
    upscale = UpscaleParams(image=binding.source, scale=2,
                            model_id='realesrgan_x2', model_lock_id='b' * 64)
    assert policy(scope, context, 'Params', denoise, binding) is True
    assert policy(scope, context, 'Params', upscale, binding) is True
    assert policy(scope, context, 'MaskRef', mask, binding) is True
    foreign = binding.model_copy(update={'source': ArtifactRef(
        artifact_id=uuid4(), sha256='e' * 64)})
    assert policy(scope, context, 'Params', denoise, foreign) is False
    assert policy(scope, context, 'Params', upscale, foreign) is False
    assert policy(scope, context, 'Unknown', {}, binding) is False
    assert policy(scope, context, 'Params',
        upscale.model_copy(update={'scale': 4}), binding) is False
    assert policy(scope, SimpleNamespace(operation='v2.interchange.import',
        version='1', attempt=1, fence=8, result_id=scope[4]),
        'Params', denoise, binding) is False
    assert policy(scope, context, 'MaskRef',
        mask.model_copy(update={'object_id': uuid4()}), binding) is False
    other_canvas = binding.model_copy(update={'canvas_hash': 'f' * 64})
    assert policy(scope, context, 'MaskRef', mask, other_canvas) is False
    image = WorkingImage.model_validate_json(adopted_fixture()[3])
    assert policy(scope, context, 'WorkingImage', image, binding) is True
    assert policy(scope, context, 'WorkingImage', image, other_canvas) is False


def test_import_semantics_empty_pin_does_not_approve_model_documents():
    _, binding = _mask_and_binding()
    scope = (uuid4(), uuid4(), 1, 7, uuid4())
    context = SimpleNamespace(operation='v2.interchange.import', version='1',
                              attempt=1, fence=7, result_id=scope[4])
    policy = SupportedImportSemanticPolicy({})
    params = DenoiseParams(image=binding.source, strength=0.5,
                           model_lock_id='a' * 64)
    assert policy(scope, context, 'Params', params, binding) is False
    fixed = OverrideV1(schema_version=1, input_sha256='a' * 64,
        modules=OverrideModules(strength=0.5), revision=1)
    assert policy(scope, context, 'Override', fixed, binding) is True
    assert policy(scope, context, 'Override', fixed.model_copy(update={
        'accepted_candidate_ids': ['foreign']}), binding) is False


def test_import_layer_stack_rejects_non_render_qualification_names():
    _, binding = _mask_and_binding()
    scope = (uuid4(), uuid4(), 1, 7, uuid4())
    context = SimpleNamespace(operation='v2.interchange.import', version='1',
                              attempt=1, fence=7, result_id=scope[4])
    policy = SupportedImportSemanticPolicy({'image.denoise': object(),
        ('image.upscale', 2): object(), 'color.stats': object(),
        'mask.person': object()})
    reference = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    layer = RenderLayer(id=uuid4(), enabled=True, opacity=1, blend='normal',
        operation='image.denoise', operation_version='1', params=reference,
        mask={'op': 'ref', 'mask': reference.model_dump(mode='json'),
              'canvas_hash': binding.canvas_hash})
    def admitted(operation):
        stack = RenderLayerStack(schema_version=1, source=binding,
            layers=[layer.model_copy(update={'operation': operation})])
        return policy(scope, context, 'LayerStack', stack, binding)
    assert admitted('image.denoise') is True
    assert admitted('image.upscale') is True
    assert admitted('color.stats') is False
    assert admitted('mask.person') is False
    assert admitted('unknown') is False


def test_import_recipe_semantics_requires_current_source_binding():
    _, binding = _mask_and_binding()
    scope = (uuid4(), uuid4(), 1, 7, uuid4())
    context = SimpleNamespace(operation='v2.interchange.import', version='1',
                              attempt=1, fence=7, result_id=scope[4])
    policy = SupportedImportSemanticPolicy({})
    recipe = EditingRecipeV3(schema_version=3, source=binding,
        base_style=ArtifactRef(artifact_id=uuid4(), sha256='a' * 64),
        session_corrections=None, learning_version_id=None, overrides=[],
        layer_stack=None, renderer_version='v2.2',
        parameter_delta=CorrectionVector(ev=0, log_r=0, log_g=0, log_b=0,
            saturation_delta=0, retouch_delta=0))
    assert policy(scope, context, 'EditingRecipeV3', recipe, binding) is True
    foreign = binding.model_copy(update={'asset_version_id': uuid4()})
    assert policy(scope, context, 'EditingRecipeV3', recipe, foreign) is False


def test_import_correction_plan_requires_exactly_one_current_item():
    _, binding = _mask_and_binding()
    scope = (uuid4(), uuid4(), 1, 7, uuid4())
    context = SimpleNamespace(operation='v2.interchange.import', version='1',
                              attempt=1, fence=7, result_id=scope[4])
    policy = SupportedImportSemanticPolicy({})
    delta = CorrectionVector(ev=0, log_r=0, log_g=0, log_b=0,
        saturation_delta=0, retouch_delta=0)
    item = CorrectionItem(binding=binding, ordinal=0, delta=delta,
        disposition='no_change', reason=None)
    plan = CorrectionPlan(schema_version=1, snapshot_digest='a' * 64,
        items=[item])
    assert policy(scope, context, 'CorrectionPlan', plan, binding) is True
    foreign = binding.model_copy(update={'asset_version_id': uuid4()})
    assert policy(scope, context, 'CorrectionPlan', plan, foreign) is False
    duplicate = plan.model_copy(update={'items': [item, item]})
    assert policy(scope, context, 'CorrectionPlan', duplicate, binding) is False
