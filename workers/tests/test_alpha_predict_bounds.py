"""Alpha prediction must admit whole-frame buffers and declared inputs."""

from types import SimpleNamespace
import pytest

from photo_worker.operations.alpha.predict import _declared_alpha_inputs, _required_alpha_ram, _valid_alpha_image


def test_large_alpha_prediction_exceeds_small_ram_profile():
    assert _required_alpha_ram(10_000, 10_000, False) > 10 * 1024**3
    assert _required_alpha_ram(1024, 1024, True) < 1024**3
    assert _required_alpha_ram(2000, 3000, True) > _required_alpha_ram(2000, 3000, False)


@pytest.mark.parametrize('height,width,alpha', [(0, 1, False), (1, 0, False), (True, 1, False), (1, 1, 1)])
def test_alpha_ram_rejects_invalid_dimensions(height, width, alpha):
    with pytest.raises(ValueError, match='ALPHA_SIZE_INVALID'):
        _required_alpha_ram(height, width, alpha)


def test_alpha_requires_both_declared_artifacts():
    image = SimpleNamespace(pixels='rgb', alpha='opacity')
    assert not _declared_alpha_inputs(image, {'rgb'})
    assert _declared_alpha_inputs(image, {'rgb', 'opacity'})
    image.alpha = None
    assert _declared_alpha_inputs(image, {'rgb'})
    assert not _declared_alpha_inputs(image, {'opacity'})


def test_alpha_requires_working_rec2020_gamma_image():
    image = SimpleNamespace(dtype='float32', color=SimpleNamespace(
        profile_id='work_rec2020_gamma24_v1', encoding='gamma24'),
        canvas=SimpleNamespace(width=4000, height=3000))
    assert _valid_alpha_image(image)
    image.color.profile_id = 'linear_srgb_v1'
    assert not _valid_alpha_image(image)
    image.color.profile_id = 'work_rec2020_gamma24_v1'
    image.color.encoding = 'linear'
    assert not _valid_alpha_image(image)
    image.color.encoding = 'gamma24'
    image.canvas.width = 200_000_001
    assert not _valid_alpha_image(image)
