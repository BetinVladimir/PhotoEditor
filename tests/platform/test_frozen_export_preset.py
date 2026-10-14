"""Wire/semantic preset tests; cross-language executed evidence is still pending."""
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.export_preset import validate_frozen_export_preset


def fixture():
    return {'format': 'jpeg', 'icc_id': 'srgb_v1', 'quality': 90,
            'subsampling': '444', 'alpha': 'matte', 'matte_rgb': [1, 1, 1],
            'metadata_policy': 'strip', 'naming_template': '{name}-{version}',
            'size': {'long_edge': 2400, 'fit': 'contain', 'allow_upscale': False}}


def test_exact_frozen_go_shape_and_optional_effects():
    value = fixture()
    validate_frozen_export_preset(canonical_bytes(value))
    value['watermark'] = {'asset_id': '00000000-0000-4000-8000-000000000001', 'opacity': .5,
                          'anchor': 'center', 'margin_x': 0, 'margin_y': 0}
    value['sharpen'] = {'radius': 1, 'strength': .5, 'threshold': .01}
    validate_frozen_export_preset(canonical_bytes(value))


@pytest.mark.parametrize('change', ['quality', 'alpha', 'metadata', 'size', 'unknown', 'null', 'missing_default', 'name_bytes', 'watermark'])
def test_invalid_config_never_silently_normalizes(change):
    value = fixture()
    if change == 'quality': value['quality'] = 101
    elif change == 'alpha': value['alpha'] = 'preserve'
    elif change == 'metadata': value['metadata_policy'] = 'copyright'
    elif change == 'size': value['size']['width'] = 100
    elif change == 'unknown': value['extra'] = True
    elif change == 'null': value['watermark'] = None
    elif change == 'missing_default': del value['size']['allow_upscale']
    elif change == 'name_bytes': value['naming_template'] = 'ф' * 101
    elif change == 'watermark': value['watermark'] = {'asset_id': '00000000-0000-0000-0000-000000000000', 'opacity': .5, 'anchor': 'center', 'margin_x': 0, 'margin_y': 0}
    with pytest.raises(ValueError): validate_frozen_export_preset(canonical_bytes(value))


def test_preserve_png_has_no_matte_or_quality_fields():
    value = fixture()
    value.update(format='png', alpha='preserve')
    del value['quality'];del value['matte_rgb']
    validate_frozen_export_preset(canonical_bytes(value))


def test_duplicate_keys_refused():
    with pytest.raises(ValueError): validate_frozen_export_preset(b'{"format":"jpeg","format":"png"}')
