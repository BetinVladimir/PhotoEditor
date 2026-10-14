"""Stage complete negative output closure; atomic adoption is separate."""
from dataclasses import dataclass
from uuid import uuid4
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import MaskRef
from .contracts import ImageResult
from .negative_prepare import NegativeFiles
from .negative_result import build_negative_metadata


@dataclass(frozen=True)
class StagedNegativeResult:
    document: ArtifactRef
    outputs: tuple[ArtifactRef,...]
    review_required: bool
    bytes_written: int


def stage_negative_result(artifacts,params,source_raw,files,*,clipping_fraction,check_active):
    if not isinstance(files,NegativeFiles) or not callable(check_active):
        raise ValueError('NEGATIVE_STAGING_INVALID')
    if clipping_fraction!=files.clipping_fraction:
        raise ValueError('NEGATIVE_STATISTICS_MISMATCH')
    # Validate source geometry/report before invoking a publisher callback.
    build_negative_metadata(params,source_raw,ArtifactRef(artifact_id=uuid4(),sha256='a'*64),
        clipping_fraction=clipping_fraction)
    files.reservation.validate_file(files.rgb)
    files.reservation.validate_file(files.changed_mask)
    outputs=[];written=0
    def stage(path,role):
        nonlocal written
        check_active();ref=artifacts.stage(path,files.reservation,role,'application/x-npy')
        outputs.append(ref);written+=path.stat().st_size;check_active();return ref
    def document(value,role):
        nonlocal written
        check_active();ref=artifacts.stage_contract(value,role)
        outputs.append(ref);written+=len(canonical_bytes(value));check_active();return ref
    pixels=stage(files.rgb,'pixels')
    image,report=build_negative_metadata(params,source_raw,pixels,clipping_fraction=clipping_fraction)
    image_ref=document(image,'working_image')
    mask_pixels=stage(files.changed_mask,'mask_pixels')
    mask=MaskRef(artifact=mask_pixels,canvas=image.canvas,transform_chain=image.transform_chain,
        object_id=None,selector='changed',confidence=1.,model_lock_id=None,protected=False)
    mask_ref=document(mask,'mask');report_ref=document(report,'report')
    root=document(ImageResult(schema_version=1,working_image=image_ref,report=report_ref,
        changed_mask=mask_ref,generated_mask=None),'document')
    if (any(r.artifact_id.int==0 or r.sha256=='0'*64 for r in outputs)
            or len({r.artifact_id for r in outputs})!=6):raise ValueError('NEGATIVE_STAGING_INVALID')
    check_active();return StagedNegativeResult(root,tuple(outputs),True,written)
