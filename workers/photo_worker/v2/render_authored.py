"""Join complete authored metadata and owned same-canvas pixel preparation."""
from contextlib import contextmanager
from dataclasses import dataclass
from .render_layer_params import load_render_layer_params
from .render_prepare import PreparedRender, admit_render_inputs, prepare_render_pixels
from .render_style import PreparedRenderStyle, prepare_render_style


@dataclass(frozen=True)
class PreparedAuthoredRender:
    style: PreparedRenderStyle
    render: PreparedRender


@contextmanager
def prepare_authored_render(artifacts, inputs, operators, scalar_overrides, render_base,
                            render_proposal, verify_base_runtime, *, execution_profile,
                            provider_peak, supported_modules, max_params_document_bytes,
                            max_override_document_bytes, max_input_bytes,
                            max_mask_bytes, max_tile_bytes, alpha_mode='require_float32',
                            tile_size=512, source_result=None, check_active=lambda: None):
    """Require admission before IO and runtime proof before style resolution.

    Base provider receives (inputs, PreparedRenderStyle, original pixels, verified mask tiles), so
    authored modules and independently reconciled corrections are explicit.
    Providers must be installed/qualified separately; geometry still requires
    its dedicated stage. Keep this context open through measured publication.
    """
    check_active()
    if not callable(verify_base_runtime) or not callable(render_base):
        raise ValueError('RENDER_BASE_PROVIDER_INVALID')
    admit_render_inputs(inputs, execution_profile, provider_peak,
        tile_size=tile_size, alpha_mode=alpha_mode)
    verify_base_runtime()
    check_active()
    layers = load_render_layer_params(artifacts, inputs, operators,
        max_document_bytes=max_params_document_bytes, check_active=check_active)
    style = prepare_render_style(artifacts, inputs, scalar_overrides,
        max_override_document_bytes=max_override_document_bytes, check_active=check_active)
    from .render_modules import required_render_modules
    if (not isinstance(supported_modules, frozenset)
            or not required_render_modules(style.effective) <= supported_modules):
        raise ValueError("RENDER_COMPLETE_PROVIDER_REQUIRED")
    with prepare_render_pixels(artifacts, inputs, layers,
            lambda state, original, masks: render_base(state, style, original, masks), render_proposal,
            max_input_bytes=max_input_bytes, max_mask_bytes=max_mask_bytes,
            max_tile_bytes=max_tile_bytes, alpha_mode=alpha_mode, tile_size=tile_size, base_with_masks=True,
            source_result=source_result, execution_profile=execution_profile,
            provider_peak=provider_peak, check_active=check_active) as prepared:
        yield PreparedAuthoredRender(style, prepared)
        check_active()
