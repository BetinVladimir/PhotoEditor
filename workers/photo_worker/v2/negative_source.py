"""Leased accepted source metadata authority; actual RGB bytes are separate."""
from copy import deepcopy
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.optical_negative import OpticalNegativeParams
from photo_worker.contracts.images import WorkingImage
from .adoption import AdoptionInvalid


def negative_source_authority(database,lease,params_raw,*,verify_color,check_active):
    if not callable(check_active) or not callable(verify_color) or not callable(getattr(database,'negative_source_ready',None)):
        raise ValueError('NEGATIVE_AUTHORITY_REQUIRED')
    owned=deepcopy(lease)
    if not isinstance(params_raw,bytes) or not 2<=len(params_raw)<=65536:
        raise ValueError('NEGATIVE_PARAMS_INVALID')
    raw=bytes(params_raw);command=owned.command
    if command.operation!='optical.negative' or command.operation_version!='1' or sha256(raw).hexdigest()!=command.params.sha256:
        raise ValueError('NEGATIVE_PARAMS_INVALID')
    params=OpticalNegativeParams.model_validate_json(raw)
    if canonical_bytes(params)!=raw:raise ValueError('NEGATIVE_PARAMS_INVALID')
    def verify(value,source_raw):
        if canonical_bytes(OpticalNegativeParams.model_validate_json(canonical_bytes(value)))!=raw:
            raise AdoptionInvalid('NEGATIVE_PARAMS_CHANGED')
        if not isinstance(source_raw,bytes) or not 2<=len(source_raw)<=65536 or sha256(source_raw).hexdigest()!=params.base.source.sha256:
            raise AdoptionInvalid('NEGATIVE_SOURCE_CHANGED')
        image=WorkingImage.model_validate_json(source_raw)
        if canonical_bytes(image)!=source_raw or canonical_hash(image.canvas)!=params.base.canvas_hash or image.dtype!='float32' or image.color.encoding!='linear':
            raise AdoptionInvalid('NEGATIVE_SOURCE_CHANGED')
        check_active()
        if database.negative_source_ready(deepcopy(owned),raw) is not True:
            raise AdoptionInvalid('NEGATIVE_SOURCE_STALE')
        check_active()
        if verify_color(image.model_copy(deep=True)) is not True:
            raise AdoptionInvalid('NEGATIVE_ICC_MISMATCH')
        check_active()
        return True
    return verify


from contextlib import contextmanager


@contextmanager
def load_negative_source_arrays(reader,database,lease,params_raw,source_raw,*,
        scratch_root,max_bytes,ram_max_bytes,verify_color,check_active):
    """Retain hash-verified RGB/alpha mappings with live source authority.

    Reader must resolve only declared original task inputs. Budget includes
    retained RGB/alpha and planned prepared RGB/mask; preparation scratch is
    separately reserved by ScratchSpace. Alpha is validated and inherited.
    """
    from .array_evidence import ArrayEvidenceSession
    from .adoption import ArtifactRef
    params=OpticalNegativeParams.model_validate_json(params_raw)
    original=bytes(source_raw) if isinstance(source_raw,bytes) else source_raw
    authority=negative_source_authority(database,lease,params_raw,
        verify_color=verify_color,check_active=check_active)
    authority(params,original)
    image=WorkingImage.model_validate_json(original)
    h,w=image.canvas.height,image.canvas.width;pixels=h*w
    owners=28+(4 if image.alpha is not None else 0)
    if pixels>200_000_000 or type(ram_max_bytes) is not int or not 1<=ram_max_bytes<=2**53-1:
        raise AdoptionInvalid('NEGATIVE_RESOURCE_INVALID')
    if ram_max_bytes<owners*pixels+128*min(pixels,512*512)+65536:
        raise AdoptionInvalid('NEGATIVE_RAM_EXCEEDED')
    if type(max_bytes) is not int or not 1<=max_bytes<=2**53-1:
        raise AdoptionInvalid('NEGATIVE_RESOURCE_INVALID')
    if max_bytes<(12+(4 if image.alpha is not None else 0))*pixels+32768:
        raise AdoptionInvalid('NEGATIVE_SCRATCH_REQUIRED')
    def active():authority(params,original)
    def ref(value):return ArtifactRef(value.artifact_id,value.sha256)
    with ArrayEvidenceSession(reader,scratch_root,max_bytes,active) as arrays:
        scan=arrays.array(ref(image.pixels),(h,w,3),source='input',legacy_input_npy=True)
        if image.alpha is not None:
            arrays.array(ref(image.alpha),(h,w),source='input',unit=True,legacy_input_npy=True)
        active();yield scan;active()
