"""Bounded declared-input loading for a complete frozen authored recipe."""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef, canonical_hash
from photo_worker.contracts.images import WorkingImage, MaskRef
from .render_recipe import FrozenRenderRecipe, decode_frozen_render_recipe
from .render_layers import RenderLayerStack, decode_render_layer_stack


def read_render_document(artifacts, ref: ArtifactRef, check_active=lambda: None) -> bytes:
    """TaskArtifacts.stream enforces closure membership; independently hash bytes."""
    check_active();chunks=[];size=0
    with artifacts.stream(ref) as stream:
        while True:
            check_active();chunk=stream.read(min(65536,1048577-size))
            if not chunk:break
            size+=len(chunk)
            if size>1048576:raise ValueError('RENDER_DOCUMENT_TOO_LARGE')
            chunks.append(chunk)
    check_active();body=b''.join(chunks)
    if size<2 or sha256(body).hexdigest()!=ref.sha256:
        raise ValueError('RENDER_DOCUMENT_DIGEST_CHANGED')
    return body


@dataclass(frozen=True)
class FrozenRenderInputs:
    recipe: FrozenRenderRecipe
    source: WorkingImage
    layers: RenderLayerStack | None
    protection: MaskRef | None


def load_frozen_render_inputs(artifacts, reference: ArtifactRef, *, check_active=lambda: None) -> FrozenRenderInputs:
    """Validate root/source/layers/protection metadata before full composition.

    Caller admits RAM/scratch/runtime before invoking. Params, learning model,
    style, mask pixel arrays and operator-specific nested closure remain required
    by the next composition stage; this loader never declares them qualified.
    """
    recipe=decode_frozen_render_recipe(read_render_document(artifacts,reference,check_active))
    check_active();source=artifacts.contract(recipe.recipe.source.source,WorkingImage);check_active()
    if canonical_hash(source.canvas)!=recipe.recipe.source.canvas_hash:
        raise ValueError('RENDER_SOURCE_CANVAS_CHANGED')
    layers=None
    if recipe.recipe.layer_stack is not None:
        layers=decode_render_layer_stack(read_render_document(artifacts,recipe.recipe.layer_stack,check_active),recipe.recipe.layer_stack,recipe.recipe.source)
    protection=None
    if recipe.recipe.protected_mask is not None:
        check_active();protection=artifacts.contract(recipe.recipe.protected_mask,MaskRef);check_active()
        if (not protection.protected or protection.canvas!=source.canvas
                or protection.transform_chain.hash!=source.transform_chain.hash):
            raise ValueError('RENDER_PROTECTION_CHANGED')
    return FrozenRenderInputs(recipe,source,layers,protection)
