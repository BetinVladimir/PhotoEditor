"""Resolve pinned style and complete authored overrides before pixel execution."""
from dataclasses import dataclass
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.override import OverrideV1
from photo_worker.contracts.style import StyleV2
from photo_worker.operations.session.contracts import CorrectionVector
from photo_worker.operations.learning.recipe import materialize_frozen_style
from .render_inputs import read_render_document
from .render_overrides import load_render_overrides, merge_render_override_modules
from .render_scalars import ResolvedRenderOverride, verify_render_scalar_evidence


@dataclass(frozen=True)
class PreparedRenderStyle:
    base: StyleV2
    merged: StyleV2
    effective: StyleV2
    overrides: tuple[OverrideV1, ...]
    effective_delta: CorrectionVector


def prepare_render_style(artifacts, inputs, scalar_overrides: tuple[ResolvedRenderOverride, ...], *,
                         max_override_document_bytes: int, check_active=lambda: None) -> PreparedRenderStyle:
    """Read admitted immutable evidence; preserve module and scalar semantics.

    The production profile gate rejects example styles. Whole-module overrides
    are retained independently of their caller-resolved scalar contributions.
    effective is materialized once after module resolution; providers render
    effective without adding effective_delta again. This function neither
    executes pixels nor proves SQL approval/runtime locks.
    """
    check_active()
    raw = read_render_document(artifacts, inputs.recipe.recipe.base_style, check_active)
    if canonical_bytes(parse_json(raw)) != raw:
        raise ValueError('RENDER_STYLE_NONCANONICAL')
    base = StyleV2.from_bytes(raw)
    overrides = load_render_overrides(artifacts, inputs,
        max_document_bytes=max_override_document_bytes, check_active=check_active)
    merged = merge_render_override_modules(base, overrides, inputs.source, check_active=check_active)
    delta = verify_render_scalar_evidence(artifacts, inputs.recipe, scalar_overrides,
        check_active=check_active)
    effective = materialize_frozen_style(inputs.recipe.scalars, merged,
        inputs.recipe.recipe.base_style, inputs.recipe.recipe.source,
        inputs.recipe.scalars.icc_lock_sha, inputs.recipe.scalars.model_lock_sha)
    check_active()
    return PreparedRenderStyle(base, merged, effective, overrides, delta.model_copy(deep=True))
