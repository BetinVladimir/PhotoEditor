import numpy as np
import pytest
from photo_worker.algorithms.portrait_donor_patch import prepare_donor_patch


def inputs():
    source = np.ones((40, 40, 3), np.float32)
    donor = np.full_like(source, 2)
    landmarks = np.random.default_rng(14).uniform(4, 35, (68, 2))
    mask = np.zeros((40, 40), np.float32);mask[12:28, 12:28] = 1
    protected = np.zeros_like(mask);protected[20:24, 20:24] = 1
    return source, donor, landmarks, landmarks.copy(), mask, protected


def test_patch_pipeline_is_reviewed_and_preserves_disabled_pixels():
    args = inputs();calls = []
    result = prepare_donor_patch(*args, 80, 1, (0, 0), lambda: calls.append(1))
    assert len(calls) == 5 and result.requires_review
    assert not result.pixels.flags.writeable
    source, _, _, _, mask, guard = args
    disabled = (mask == 0) | (guard == 1)
    np.testing.assert_array_equal(result.pixels.view(np.uint32)[disabled], source.view(np.uint32)[disabled])
    assert result.pixels[15, 15, 0] > 1
    expected=np.any(result.pixels.view(np.uint32)!=source.view(np.uint32),axis=2).astype(np.float32)
    np.testing.assert_array_equal(result.changed_mask,expected)
    assert not result.changed_mask.flags.writeable
    assert not np.any(result.changed_mask[disabled])


@pytest.mark.parametrize('stop', range(1, 6))
def test_patch_cancellation_returns_no_result(stop):
    args = inputs();calls = 0
    def check():
        nonlocal calls
        calls += 1
        if calls == stop: raise InterruptedError('cancelled')
    with pytest.raises(InterruptedError):
        prepare_donor_patch(*args, 80, 1, (0, 0), check)
    np.testing.assert_array_equal(args[0], 1)


def test_global_origin_requires_real_donor_coverage():
    with pytest.raises(ValueError, match='DONOR_COVERAGE'):
        prepare_donor_patch(*inputs(), 80, 1, (100, 100), lambda: None)


@pytest.mark.parametrize('strength',[0.,.7,1.])
def test_gamma_patch_matches_linear_composition_and_preserves_encoded_source(strength):
    args=list(inputs())
    args[0][:]=np.float32(.731234)
    args[0][0,0]=np.float32(-0.)
    args[1][:]=np.float32(1.5)
    original=args[0].copy()
    linear_args=list(args)
    linear_args[0]=np.power(args[0].astype(np.float64),2.4).astype(np.float32)
    linear_args[1]=np.power(args[1].astype(np.float64),2.4).astype(np.float32)
    expected=prepare_donor_patch(*linear_args,80,strength,(0,0),lambda:None)
    actual=prepare_donor_patch(*args,80,strength,(0,0),lambda:None,encoding='gamma24')
    active=(args[4]>0)&(args[5]<1)&(strength>0)
    np.testing.assert_allclose(actual.pixels[active],
        np.power(expected.pixels[active].astype(np.float64),1/2.4),rtol=1e-6)
    np.testing.assert_array_equal(actual.pixels.view(np.uint32)[~active],original.view(np.uint32)[~active])
    np.testing.assert_array_equal(args[0].view(np.uint32),original.view(np.uint32))
    assert actual.requires_review and not actual.pixels.flags.writeable
    np.testing.assert_array_equal(actual.changed_mask,
        np.any(actual.pixels.view(np.uint32)!=original.view(np.uint32),axis=2).astype(np.float32))
    assert not actual.changed_mask.flags.writeable


def test_gamma_patch_rejects_source_decode_overflow():
    args=list(inputs());args[0][:]=np.finfo(np.float32).max
    with pytest.raises(ValueError,match='DONOR_PIXELS'):
        prepare_donor_patch(*args,80,1,(0,0),lambda:None,encoding='gamma24')


def test_transparent_donor_target_is_uncovered_and_returns_no_result():
    args=inputs();alpha=np.zeros(args[0].shape[:2],np.float32)
    with pytest.raises(ValueError,match='DONOR_ALPHA_COVERAGE'):
        prepare_donor_patch(*args,80,1,(0,0),lambda:None,donor_alpha=alpha)


