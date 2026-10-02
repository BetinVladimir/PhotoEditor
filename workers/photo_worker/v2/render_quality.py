"""Join measured final quality with admitted source/base/layer report roots."""
from dataclasses import dataclass
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.optical.contracts import ImageReport
from .render_reports import RenderReportEvidence,collect_render_parent_reports


@dataclass(frozen=True)
class RenderQuality:
    report:ImageReport
    parents:RenderReportEvidence


def build_render_quality(artifacts,measured,parents,binding,*,check_active=lambda:None):
    """Caller proves complete parent ImageResult inventory and measures metrics.

    Preserves measured identity, clipping, transforms and reason. It cannot
    overwrite a provider-supplied parent list or salvage fail/unavailable output.
    An inherited review only adds a reason if measured quality has none.
    """
    check_active()
    if not isinstance(measured,ImageReport):raise ValueError('RENDER_QUALITY_INPUT_INVALID')
    report=ImageReport.model_validate_json(canonical_bytes(measured))
    if (report.status not in ('pass','review') or binding not in report.sources
            or any(source.asset_version_id==binding.asset_version_id and source!=binding for source in report.sources)):
        raise ValueError('RENDER_QUALITY_MEASURED_INVALID')
    evidence=collect_render_parent_reports(artifacts,parents,binding,check_active=check_active)
    if report.parent_reports is not None and tuple(report.parent_reports)!=evidence.reports:
        raise ValueError('RENDER_QUALITY_PARENT_INVENTORY_CHANGED')
    body=report.model_dump(mode='json');body['parent_reports']=[ref.model_dump(mode='json') for ref in evidence.reports]
    if evidence.review_required:
        body['status']='review'
        if body['reason'] is None:body['reason']='RENDER_PARENT_REVIEW'
    result=ImageReport.model_validate_json(canonical_bytes(body));check_active()
    return RenderQuality(result,evidence)
