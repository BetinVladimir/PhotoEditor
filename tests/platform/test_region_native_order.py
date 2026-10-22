"""Authenticated stage coverage must never silently become a color-only preview."""
from types import SimpleNamespace
import pytest
from photo_worker.operations.region.native_order import validate_native_region_order


def effective(*,beauty=False,denoise='off'):
    return SimpleNamespace(beauty=SimpleNamespace(enabled=beauty),strength=.5,
        extensions=SimpleNamespace(restoration=SimpleNamespace(denoise=denoise,denoise_strength=.4)))


def stage(operation,params=None,policy=None):
    return (SimpleNamespace(operation=operation),params or {},None,None,policy or {})


def test_supported_stages_follow_actual_apply_order():
    validate_native_region_order(effective(),[stage(name) for name in
        ('geometry.body','image.color','image.local','portrait.detail','background.compose','geometry.crop','image.upscale')],model_lock_sha='a'*64)


@pytest.mark.parametrize('operations',[
    ('geometry.crop','image.local'),('image.local','image.local'),
    ('background.compose','background.blur'),
])
def test_duplicate_or_reordered_native_stages_refuse(operations):
    with pytest.raises(ValueError,match='STAGE_ORDER_INVALID'):
        validate_native_region_order(effective(),[stage(name) for name in operations],model_lock_sha='a'*64)


@pytest.mark.parametrize('operation',['unknown.operation'])
def test_uninstalled_native_stage_is_explicitly_unavailable(operation):
    with pytest.raises(ValueError,match='PROVIDER_UNAVAILABLE:'+operation):
        validate_native_region_order(effective(),[stage(operation)],model_lock_sha='a'*64)


def test_two_generation_stages_follow_authoritative_distinct_phase_positions():
    def generated(phase):return (SimpleNamespace(operation='image.generate',phase=phase),{},None,None,{})
    validate_native_region_order(effective(),[stage('image.local'),generated('background'),stage('image.inpaint'),generated('generate'),stage('geometry.crop')],model_lock_sha='a'*64)
    with pytest.raises(ValueError,match='STAGE_ORDER_INVALID'):
        validate_native_region_order(effective(),[generated('generate'),stage('image.inpaint')],model_lock_sha='a'*64)
    with pytest.raises(ValueError,match='STAGE_ORDER_INVALID'):
        validate_native_region_order(effective(),[generated('background'),generated('background')],model_lock_sha='a'*64)


def test_automatic_repair_positions_match_apply():
    validate_native_region_order(effective(),[stage(name) for name in
        ('image.color','sensor.dust_auto_repair','image.local','portrait.detail','defects.auto_repair','background.compose')],model_lock_sha='a'*64)
    with pytest.raises(ValueError,match='STAGE_ORDER_INVALID'):
        validate_native_region_order(effective(),[stage('image.local'),stage('sensor.dust_auto_repair')],model_lock_sha='a'*64)


def test_enabled_denoise_requires_authenticated_stage_and_exact_model_and_strength():
    requested=effective(denoise='scunet_color_real_psnr')
    with pytest.raises(ValueError,match='DENOISE_STAGE_COVERAGE'):
        validate_native_region_order(requested,[],model_lock_sha='a'*64)
    params=dict(model_lock_id='a'*64,model_id='scunet_color_real_psnr',strength=.2)
    validate_native_region_order(requested,[stage('image.denoise',params)],model_lock_sha='a'*64)
    for override in (dict(model_lock_id='b'*64),dict(strength=.4),dict(model_id='other')):
        with pytest.raises(ValueError,match='DENOISE_EFFECTIVE_POLICY'):
            validate_native_region_order(requested,[stage('image.denoise',params|override)],model_lock_sha='a'*64)


def test_enabled_beauty_never_disappears_without_a_stage():
    with pytest.raises(ValueError,match='BEAUTY_STAGE_COVERAGE'):
        validate_native_region_order(effective(beauty=True),[],model_lock_sha='a'*64)


def test_retouch_delta_does_not_relabel_original_beauty_policy():
    original=effective(beauty=True);edited=effective(beauty=True)
    original.beauty.smoothing=.2;edited.beauty.smoothing=.7
    resolved=SimpleNamespace(modules=SimpleNamespace(beauty=original.beauty),strength=edited.strength)
    entries=[stage('portrait.beauty',policy={'resolved':resolved})]
    validate_native_region_order(edited,entries,model_lock_sha='a'*64,original_style=original)
    with pytest.raises(ValueError,match='BEAUTY_EFFECTIVE_POLICY'):
        validate_native_region_order(edited,entries,model_lock_sha='a'*64)
    # New recipe-only beauty is executed privately; the original DAG is still
    # checked against the actual originally disabled module.
    validate_native_region_order(edited,[],model_lock_sha='a'*64,original_style=effective())
