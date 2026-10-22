"""Original-byte pinned source document boundary for approved exposure loading."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,canonical_hash,parse_json
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from .local_exposure import local_exposure_memory
from photo_worker.operations.session.contracts import VersionBinding


def bind_local_exposure_source(edit,source_raw):
    owned=ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    if owned.kind!='local_exposure':
        raise ValueError('SEMANTIC_EXPOSURE_KIND_INVALID')
    return bind_exposure_working_source(owned.source,source_raw)


def bind_exposure_working_source(source,source_raw):
    """Exact source content binding; independently authorized by each caller."""
    source=VersionBinding.model_validate_json(canonical_bytes(source))
    if source.asset_version_id.int==0 or source.source.artifact_id.int==0:
        raise ValueError('SEMANTIC_EXPOSURE_SOURCE_INVALID')
    if not isinstance(source_raw,(bytes,bytearray)) or not 2<=len(source_raw)<=1048576:
        raise ValueError('SEMANTIC_EXPOSURE_SOURCE_INVALID')
    raw=bytes(source_raw)
    if sha256(raw).hexdigest()!=source.source.sha256:
        raise ValueError('SEMANTIC_EXPOSURE_SOURCE_HASH')
    image=WorkingImage.model_validate_json(canonical_bytes(parse_json(raw)))
    if image.pixels.artifact_id.int==0 or (image.alpha is not None and image.alpha.artifact_id.int==0):
        raise ValueError('SEMANTIC_EXPOSURE_SOURCE_INVALID')
    if canonical_hash(image.canvas)!=source.canvas_hash:
        raise ValueError('SEMANTIC_EXPOSURE_SOURCE_CHANGED')
    if (image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1'
            or image.color.encoding!='gamma24'):
        raise ValueError('SEMANTIC_EXPOSURE_COLOR_UNSUPPORTED')
    local_exposure_memory((image.canvas.height,image.canvas.width))
    return image
