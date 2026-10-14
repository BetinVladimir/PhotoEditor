"""Original manual producer replay for physically pinned donor masks."""
from hashlib import sha256
from contextlib import contextmanager
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.manual_mask import RasterizedMask,MaskRasterParams,ManualMask
from photo_worker.operations.portrait.local_manual_proof import verify_manual_pair
from .portrait_donor_images import verify_donor_working_image
from .portrait_donor_masks import verify_donor_mask,read_donor_masks


@contextmanager
def read_verified_donor_manual_masks(params,source_raw,target_raw,protected_raw,
        root_ref,root_raw,producer_params_ref,producer_params_raw,reader,*,
        vectors_raw=None,scratch_root,max_bytes,check_active,producer_authority):
    """Current original producer gate plus physical NPY/raster verification.

    Authority must bind root/Params/vector lineage to this task's exact mask
    refs. Receives independent Params/ref copies; no stored approval is reused.
    """
    if not callable(check_active) or not callable(producer_authority):
        raise ValueError('DONOR_MANUAL_AUTHORITY')
    owned=params.model_copy(deep=True)
    source,target,guard,root,producer=map(bytes,
        (source_raw,target_raw,protected_raw,root_raw,producer_params_raw))
    root_pin=root_ref.model_copy(deep=True);params_pin=producer_params_ref.model_copy(deep=True)
    vectors=None if vectors_raw is None else bytes(vectors_raw)
    def admit():
        check_active()
        if producer_authority(owned.model_copy(deep=True),root_pin.model_copy(deep=True),
                              params_pin.model_copy(deep=True)) is not True:
            raise ValueError('DONOR_MANUAL_AUTHORITY')
        check_active()
    admit()
    with read_donor_masks(owned,source,target,guard,reader,scratch_root=scratch_root,
            max_bytes=max_bytes,check_active=check_active) as material:
        verify_donor_manual_masks(owned,source,target,guard,root_pin,root,params_pin,
            producer,material[2],material[3],vectors_raw=vectors,check_active=check_active)
        admit()
        yield material
        admit()


def verify_donor_manual_masks(params,source_raw,target_raw,protected_raw,
        root_ref,root_raw,producer_params_ref,producer_params_raw,target_pixels,guard_pixels,
        *,vectors_raw=None,check_active):
    """Caller must establish canonical original producer refs under live lease.

    Verifies exact document pins and independently re-rasterizes both masks.
    Does not authorize supplied root/Params refs or prove eye selection.
    """
    if not callable(check_active):raise ValueError('DONOR_MANUAL_CONTEXT')
    check_active()
    owned=params.model_copy(deep=True)
    source=verify_donor_working_image(bytes(source_raw),owned.base)
    target=verify_donor_mask(bytes(target_raw),owned.eyes_mask,source,protected=False)
    guard=verify_donor_mask(bytes(protected_raw),owned.protected,source,protected=True)
    raw=bytes(root_raw);parse_json(raw,max_bytes=1_048_576)
    if root_ref.artifact_id.int==0 or sha256(raw).hexdigest()!=root_ref.sha256:
        raise ValueError('DONOR_MANUAL_ROOT_SHA')
    root=RasterizedMask.model_validate_json(raw)
    if canonical_bytes(root)!=raw:raise ValueError('DONOR_MANUAL_ROOT_CANONICAL')
    producer_raw=bytes(producer_params_raw)
    parse_json(producer_raw,max_bytes=1_048_576)
    if (producer_params_ref.artifact_id.int==0 or
            sha256(producer_raw).hexdigest()!=producer_params_ref.sha256):
        raise ValueError('DONOR_MANUAL_PARAMS_SHA')
    producer=MaskRasterParams.model_validate_json(producer_raw)
    if canonical_bytes(producer)!=producer_raw:
        raise ValueError('DONOR_MANUAL_PARAMS_CANONICAL')
    if producer.vectors is not None:
        if vectors_raw is None:raise ValueError('DONOR_MANUAL_VECTORS_SHA')
        vector_bytes=bytes(vectors_raw);parse_json(vector_bytes,max_bytes=1_048_576)
        if producer.vectors.artifact_id.int==0 or sha256(vector_bytes).hexdigest()!=producer.vectors.sha256:
            raise ValueError('DONOR_MANUAL_VECTORS_SHA')
        if canonical_bytes(ManualMask.model_validate_json(vector_bytes))!=vector_bytes:
            raise ValueError('DONOR_MANUAL_VECTORS_CANONICAL')
    return verify_manual_pair(owned.base.source,source,producer_params_ref,
        producer_raw,root,target,guard,target_pixels,guard_pixels,
        vectors_raw=None if vectors_raw is None else bytes(vectors_raw),check_active=check_active)
