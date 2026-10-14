"""Pinned physical eye/protection masks; producer lineage is a separate gate."""
from contextlib import contextmanager
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.images import MaskRef
from .adoption import ArtifactRef
from .array_evidence import ArrayEvidenceSession
from .portrait_donor_images import verify_donor_working_image


def verify_donor_mask(raw,ref,source,*,protected):
    parse_json(raw,max_bytes=1_048_576)
    if sha256(raw).hexdigest()!=ref.sha256:
        raise ValueError('DONOR_MASK_SHA')
    mask=MaskRef.model_validate_json(raw)
    if (canonical_bytes(mask)!=raw or mask.canvas!=source.canvas or
            mask.transform_chain!=source.transform_chain or mask.protected!=protected or
            mask.artifact.artifact_id.int==0):
        raise ValueError('DONOR_MASK_BINDING')
    return mask


@contextmanager
def read_donor_masks(params,source_raw,target_raw,protected_raw,reader,*,
                     scratch_root,max_bytes,check_active):
    """Load exact full-canvas mask mappings; no inference/resizing/substitution.

    Reader must prove input scope/version. Caller must prove original producer
    lineage/manual replay and eye-mask selection; geometry alone is insufficient.
    """
    if not callable(check_active):raise ValueError('DONOR_MASK_CONTEXT')
    owned=params.model_copy(deep=True)
    source=verify_donor_working_image(bytes(source_raw),owned.base)
    target=verify_donor_mask(bytes(target_raw),owned.eyes_mask,source,protected=False)
    guard=verify_donor_mask(bytes(protected_raw),owned.protected,source,protected=True)
    if target.artifact.artifact_id==guard.artifact.artifact_id:
        raise ValueError('DONOR_MASK_BINDING')
    shape=(source.canvas.height,source.canvas.width)
    with ArrayEvidenceSession(reader,scratch_root,max_bytes,check_active) as arrays:
        def load(mask):
            ref=ArtifactRef(mask.artifact.artifact_id,mask.artifact.sha256)
            return arrays.array(ref,shape,source='input',unit=True,legacy_input_npy=True)
        target_pixels=load(target);guard_pixels=load(guard)
        check_active()
        yield target,guard,target_pixels,guard_pixels
        check_active()
