from hashlib import sha256

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.interchange_export_style import validate_export_override


def _override(extensions):
    raw = canonical_bytes({
        'schema_version': 1,
        'input_sha256': 'a' * 64,
        'modules': {'extensions': extensions},
        'revision': 1,
    })
    return raw, sha256(raw).hexdigest()


@pytest.mark.parametrize('module,body,model', [
    ('inpainting', {'enabled': True, 'model': 'big_lama', 'policy': 'review', 'mask_source': 'per_image'}, 'big_lama'),
    ('generation', {'enabled': True, 'model': 'sd15_inpaint', 'prompt': 'sky', 'negative_prompt': '',
                    'seed': 1, 'steps': 10, 'guidance_scale': 7, 'strength': .5,
                    'canvas': [512, 512], 'require_acceptance': True}, 'sd15_inpaint'),
])
def test_override_requires_installed_model_for_enabled_module(module, body, model):
    raw, digest = _override({module: body})
    with pytest.raises(ValueError, match='MODEL_NOT_INSTALLED'):
        validate_export_override(raw, digest, {})
    with pytest.raises(ValueError, match='MODEL_NOT_INSTALLED'):
        validate_export_override(raw, digest, {model: 'operator-label-without-digest'})
    assert validate_export_override(raw, digest, {model: 'b' * 64}).modules.extensions is not None


def test_disabled_override_module_does_not_require_model_lock():
    raw, digest = _override({'inpainting': {'enabled': False}, 'generation': {'enabled': False}})
    assert validate_export_override(raw, digest, {}).modules.extensions is not None
