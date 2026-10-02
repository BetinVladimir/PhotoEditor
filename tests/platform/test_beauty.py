import numpy as np
import pytest
from photo_worker.contracts.style import Beauty
from photo_worker.algorithms.beauty import beauty_filter


def params(smoothing=.5,tone=.4,texture=.9):
    return Beauty(enabled=True,algorithm='beautyfilter_v1',smoothing=smoothing,tone_evenness=tone,texture_keep=texture,skin_mask='face68_conservative_v1',protect_features=True)
def fixture():
    yy,xx=np.mgrid[:128,:128]
    image=np.stack([.4+xx*.001,.3+xx*.001,.25+xx*.001],axis=-1).astype(np.float32)
    image+=np.random.default_rng(9).normal(0,.008,image.shape).astype(np.float32)
    skin=np.ones((128,128),np.float32);protected=np.zeros_like(skin)
    return image,skin,protected


def test_protected_and_outside_skin_are_exact_and_source_unchanged():
    image,skin,protect=fixture();skin[:20]=0;protect[:,40:55]=1;before=image.copy()
    result=beauty_filter(image,skin,protect,100.,params())
    assert result.applied and result.texture_energy_ratio is not None
    assert np.array_equal(result.rgb[:20],image[:20])
    assert np.array_equal(result.rgb[:,40:55],image[:,40:55])
    assert np.array_equal(image,before) and not result.rgb.flags.writeable
    assert np.isfinite(result.rgb).all() and result.rgb.min()>=0 and result.rgb.max()<=1


def test_tone_independent_of_smoothing_and_texture_preserved():
    image,skin,protect=fixture()
    tone=beauty_filter(image,skin,protect,100.,params(smoothing=0.,tone=1.))
    assert tone.applied and np.std(tone.rgb.mean(axis=0))<np.std(image.mean(axis=0))
    no_texture=beauty_filter(image,skin,protect,100.,params(smoothing=1.,tone=0.,texture=0.))
    texture=beauty_filter(image,skin,protect,100.,params(smoothing=1.,tone=0.,texture=1.))
    assert texture.texture_energy_ratio>no_texture.texture_energy_ratio


def test_gates_zero_strength_and_cancel():
    image,skin,protect=fixture()
    for kwargs,reason in [(dict(confidence=.5),'low_confidence'),(dict(face_width=40.),'face_too_small')]:
        width=kwargs.pop('face_width',100.)
        result=beauty_filter(image,skin,protect,width,params(),**kwargs)
        assert not result.applied and result.reason==reason and np.array_equal(result.rgb,image)
    result=beauty_filter(image,skin,protect,100.,params(smoothing=0.,tone=0.))
    assert result.reason=='zero_strength' and np.array_equal(result.rgb,image)
    with pytest.raises(ValueError):beauty_filter(image,skin,protect,100.,params(),max_work_pixels=1)
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):beauty_filter(image,skin,protect,100.,params(),check_active=cancel)
