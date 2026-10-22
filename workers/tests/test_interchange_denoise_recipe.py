"""Real denoise/layer/source contracts; Style and binary IO remain test ports."""
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, parse_json
from photo_worker.contracts.images import WorkingImage, MaskRef
from photo_worker.contracts.restoration import DenoiseParams
from photo_worker.operations.session.contracts import CorrectionVector
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_recipe_closure import PreparedRecipeClosureQualifier
from photo_worker.v2.render_layers import RenderLayer, RenderLayerStack
from photo_worker.v2.render_recipe import EditingRecipeV3
from test_interchange_recipe_semantics import semantics_fixture


@pytest.mark.parametrize('mode', ['valid', 'disabled', 'source', 'runtime', 'tile'])
def test_denoise_recipe_uses_real_params_source_semantics_and_gc_receipt(mode):
    seen = []
    def runtime(contract, typed, binding):
        seen.append((contract, type(typed).__name__))
        return not (mode == 'runtime' and isinstance(typed, DenoiseParams))
    semantic, binding, image = semantics_fixture(runtime)
    bodies, entries = {}, []
    def put(value, contract):
        raw = canonical_bytes(value)
        reference = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
        bodies[reference.artifact_id] = raw
        entries.append((reference, contract, 'application/json'))
        return reference
    bodies[binding.source.artifact_id] = canonical_bytes(image)
    entries.append((binding.source, 'WorkingImage', 'application/json'))
    entries.append((image.pixels, 'WorkingPixels', 'application/x-npy'))
    if image.alpha is not None:
        entries.append((image.alpha, 'WorkingAlpha', 'application/x-npy'))
    mask_pixels = ArtifactRef(artifact_id=uuid4(), sha256='d' * 64)
    mask = MaskRef(model_lock_id=None, artifact=mask_pixels, canvas=image.canvas, transform_chain=image.transform_chain,
                   selector='manual', confidence=1, protected=True)
    mask_ref = put(mask, 'MaskRef')
    entries.append((mask_pixels, 'MaskPixels', 'application/x-npy'))
    params = DenoiseParams(image=binding.source, strength=.75, model_lock_id='b' * 64)
    value = parse_json(canonical_bytes(params))
    if mode == 'source': value['image']['artifact_id'] = str(uuid4())
    if mode == 'tile': value['tile'] = 1024
    params_ref = put(value, 'Params')
    layer = RenderLayer(id=uuid4(), enabled=mode != 'disabled', opacity=0 if mode == 'disabled' else 1,
                        blend='normal', operation='image.denoise', operation_version='1', params=params_ref,
                        mask=dict(op='ref', mask=mask_ref.model_dump(mode='json'), canvas_hash=binding.canvas_hash))
    stack_ref = put(RenderLayerStack(schema_version=1, source=binding, layers=[layer]), 'LayerStack')
    # The isolated Style port is explicit: this fixture does not certify StyleV2.
    style_ref = put({}, 'StyleV2')
    recipe = EditingRecipeV3(schema_version=3, source=binding, base_style=style_ref,
        session_corrections=None, learning_version_id=None, overrides=[], layer_stack=stack_ref,
        renderer_version='v2.2', parameter_delta=CorrectionVector(ev=0,log_r=0,log_g=0,log_b=0,
                                                               saturation_delta=0,retouch_delta=0))
    root = put(recipe, 'EditingRecipeV3')
    definitions = {'StyleV2': {'type': 'object'}}
    for model in (WorkingImage, MaskRef, DenoiseParams, RenderLayerStack, EditingRecipeV3):
        schema = model.model_json_schema()
        definitions.update(schema.pop('$defs', {}))
        definitions[model.__name__] = schema
    definitions['LayerStack'] = definitions['RenderLayerStack']
    schemas = canonical_bytes({'$defs': definitions})
    class Documents:
        def __call__(self, contract, value, target):
            if contract == 'StyleV2': return value == {}
            return semantic(contract, value, target)
        def qualify_params(self, schema, value, target):
            return semantic.qualify_params(schema, value, target)
    closure = PreparedRecipeClosureQualifier(entries=entries, schemas_jcs=schemas,
        schemas_sha=sha256(schemas).hexdigest(), read_document=lambda ref, maximum: bodies[ref.artifact_id],
        qualify_document=Documents(), qualify_binary=lambda *args: True)
    if mode in ('valid', 'disabled'):
        assert closure(root, recipe, binding) is True
        assert ('Params', 'DenoiseParams') in seen
        receipt = closure.qualified_document_refs(root, binding)
        assert any(row['artifact_id'] == str(params_ref.artifact_id) and row['sha256'] == params_ref.sha256
                   for row in receipt)
    else:
        with pytest.raises((AdoptionInvalid, ValueError)):
            closure(root, recipe, binding)
        with pytest.raises(AdoptionInvalid, match='RECEIPT_REQUIRED'):
            closure.qualified_document_refs(root, binding)
