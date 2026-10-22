"""Attempt-scoped staging of the four already validated preview media."""
from dataclasses import dataclass
from hashlib import sha256
import numpy as np
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from .preview_presentation import ExposureDisplay, write_exposure_display
from .preview_overlay import write_preview_mask_overlay


@dataclass(frozen=True)
class StagedPreviewMedia:
    before: ArtifactRef
    after: ArtifactRef
    affected: ArtifactRef
    protected: ArtifactRef
    bytes_written: int


def stage_exposure_preview_media(artifacts, *, before, after, affected, protected,
        forbidden_ids, srgb_sha, max_bytes, max_output_bytes, verify_runtime,
        verify_admission, check_active):
    """No receipt/quality authority: caller must bind arrays to canonical evidence.

    Each object is independently attempt-staged. Failed later publication is
    cleaned by the normal attempt lifecycle; no partial media bundle is returned.
    """
    if (not all(callable(fn) for fn in (verify_runtime,verify_admission,check_active))
            or not isinstance(before,ExposureDisplay) or not isinstance(after,ExposureDisplay)
            or type(max_bytes) is not int or not 1<=max_bytes<=2**53-1
            or type(max_output_bytes) is not int or not 1<=max_output_bytes<=2**53-1):
        raise ValueError('SEMANTIC_PREVIEW_MEDIA_INVALID')
    for display in (before,after):
        if (not isinstance(display.rgb,np.ndarray) or display.rgb.ndim!=3
                or display.rgb.shape[2]!=3 or display.rgb.dtype!=np.float32
                or not isinstance(display.profile,bytes) or not 1<=len(display.profile)<=1048576
                or display.profile_sha256!=srgb_sha or sha256(display.profile).hexdigest()!=srgb_sha):
            raise ValueError('SEMANTIC_PREVIEW_MEDIA_INVALID')
    shape=before.rgb.shape[:2]
    if (not 1<=shape[0]<=4096 or not 1<=shape[1]<=4096 or after.rgb.shape[:2]!=shape
            or any(not isinstance(value,np.ndarray) or value.dtype!=np.uint8
                or value.shape!=(*shape,4) for value in (affected,protected))):
        raise ValueError('SEMANTIC_PREVIEW_MEDIA_INVALID')
    pixels=shape[0]*shape[1]
    resident=before.rgb.nbytes+after.rgb.nbytes+affected.nbytes+protected.nbytes
    for display in (before,after):
        if display.alpha is not None:
            if (not isinstance(display.alpha,np.ndarray) or display.alpha.dtype!=np.float32
                    or display.alpha.shape!=shape):
                raise ValueError('SEMANTIC_PREVIEW_MEDIA_INVALID')
            resident+=display.alpha.nbytes
    bounds=[5*pixels+len(display.profile)+65536 for display in (before,after)]+[5*pixels+65536]*2
    if max_output_bytes<sum(bounds) or max_bytes<resident+64*pixels+16*1024**2:
        raise ValueError('SEMANTIC_PREVIEW_MEDIA_BUDGET')
    seen=set(forbidden_ids);refs=[];written=0
    def admitted():
        check_active()
        if verify_admission() is not True or verify_runtime() is not True:
            raise ValueError('SEMANTIC_PREVIEW_MEDIA_ADMISSION_DENIED')
        check_active()
    admitted()
    for index,(role,value,bound) in enumerate(zip(('before','after','affected','protected'),
            (before,after,affected,protected),bounds)):
        admitted()
        with artifacts.scratch.reserve(bound) as reservation:
            path=reservation.path('semantic-preview-'+role,'.png')
            if index<2:
                size=write_exposure_display(path,value,srgb_sha=srgb_sha,max_bytes=max_bytes-resident,
                    max_output_bytes=bound,verify_runtime=verify_runtime,check_active=check_active)
            else:
                size=write_preview_mask_overlay(path,value,max_bytes=max_bytes-resident,
                    max_output_bytes=bound,check_active=check_active)
            reservation.validate_file(path)
            digest=sha256()
            with path.open('rb') as stream:
                while block:=stream.read(65536):
                    check_active();digest.update(block)
            admitted()
            ref=ArtifactRef.model_validate_json(canonical_bytes(artifacts.stage(path,reservation,role,'image/png')))
            if ref.artifact_id.int==0 or ref.artifact_id in seen or ref.sha256!=digest.hexdigest():
                raise ValueError('SEMANTIC_PREVIEW_MEDIA_OUTPUT_INVALID')
            seen.add(ref.artifact_id);refs.append(ref);written+=size
            if written>max_output_bytes:raise ValueError('SEMANTIC_PREVIEW_MEDIA_BUDGET')
        admitted()
    return StagedPreviewMedia(*refs,written)
