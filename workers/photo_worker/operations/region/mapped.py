"""Verified full-canvas inputs for an independently qualified Region pipeline."""
from contextlib import contextmanager

from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.style import StyleV2
from photo_worker.contracts.images import WorkingImage
from photo_worker.v2.render_source import load_render_source_pixels
from .adapter import RenderedRegionCanvas


@contextmanager
def prepare_region_full_canvas(artifacts, source, style, render_pipeline, *,
                               max_input_bytes, check_active=lambda: None):
    """Keep original verified NPY/normalized alpha alive through full rendering.

    Pipeline receives (source metadata, effective style, original pixels). It
    must render every enabled module before ROI presentation. This helper does
    not qualify providers, admit resources or substitute an identity renderer.
    """
    check_active()
    if (not isinstance(source, WorkingImage) or not isinstance(style, StyleV2)
            or not callable(render_pipeline)):
        raise ValueError('REGION_FULL_PIPELINE_INVALID')
    style_sha = canonical_hash(style)
    source_sha = canonical_hash(source)
    def check_inputs():
        check_active()
        if canonical_hash(style) != style_sha:
            raise ValueError('REGION_EFFECTIVE_STYLE_CHANGED')
        if canonical_hash(source) != source_sha:
            raise ValueError('REGION_SOURCE_METADATA_CHANGED')
    with load_render_source_pixels(artifacts, source, max_input_bytes=max_input_bytes,
            alpha_mode='normalize_uint16', check_active=check_active) as loaded:
        check_inputs()
        with render_pipeline(source, style, loaded.pixels) as rendered:
            check_active()
            if not isinstance(rendered, RenderedRegionCanvas):
                raise ValueError('REGION_RENDER_INVALID')
            check_inputs()
            yield rendered
            check_active()
            check_inputs()
        check_inputs()
