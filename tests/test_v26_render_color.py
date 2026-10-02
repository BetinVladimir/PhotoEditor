from types import SimpleNamespace
import numpy as np
import pytest
from photo_worker.v2.render_color import prepare_color_base
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_style import prepare_render_style
from photo_worker.algorithms.color import apply_global,apply_strength
from test_v26_render_authored import fixture


def test_existing_color_math_receives_effective_delta_once_with_owned_output(tmp_path):
    artifacts,inputs,overrides,_,_,path=fixture(tmp_path)
    style=prepare_render_style(artifacts,inputs,overrides,max_override_document_bytes=4096)
    original=AdjustmentPixels(np.full((1,2,3),.25,np.float32),None)
    expected=apply_strength(original.rgb,apply_global(original.rgb,style.effective.global_),style.effective.strength)
    try:
        with prepare_color_base(artifacts,inputs,style,original,person_reliable=False) as result:
            np.testing.assert_array_equal(result.rgb,expected)
            assert not result.rgb.flags.writeable and not np.shares_memory(original.rgb,result.rgb)
            assert result.alpha is None
        assert set(tmp_path.rglob('*.npy'))=={path}
    finally:artifacts.scratch.cleanup()


def test_effect_pipeline_is_never_silently_skipped(tmp_path):
    artifacts,inputs,_,_,_,_=fixture(tmp_path)
    # Model-validated styles are supplied by preparation; this isolates coverage.
    style=SimpleNamespace(effective=SimpleNamespace(mode='fixed',local=SimpleNamespace(enabled=False),
        beauty=SimpleNamespace(enabled=True),defects=SimpleNamespace(enabled=False),geometry=SimpleNamespace(enabled=False),
        extensions=SimpleNamespace(culling=None,series=None,composition=None,portrait_detail=None,inpainting=None,
            generation=None,masking=None,restoration=None,background=None,region_adjustments=[])))
    try:
        with pytest.raises(ValueError,match='EFFECT_PIPELINE_REQUIRED'):
            with prepare_color_base(artifacts,inputs,style,None,person_reliable=False):pass
    finally:artifacts.scratch.cleanup()


def test_color_peak_accounts_owned_output_and_tile_memory(tmp_path):
    from photo_worker.v2.render_color import color_provider_peak
    artifacts,inputs,overrides,_,_,_=fixture(tmp_path)
    try:
        style=prepare_render_style(artifacts,inputs,overrides,max_override_document_bytes=4096)
        peak=color_provider_peak(6000,4000,style.effective)
        assert peak['scratch_bytes']==6000*4000*12+8192
        assert peak['ram_bytes']>peak['scratch_bytes']
        assert peak['vram_bytes']==peak['output_bytes']==0
        with pytest.raises(ValueError):color_provider_peak(200000001,1,style.effective)
    finally:artifacts.scratch.cleanup()


def test_positive_protection_preserves_exact_original_pixels(tmp_path):
    artifacts,inputs,overrides,_,_,path=fixture(tmp_path)
    style=prepare_render_style(artifacts,inputs,overrides,max_override_document_bytes=4096)
    original=AdjustmentPixels(np.full((1,2,3),.25,np.float32),None)
    try:
        with prepare_color_base(artifacts,inputs,style,original,person_reliable=False,
                read_protected=lambda *args:np.array([[.001,0]],np.float32)) as result:
            np.testing.assert_array_equal(result.rgb[:,0].view(np.uint32),original.rgb[:,0].view(np.uint32))
            assert np.any(result.rgb[:,1]!=original.rgb[:,1])
        assert set(tmp_path.rglob('*.npy'))=={path}
    finally:artifacts.scratch.cleanup()


@pytest.mark.parametrize('value',[None,np.array([[1,0]],np.float64),np.array([[float('nan'),0]],np.float32)])
def test_invalid_protection_fails_with_owner_cleanup(tmp_path,value):
    artifacts,inputs,overrides,_,_,path=fixture(tmp_path)
    style=prepare_render_style(artifacts,inputs,overrides,max_override_document_bytes=4096)
    try:
        with pytest.raises(ValueError,match='PROTECTION_INVALID'):
            with prepare_color_base(artifacts,inputs,style,AdjustmentPixels(np.zeros((1,2,3),np.float32),None),
                    person_reliable=False,read_protected=lambda *args:value):pass
        assert set(tmp_path.rglob('*.npy'))=={path}
    finally:artifacts.scratch.cleanup()


def test_result_diagnostics_do_not_read_closed_pixel_mappings(tmp_path):
    artifacts,inputs,overrides,_,_,_=fixture(tmp_path)
    style=prepare_render_style(artifacts,inputs,overrides,max_override_document_bytes=4096)
    try:
        with prepare_color_base(artifacts,inputs,style,AdjustmentPixels(np.zeros((1,2,3),np.float32),None),
                person_reliable=False) as result:
            assert repr(result)=='AdjustmentPixels()'
        # The owner has closed the mmap; repr must never dereference its pages.
        assert repr(result)=='AdjustmentPixels()'
    finally:artifacts.scratch.cleanup()
