"""Attempt-scoped WorkingImage staging; task adoption/receipt remains separate."""
from dataclasses import dataclass
from hashlib import sha256
import numpy as np
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.contracts.images import WorkingImage
from .exposure_source import bind_local_exposure_source


@dataclass(frozen=True)
class StagedExposure:
    pixels:ArtifactRef
    image:ArtifactRef
    bytes_written:int


def stage_local_exposure_result(artifacts,edit,source_raw,encoded,*,verify_admission,check_active,max_bytes,max_output_bytes):
    if not callable(verify_admission) or not callable(check_active):
        raise ValueError('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
    edit=ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    image=bind_local_exposure_source(edit,source_raw)
    def admitted():
        check_active()
        if verify_admission(edit.model_copy(deep=True)) is not True:
            raise ValueError('SEMANTIC_EXPOSURE_ADMISSION_DENIED')
        check_active()
    return stage_exposure_working_image(artifacts,image,encoded,admitted=admitted,
        check_active=check_active,max_bytes=max_bytes,max_output_bytes=max_output_bytes)


def stage_exposure_working_image(artifacts,image,encoded,*,admitted,check_active,max_bytes,max_output_bytes):
    """Attempt staging only; caller supplies independent authority and source proof."""
    if not callable(admitted) or not callable(check_active):
        raise ValueError('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
    image=WorkingImage.model_validate_json(canonical_bytes(image))
    shape=(image.canvas.height,image.canvas.width,3);pixels=shape[0]*shape[1]
    if not isinstance(encoded,np.ndarray) or encoded.dtype!=np.float32 or encoded.shape!=shape:
        raise ValueError('SEMANTIC_EXPOSURE_ARRAYS_INVALID')
    if (type(max_bytes) is not int or not 1<=max_bytes<=2**53-1 or max_bytes<36*pixels+64*1024**2
            or type(max_output_bytes) is not int or not 1<=max_output_bytes<=2**53-1
            or max_output_bytes<12*pixels+16384+1048576):
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    admitted()
    encoded=encoded.copy()
    admitted()
    for row in range(0,shape[0],256):
        check_active();tile=encoded[row:row+256]
        if not np.isfinite(tile).all() or np.any(tile<0):raise ValueError('SEMANTIC_EXPOSURE_ARRAYS_INVALID')
    with artifacts.scratch.reserve(12*pixels+16384) as reservation:
        path=reservation.path('semantic-exposure','.npy')
        output=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=shape)
        try:
            for row in range(0,shape[0],256):
                check_active();output[row:row+256]=encoded[row:row+256]
            output.flush();reservation.validate_file(path)
        finally:output._mmap.close()
        admitted()
        digest=sha256()
        with path.open('rb') as stream:
            while block:=stream.read(65536):
                check_active();digest.update(block)
        admitted()
        pixel_ref=ArtifactRef.model_validate_json(canonical_bytes(artifacts.stage(path,reservation,'pixels','application/x-npy')))
        if pixel_ref.artifact_id.int==0 or pixel_ref.artifact_id==image.pixels.artifact_id or pixel_ref.sha256!=digest.hexdigest():
            raise ValueError('SEMANTIC_EXPOSURE_OUTPUT_INVALID')
        written=path.stat().st_size
    result=WorkingImage.model_validate_json(canonical_bytes(image.model_copy(update={'pixels':pixel_ref.model_copy(deep=True)})))
    result_bytes=canonical_bytes(result)
    admitted()
    image_ref=ArtifactRef.model_validate_json(canonical_bytes(artifacts.stage_contract(result,'working_image')))
    if (image_ref.artifact_id.int==0 or image_ref.artifact_id==pixel_ref.artifact_id
            or image_ref.sha256!=sha256(result_bytes).hexdigest()
            or canonical_bytes(result)!=result_bytes):
        raise ValueError('SEMANTIC_EXPOSURE_OUTPUT_INVALID')
    admitted()
    return StagedExposure(pixel_ref,image_ref,written+len(result_bytes))
