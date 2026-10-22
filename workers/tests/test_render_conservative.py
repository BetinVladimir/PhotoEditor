from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.conservative import ConservativeRepairReport
from photo_worker.v2.render_conservative import PinnedConservativeProviderFactory
from photo_worker.v2.render_effects import RenderLayerEffect, effect_review_required
from photo_worker.v2.render_providers import combine_render_layer_factories


def report(operation='defects.auto_repair'):
    ref = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    return ConservativeRepairReport(policy='conservative_auto_v1', operation=operation,
        image=ref, proposal=ref, resolved=ref, selected_candidate_ids=['b' * 64],
        applied_candidate_ids=['b' * 64], skipped_candidate_ids=[])


@pytest.mark.parametrize('backend', ['cpu', 'mps', 'cuda'])
def test_auto_native_provider_installs_both_distinct_operators_for_qualified_renderer(backend):
    factory = PinnedConservativeProviderFactory(runtime_sha256='c' * 64,
        verify_runtime=lambda: None, validate_binding=lambda params, state: None)
    providers = combine_render_layer_factories(factory)(object(), [], {'backend': backend}, lambda: None)
    assert {provider.operator.operation for provider in providers} == {'defects.auto_repair', 'sensor.dust_auto_repair'}
    assert all(provider.operator.version == '1' for provider in providers)


@pytest.mark.parametrize('missing', ['verify_runtime', 'validate_binding'])
def test_auto_provider_never_installs_without_live_runtime_and_binding_authority(missing):
    args = dict(runtime_sha256='c' * 64, verify_runtime=lambda: None, validate_binding=lambda params, state: None)
    args[missing] = None
    with pytest.raises(ValueError, match='INSTALLATION'):
        PinnedConservativeProviderFactory(**args)


def test_auto_effect_keeps_typed_inventory_and_does_not_fabricate_review_decision():
    effect = RenderLayerEffect(uuid4(), 'defects.auto_repair', '1', 'c' * 64, (), False,
        conservative_report=report())
    assert effect_review_required(effect) is False
    wrong = RenderLayerEffect(effect.layer_id, 'sensor.dust_auto_repair', '1', 'c' * 64, (), False,
        conservative_report=report())
    with pytest.raises(ValueError): effect_review_required(wrong)
    malformed = report().model_copy(update={'applied_candidate_ids': ['d' * 64]})
    with pytest.raises(ValueError):
        effect_review_required(RenderLayerEffect(effect.layer_id, effect.operation, '1', 'c' * 64, (), False,
            conservative_report=malformed))
