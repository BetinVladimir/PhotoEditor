"""The built-in import policy never approves unknown or review-sensitive work."""

from types import SimpleNamespace
from uuid import uuid4

from photo_worker.contracts.restoration import DenoiseParams, UpscaleParams
from photo_worker.v2.interchange_import_adopter_semantics import SupportedImportSemanticPolicy
from test_interchange_person_mask_confirmation import _mask_and_binding


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
    assert policy(scope, context, 'Unknown', {}, binding) is False
    assert policy(scope, context, 'Params',
        upscale.model_copy(update={'scale': 4}), binding) is False
    assert policy(scope, SimpleNamespace(operation='v2.interchange.import',
        version='1', attempt=1, fence=8, result_id=scope[4]),
        'Params', denoise, binding) is False
    assert policy(scope, context, 'MaskRef',
        mask.model_copy(update={'object_id': uuid4()}), binding) is False