def test_transparent_color_ring_is_excluded_from_gain_statistics():
    args=inputs();alpha=(args[4]>0).astype(np.float32)
    result=prepare_donor_patch(*args,80,1,(0,0),lambda:None,donor_alpha=alpha)
    assert result.color.sample_count==0
    assert result.color.warning=='COLOR_MATCH_UNAVAILABLE'
    np.testing.assert_array_equal(result.color.gains,1)
    assert result.requires_review


def test_transparent_source_ring_is_excluded_and_source_alpha_is_unchanged():
    args=inputs();alpha=(args[4]>0).astype(np.float32);original=alpha.copy()
    result=prepare_donor_patch(*args,80,1,(0,0),lambda:None,source_alpha=alpha)
    assert result.color.sample_count==0 and result.color.warning=='COLOR_MATCH_UNAVAILABLE'
    np.testing.assert_array_equal(alpha.view(np.uint32),original.view(np.uint32))


@pytest.mark.parametrize('value',[np.nan,-.1,1.1])
def test_invalid_source_alpha_returns_no_patch(value):
    args=inputs();alpha=np.full(args[4].shape,value,np.float32)
    with pytest.raises(ValueError,match='DONOR_ALPHA'):
        prepare_donor_patch(*args,80,1,(0,0),lambda:None,source_alpha=alpha)


def test_zero_strength_bypasses_gamma_overflow_and_donor_sampling(monkeypatch):
    import photo_worker.algorithms.portrait_donor_patch as module
    args=list(inputs());args[0][:]=np.finfo(np.float32).max
    args[0][0,0,0]=np.float32(-0.)
    original=args[0].copy()
    def forbidden(*a,**kw):
        pytest.fail('Disabled effect sampled donor pixels')
    monkeypatch.setattr(module,'sample_donor_pixels',forbidden)
    result=prepare_donor_patch(*args,80,0,(0,0),lambda:None,encoding='gamma24')
    np.testing.assert_array_equal(result.pixels.view(np.uint32),original.view(np.uint32))
    assert result.color.sample_count==0 and result.color.warning is None
    assert not result.pixels.flags.writeable
    np.testing.assert_array_equal(result.changed_mask,0)


def test_fully_protected_patch_bypasses_gamma_conversion_and_sampling(monkeypatch):
    import photo_worker.algorithms.portrait_donor_patch as module
    args=list(inputs());args[0][:]=np.finfo(np.float32).max;args[5][:]=np.float32(.01)
    def forbidden(*a,**kw):
        pytest.fail('Protected patch sampled donor')
    monkeypatch.setattr(module,'sample_donor_pixels',forbidden)
    result=prepare_donor_patch(*args,80,1,(0,0),lambda:None,encoding='gamma24')
    np.testing.assert_array_equal(result.pixels.view(np.uint32),args[0].view(np.uint32))


def test_protected_hdr_pixel_does_not_fail_active_gamma_patch():
    args=list(inputs());args[0][21,21]=np.finfo(np.float32).max
    result=prepare_donor_patch(*args,80,1,(0,0),lambda:None,encoding='gamma24')
    np.testing.assert_array_equal(result.pixels[21,21].view(np.uint32),args[0][21,21].view(np.uint32))
    assert result.pixels[15,15,0]>1


@pytest.mark.parametrize('encoding',['linear','gamma24'])
@pytest.mark.parametrize('guard',[np.float32(1e-8),np.float32(.5)])
def test_any_positive_protection_preserves_source_bits(encoding,guard):
    args=list(inputs())
    args[0][:]=np.float32(.713246)
    args[5][14:18,14:18]=guard
    result=prepare_donor_patch(*args,80,1,(0,0),lambda:None,encoding=encoding)
    protected=args[5]>0
    np.testing.assert_array_equal(result.pixels.view(np.uint32)[protected],
                                  args[0].view(np.uint32)[protected])
    assert result.pixels[25,25,0]>args[0][25,25,0]
