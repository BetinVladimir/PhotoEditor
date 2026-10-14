"""Metadata for independently prepared negative pixels; no publication grant."""
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash, parse_json
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.optical_negative import OpticalNegativeParams
from .contracts import ImageReport


def build_negative_metadata(params, source_raw, pixels, *, clipping_fraction):
    p=OpticalNegativeParams.model_validate_json(canonical_bytes(params))
    pixel_ref=ArtifactRef.model_validate_json(canonical_bytes(pixels))
    if not isinstance(source_raw,(bytes,bytearray)) or not 2<=len(source_raw)<=65536:
        raise ValueError('NEGATIVE_SOURCE_INVALID')
    raw=bytes(source_raw)
    if sha256(raw).hexdigest()!=p.base.source.sha256:raise ValueError('NEGATIVE_SOURCE_HASH_MISMATCH')
    image=WorkingImage.model_validate_json(canonical_bytes(parse_json(raw,65536)))
    if (canonical_bytes(image)!=raw or canonical_hash(image.canvas)!=p.base.canvas_hash
            or image.dtype!='float32' or image.color.encoding!='linear'):
        raise ValueError('NEGATIVE_SOURCE_INVALID')
    input_ids={p.base.source.artifact_id,p.calibration.artifact_id}
    # Include referenced dense transform fields as well as RGB and alpha.
    pending=[image.model_dump(mode='json')]
    while pending:
        value=pending.pop()
        if isinstance(value,dict):
            if set(value)=={'artifact_id','sha256'}:
                ref=ArtifactRef.model_validate_json(canonical_bytes(value))
                input_ids.add(ref.artifact_id)
            else:pending.extend(value.values())
        elif isinstance(value,list):pending.extend(value)
    exact_bypass=(p.strength==0 and pixel_ref==image.pixels
        and pixel_ref.artifact_id not in {p.base.source.artifact_id,p.calibration.artifact_id}
        and (image.alpha is None or image.alpha.artifact_id!=pixel_ref.artifact_id))
    if (pixel_ref.artifact_id.int==0 or pixel_ref.sha256=='0'*64
            or (pixel_ref.artifact_id in input_ids and not exact_bypass)
            or type(clipping_fraction) not in (float,int) or not 0<=clipping_fraction<=1):
        raise ValueError('NEGATIVE_RESULT_INVALID')
    # Only pixels change: inherited alpha, ICC, canvas and transform lineage stay.
    output=image.model_copy(deep=True);output.pixels=pixel_ref
    report=ImageReport(schema_version=1,status='review',operation='optical.negative',
        skipped_count=0,reason='negative_review',outside_mask_max_error=0.,
        clipping_fraction=clipping_fraction,identity=None,transform=None,
        sources=[p.base],registrations=[])
    return output,report
