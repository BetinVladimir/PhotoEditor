"""Stage a deterministic composed layer closure without losing source geometry."""
from dataclasses import dataclass
from photo_worker.contracts.base import ArtifactRef,canonical_hash,canonical_bytes
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.operations.optical.contracts import ImageResult,ImageReport
from .prepare import LayerFiles
from .reports import parent_report_evidence

@dataclass(frozen=True)
class StagedLayerResult:
    document:ArtifactRef
    outputs:tuple[ArtifactRef,...]
    bytes_written:int
    review_required:bool


def stage_layer_result(artifacts,source_ref:ArtifactRef,source:WorkingImage,binding:VersionBinding,
                       files:LayerFiles,*,proposal_result:ImageResult,source_result:ImageResult|None=None)->StagedLayerResult:
    """Preparation must have proved same canvas/chain/color and exact protection.

    Unchanged pixels/alpha retain original refs independently. No new geometry,
    generated-pixel claim or photographic identity verification is manufactured.
    The graph/adopter must verify the actual operation and referenced closure.
    """
    if (binding.source!=source_ref or binding.canvas_hash!=canonical_hash(source.canvas) or source.dtype!='float32' or
            files.any_changed!=(files.rgb_changed or files.alpha_changed) or (files.alpha_changed or files.alpha_converted) and files.alpha is None or
            type(files.alpha_converted) is not bool or files.alpha_converted and source.alpha is None):
        raise ValueError('LAYER_RESULT_INVALID')
    if (not isinstance(proposal_result,ImageResult) or source_result is not None and
            (not isinstance(source_result,ImageResult) or source_result.working_image!=source_ref)):
        raise ValueError('LAYER_PARENT_INVALID')
    source_generated=source_result.generated_mask if source_result is not None else None
    proposal_generated=proposal_result.generated_mask
    has_generated=source_generated is not None or proposal_generated is not None
    if (files.source_generated_ref!=source_generated or files.proposal_generated_ref!=proposal_generated or
            (files.generated_mask is not None)!=has_generated):
        raise ValueError('LAYER_GENERATED_BINDING_INVALID')
    evidence=parent_report_evidence(artifacts,source_result,proposal_result,binding)
    review_required=evidence.review_required
    files.reservation.validate()
    outputs=[];written=0
    def stage(path,role):
        nonlocal written
        files.reservation.validate_file(path)
        ref=artifacts.stage(path,files.reservation,role,'application/x-npy')
        outputs.append(ref);written+=path.stat().st_size;return ref
    def contract(value,role):
        nonlocal written
        ref=artifacts.stage_contract(value,role);outputs.append(ref);written+=len(canonical_bytes(value));return ref
    pixels=stage(files.rgb,'pixels') if files.rgb_changed else source.pixels
    alpha=stage(files.alpha,'alpha') if files.alpha_changed or files.alpha_converted else source.alpha
    image=WorkingImage(pixels=pixels,alpha=alpha,dtype=source.dtype,canvas=source.canvas,color=source.color,transform_chain=source.transform_chain)
    image_ref=contract(image,'working_image') if files.any_changed or files.alpha_converted else source_ref
    changed_pixels=stage(files.changed_mask,'mask_pixels')
    changed=contract(MaskRef(artifact=changed_pixels,canvas=image.canvas,transform_chain=image.transform_chain,
        selector='changed',confidence=1.,model_lock_id=None,protected=False),'mask')
    generated=None
    if has_generated:
        generated_pixels=stage(files.generated_mask,'mask_pixels')
        generated=contract(MaskRef(artifact=generated_pixels,canvas=image.canvas,transform_chain=image.transform_chain,
            selector='generated',confidence=1.,model_lock_id=None,protected=False),'mask')
    report=contract(ImageReport(schema_version=1,status='review' if review_required else 'pass',operation='layer.compose',skipped_count=0,
        reason='LAYER_PARENT_REVIEW' if review_required else None,
        outside_mask_max_error=0.,clipping_fraction=0.,identity=None,transform=None,sources=[binding],registrations=[],parent_reports=list(evidence.reports)),'report')
    document=contract(ImageResult(schema_version=1,working_image=image_ref,changed_mask=changed,report=report,generated_mask=generated),'document')
    return StagedLayerResult(document,tuple(outputs),written,review_required)
