"""Bounded physical source/donor mappings; caller owns live scope authorization."""
from contextlib import contextmanager
from dataclasses import dataclass
from .adoption import ArtifactRef
from .array_evidence import ArrayEvidenceSession
from .portrait_donor_images import verify_donor_image_pair


@dataclass(frozen=True)
class DonorImageMaterial:
    source_image: object
    donor_image: object
    source_rgb: object
    donor_rgb: object
    source_alpha: object
    donor_alpha: object


@contextmanager
def read_donor_image_material(params, source_raw, donor_raw, reader, *,
                             scratch_root, max_bytes, check_active, alpha_mode):
    """Mappings cannot escape this context. No full-frame RGB copies are made.

    Reader must establish immutable, version-pinned input evidence under current
    task authority. Alpha normalization policy must be explicitly selected.
    This loader verifies bytes/layout, not identity, consent or mask lineage.
    """
    if not callable(check_active) or alpha_mode not in ('require_float32','normalize_uint16'):
        raise ValueError('DONOR_MATERIAL_POLICY')
    owned = params.model_copy(deep=True)
    source, donor = verify_donor_image_pair(owned, bytes(source_raw), bytes(donor_raw))
    def ref(value):
        return ArtifactRef(value.artifact_id, value.sha256)
    with ArrayEvidenceSession(reader, scratch_root, max_bytes, check_active) as arrays:
        images=[]
        for image in (source, donor):
            check_active()
            shape=(image.canvas.height,image.canvas.width)
            rgb=arrays.array(ref(image.pixels),(*shape,3),source='input',legacy_input_npy=True)
            alpha=arrays.alpha(None if image.alpha is None else ref(image.alpha),shape,mode=alpha_mode)
            images.append((rgb,alpha))
        check_active()
        yield DonorImageMaterial(source,donor,images[0][0],images[1][0],images[0][1],images[1][1])
        check_active()
