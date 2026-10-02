"""Complete authored base-style provider coverage requirements."""
from photo_worker.contracts.style import StyleV2

def required_render_modules(style: StyleV2) -> frozenset[str]:
    """Declare every requested stage; zero strength does not waive coverage."""
    required = {'color', 'strength'}
    if style.mode == 'adaptive':
        required.add('adaptive')
        # A selected adaptive prototype may enable local color even when the
        # fixed fallback disables it. Zero strength does not waive coverage.
        if style.adaptation is not None and any(prototype.local.enabled for prototype in style.adaptation.prototypes):
            required.add('local')
    for name in ('local', 'beauty', 'defects', 'geometry'):
        if getattr(style, name).enabled:
            required.add(name)
    for name in ('culling', 'series', 'composition', 'portrait_detail',
                 'inpainting', 'generation'):
        value = getattr(style.extensions, name)
        if value is not None and value.enabled:
            required.add(name)
    for name in ('masking', 'restoration', 'background'):
        if getattr(style.extensions, name) is not None:
            required.add(name)
    if style.extensions.region_adjustments:
        required.add('region_adjustments')
    return frozenset(required)
