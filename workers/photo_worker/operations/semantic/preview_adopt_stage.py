"""Byte-preserving shown-pixel staging; caller must install receipt authority."""
from hashlib import sha256
import numpy as np
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.arrays import read_array
from .exposure_stage import StagedExposure


def stage_shown_preview_image(artifacts,image_ref,image_raw,*,verify_admission,
        check_active,max_output_bytes):
    """Copy verified pinned NPY, never execute an exposure/generative kernel.

    Admission must bind the approved target and canonical shown receipt. This
    helper alone is not an operation and cannot authorize adoption.
    """
    if not callable(verify_admission) or not callable(check_active):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    ref=ArtifactRef.model_validate_json(canonical_bytes(image_ref))
    if (ref.artifact_id.int==0 or not isinstance(image_raw,bytes)
            or not 2<=len(image_raw)<=1048576 or sha256(image_raw).hexdigest()!=ref.sha256):
        raise ValueError('SEMANTIC_PREVIEW_ADOPTION_INVALID')
    image=WorkingImage.model_validate_json(image_raw)
    shape=(image.canvas.height,image.canvas.width,3)
    if image.dtype!='float32' or image.pixels.artifact_id in (ref.artifact_id, image.alpha.artifact_id if image.alpha else ref.artifact_id):
        raise ValueError('SEMANTIC_PREVIEW_ADOPTION_INVALID')
    maximum=12*shape[0]*shape[1]+16384
    if type(max_output_bytes) is not int or not maximum+1048576<=max_output_bytes<=2**53-1:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    def admitted():
        check_active()
        if verify_admission() is not True:
            raise ValueError('SEMANTIC_PREVIEW_ADOPTION_DENIED')
        check_active()
    admitted()
    with artifacts.input(image.pixels,suffix='.npy') as source:
        size=source.path.stat().st_size
        if not 1<=size<=maximum:raise ValueError('SEMANTIC_PREVIEW_ADOPTION_INVALID')
        with read_array(source.path,shape,'float32',check_active,unit_interval=False) as values:
            for y in range(0,shape[0],256):
                for x in range(0,shape[1],256):
                    check_active()
                    if np.any(values[y:y+256,x:x+256]<0):raise ValueError('SEMANTIC_PREVIEW_ADOPTION_INVALID')
        admitted()
        with artifacts.scratch.reserve(size) as reservation:
            path=reservation.path('shown-preview-pixels','.npy');digest=sha256();copied=0
            with source.path.open('rb') as reader,artifacts.scratch.open_writer(path,reservation) as writer:
                while block:=reader.read(65536):
                    check_active();copied+=len(block)
                    if copied>size:raise ValueError('SEMANTIC_PREVIEW_ADOPTION_INVALID')
                    writer.write(block);digest.update(block)
            reservation.validate_file(path)
            if copied!=size or digest.hexdigest()!=image.pixels.sha256:
                raise ValueError('SEMANTIC_PREVIEW_ADOPTION_INVALID')
            admitted()
            pixels=ArtifactRef.model_validate_json(canonical_bytes(artifacts.stage(path,reservation,'pixels','application/x-npy')))
            if pixels.sha256!=image.pixels.sha256 or pixels.artifact_id.int==0 or pixels.artifact_id in (ref.artifact_id,image.pixels.artifact_id, image.alpha.artifact_id if image.alpha else ref.artifact_id):
                raise ValueError('SEMANTIC_PREVIEW_ADOPTION_INVALID')
    result=WorkingImage.model_validate_json(canonical_bytes(image.model_copy(update={'pixels':pixels})))
    raw=canonical_bytes(result)
    if size+len(raw)>max_output_bytes:raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    admitted()
    staged=ArtifactRef.model_validate_json(canonical_bytes(artifacts.stage_contract(result,'working_image')))
    if staged.artifact_id.int==0 or staged.artifact_id in (ref.artifact_id,image.pixels.artifact_id,pixels.artifact_id, image.alpha.artifact_id if image.alpha else ref.artifact_id) or staged.sha256!=sha256(raw).hexdigest() or canonical_bytes(result)!=raw:
        raise ValueError('SEMANTIC_PREVIEW_ADOPTION_INVALID')
    admitted()
    return StagedExposure(pixels,staged,size+len(raw))
