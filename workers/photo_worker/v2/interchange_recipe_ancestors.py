"""Explicit prepared source ancestry; archive references alone grant no authority."""
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from .adoption import AdoptionInvalid


def install_prepared_ancestors(documents, arrays, ancestors, qualify_ancestor, check_active):
    """Authority proves each (current binding, ancestor ref, exact image) edge.

    The caller must derive these edges from the prepared import's authenticated
    resolver. Geometry equality is necessary but cannot prove lineage. The
    callback runs before registration and must return the literal True.
    """
    owned = tuple(ancestors)
    if len(owned) > 5000 or owned and not callable(qualify_ancestor):
        raise ValueError('INTERCHANGE_ANCESTOR_INSTALLATION')
    seen = set()
    for binding, reference, image in owned:
        check_active()
        binding = VersionBinding.model_validate_json(canonical_bytes(binding))
        reference = ArtifactRef.model_validate_json(canonical_bytes(reference))
        image = WorkingImage.model_validate_json(canonical_bytes(image))
        current = documents.images.get(binding.asset_version_id)
        key = (binding.asset_version_id, reference.artifact_id)
        if (current is None or current[0] != binding or key in seen
                or reference.artifact_id.int == 0
                or sha256(canonical_bytes(image)).hexdigest() != reference.sha256
                or image.canvas != current[1].canvas
                or image.transform_chain != current[1].transform_chain
                or image.color != current[1].color or image.dtype != 'float32'
                or image.canvas.width * image.canvas.height > 200000000):
            raise AdoptionInvalid('INTERCHANGE_ANCESTOR_SOURCE')
        if qualify_ancestor(binding.model_copy(deep=True), reference.model_copy(deep=True), image.model_copy(deep=True)) is not True:
            raise AdoptionInvalid('INTERCHANGE_ANCESTOR_AUTHORITY')
        check_active()
        seen.add(key)
        documents.image_hashes.add(reference.sha256)
        h, w = image.canvas.height, image.canvas.width
        arrays._remember(binding, image.pixels, 'WorkingPixels', (h, w, 3), False, canonical=True)
        if image.alpha is not None:
            arrays._remember(binding, image.alpha, 'WorkingAlpha', (h, w), True, canonical=True)
        for step in image.transform_chain.steps:
            if step.kind == 'dense_warp':
                arrays._remember(binding, step.inverse_field, 'DenseWarpArray',
                    (step.to_canvas.height, step.to_canvas.width, 2), False, canonical=True)
    check_active()
