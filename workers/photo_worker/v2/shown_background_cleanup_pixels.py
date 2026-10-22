"""Independent bounded physical proof of byte-preserving shown-copy pixels."""
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from .adoption import ArtifactRef, AdoptionInvalid, require_adoption_active
from .array_evidence import ArrayEvidenceSession


def verify_shown_background_cleanup_pixels(reader,shown,output,*,scratch_root,max_bytes,check_active):
    if scratch_root is None or not callable(check_active) or type(max_bytes) is not int or not 1<=max_bytes<=2**53-1:
        raise AdoptionInvalid('SEMANTIC_SHOWN_PIXEL_CONTEXT_REQUIRED')
    def active():require_adoption_active(check_active)
    active()
    shown=WorkingImage.model_validate_json(canonical_bytes(shown))
    output=WorkingImage.model_validate_json(canonical_bytes(output))
    if (shown.pixels.sha256!=output.pixels.sha256 or shown.pixels.artifact_id==output.pixels.artifact_id
            or canonical_bytes(output.model_copy(update={'pixels':shown.pixels}))!=canonical_bytes(shown)):
        raise AdoptionInvalid('SEMANTIC_SHOWN_OUTPUT_CHANGED')
    shape=(shown.canvas.height,shown.canvas.width,3)
    try:
        with ArrayEvidenceSession(reader,scratch_root,max_bytes,active) as arrays:
            for image,source,role in ((shown,'input',None),(output,'output','pixels')):
                active()
                values=arrays.array(ArtifactRef(image.pixels.artifact_id,image.pixels.sha256),shape,source=source,role=role)
                for y in range(0,shape[0],256):
                    for x in range(0,shape[1],256):
                        active()
                        if np.any(values[y:y+256,x:x+256]<0):
                            raise AdoptionInvalid('SEMANTIC_SHOWN_PIXELS_INVALID')
                active()
    except AdoptionInvalid:
        raise
    except ValueError as error:
        # Layout/value failures are deterministic result defects. IO failures
        # remain untouched so the service can retry without rejecting a result.
        raise AdoptionInvalid('SEMANTIC_SHOWN_PIXELS_INVALID') from error
    active()
