"""Complete encoded pixel computation; no output publication or success receipt."""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.runtime.arrays import read_array
from .exposure_source import bind_local_exposure_source
from .exposure_loader import load_local_exposure_arrays
from .execute_exposure import execute_local_exposure
from .exposure_encoding import restore_local_exposure_encoding
from .local_exposure import local_exposure_memory



def encoded_local_exposure_memory(shape):
    """Complete declared-array/gamma compute peak, excluding publisher costs."""
    pixels=local_exposure_memory(shape)//96
    return 204*pixels+64*1024**2

def execute_encoded_local_exposure(artifacts,edit,candidate_raw,intent_raw,*,protection_ref,verify_origin,verify_color,verify_admission,max_bytes,check_active):
    if not all(callable(value) for value in (verify_origin,verify_color,verify_admission,check_active)):
        raise ValueError('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
    if type(max_bytes) is not int or not 64*1024**2<=max_bytes<=2**53-1:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    edit=ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    def admitted():
        check_active()
        if verify_admission(edit.model_copy(deep=True)) is not True:
            raise ValueError('SEMANTIC_EXPOSURE_ADMISSION_DENIED')
        check_active()
    admitted()
    source_raw=artifacts.contract_bytes(edit.source.source,WorkingImage)
    image=bind_local_exposure_source(edit,source_raw)
    shape=(image.canvas.height,image.canvas.width)
    pixels=local_exposure_memory(shape)//96
    overhead=64*1024**2
    #192P encoding cost plus12P loaded linear retained through encoding.
    required=encoded_local_exposure_memory(shape)
    if required>max_bytes:raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    arrays=load_local_exposure_arrays(artifacts,edit,protection_ref=protection_ref,
        verify_origin=verify_origin,verify_color=verify_color,verify_admission=verify_admission,
        max_bytes=max_bytes,check_active=check_active)
    linear=execute_local_exposure(edit,candidate_raw,intent_raw,verify_admission=verify_admission,
        load_verified_arrays=lambda value,**kwargs:arrays,max_bytes=max_bytes,check_active=check_active)
    admitted()
    # Re-read the same declared immutable source. No unpinned original buffer.
    with artifacts.input(image.pixels,suffix='.npy') as local:
        with read_array(local.path,(*shape,3),'float32',check_active) as mapped:
            original=mapped.copy()
    admitted()
    encoded=restore_local_exposure_encoding(original,linear,arrays.mask,arrays.protected,image.color,
        max_bytes=max_bytes-overhead-12*pixels,check_active=check_active)
    admitted()
    return encoded


def execute_measured_local_exposure(artifacts, edit, candidate_raw, intent_raw, *,
        protection_ref, verify_origin, verify_color, verify_admission, max_bytes,
        check_active):
    """Compute and independently replay measured quality before output staging."""
    from .exposure_quality import measure_exposure_quality
    edit=ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    encoded=execute_encoded_local_exposure(artifacts,edit,candidate_raw,intent_raw,
        protection_ref=protection_ref,verify_origin=verify_origin,verify_color=verify_color,
        verify_admission=verify_admission,max_bytes=max_bytes,check_active=check_active)
    image=bind_local_exposure_source(edit,artifacts.contract_bytes(edit.source.source,WorkingImage))
    pixels=image.canvas.width*image.canvas.height
    # Encoded output remains resident during the independent input reload.
    arrays=load_local_exposure_arrays(artifacts,edit,protection_ref=protection_ref,
        verify_origin=verify_origin,verify_color=verify_color,verify_admission=verify_admission,
        max_bytes=max_bytes-12*pixels,check_active=check_active)
    with artifacts.input(image.pixels,suffix='.npy') as local:
        with read_array(local.path,(image.canvas.height,image.canvas.width,3),'float32',check_active) as mapped:
            original=mapped.copy()
    report=measure_exposure_quality(edit,original,encoded,arrays.mask,arrays.protected,image.color,
        max_bytes=max_bytes-12*pixels,verify_color=verify_color,check_active=check_active)
    check_active()
    if verify_admission(edit.model_copy(deep=True)) is not True:
        raise ValueError('SEMANTIC_EXPOSURE_ADMISSION_DENIED')
    check_active()
    return encoded,report
