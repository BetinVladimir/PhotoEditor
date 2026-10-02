"""Stage complete ImageResult roots with caller-measured quality evidence."""
from dataclasses import dataclass
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.optical.contracts import ImageResult,ImageReport
from .render_stage import StagedRenderImage
from .render_reports import RenderReportEvidence


@dataclass(frozen=True)
class StagedRenderResult:
    document:ArtifactRef
    outputs:tuple[ArtifactRef,...]
    bytes_written:int
    review_required:bool


def stage_render_result(artifacts,image,report,binding,parent_evidence,*,check_active=lambda:None):
    """Caller measures quality and proves complete provider/source inventory.

    This does not manufacture clipping/identity metrics or mark failed outputs
    usable. Direct parent roots must exactly match verified evidence, retaining
    order and pins. Graph adoption independently proves the full typed closure.
    """
    check_active()
    if (not isinstance(image,StagedRenderImage) or not isinstance(report,ImageReport)
            or not isinstance(parent_evidence,RenderReportEvidence)):
        raise ValueError('RENDER_RESULT_INPUT_INVALID')
    # Revalidate even if a provider used model_copy(update=...) without validation.
    report=ImageReport.model_validate_json(canonical_bytes(report))
    if (binding not in report.sources or any(source.asset_version_id==binding.asset_version_id and source!=binding for source in report.sources)
            or tuple(report.parent_reports or ())!=parent_evidence.reports
            or report.status not in ('pass','review')
            or parent_evidence.review_required and report.status!='review'):
        raise ValueError('RENDER_RESULT_QUALITY_INVALID')
    outputs=list(image.outputs);written=image.bytes_written
    def stage(value,role):
        nonlocal written
        check_active();reference=artifacts.stage_contract(value,role);check_active()
        outputs.append(reference);written+=len(canonical_bytes(value));return reference
    report_ref=stage(report,'report')
    document=stage(ImageResult(schema_version=1,working_image=image.working_image,report=report_ref,
        changed_mask=image.changed_mask,generated_mask=image.generated_mask),'document')
    return StagedRenderResult(document,tuple(outputs),written,report.status=='review')
