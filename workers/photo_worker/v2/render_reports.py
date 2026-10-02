"""Preserve every accepted parent report and review gate for full composition."""
from dataclasses import dataclass
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.optical.contracts import ImageReport

@dataclass(frozen=True)
class RenderReportEvidence:
    reports:tuple[ArtifactRef,...]
    review_required:bool


def collect_render_parent_reports(artifacts,parents,binding,*,max_reports=1024,max_depth=32,check_active=lambda:None)->RenderReportEvidence:
    """Validate bounded report DAG; caller proves ImageResult/byte authorization.

    Retains ordered direct roots, validates every ancestor, inherits every review
    gate, and rejects cycles or contradictory artifact pins without recursion.
    """
    if (not isinstance(parents,tuple) or len(parents)>33 or type(max_reports) is not int
            or not 1<=max_reports<=1024 or type(max_depth) is not int or not 1<=max_depth<=32):
        raise ValueError('RENDER_REPORT_INVENTORY_INVALID')
    references=[];roots=set();pins={};states={};heights={};edges={};review=False
    for parent in parents:
        check_active()
        if parent is None:continue
        root=parent.report
        if root.artifact_id not in roots:
            references.append(root.model_copy(deep=True));roots.add(root.artifact_id)
        pending=[(root,1,False)]
        while pending:
            check_active();ref,depth,leaving=pending.pop();key=ref.artifact_id
            if key in pins and pins[key]!=ref.sha256:raise ValueError('RENDER_REPORT_PIN_CHANGED')
            pins[key]=ref.sha256
            if leaving:
                heights[key]=1+max((heights[child.artifact_id] for child in edges[key]),default=0)
                states[key]=2;continue
            if depth>max_depth:raise ValueError('RENDER_REPORT_DEPTH_BUDGET')
            if states.get(key)==1:raise ValueError('RENDER_REPORT_CYCLE')
            if states.get(key)==2:
                if depth+heights[key]-1>max_depth:raise ValueError('RENDER_REPORT_DEPTH_BUDGET')
                continue
            if len(states)>=max_reports:raise ValueError('RENDER_REPORT_NODE_BUDGET')
            report=artifacts.contract(ref,ImageReport);check_active()
            if binding not in report.sources or any(x.asset_version_id==binding.asset_version_id and x!=binding for x in report.sources):
                raise ValueError('RENDER_REPORT_SOURCE_CHANGED')
            if report.status in ('fail','unavailable'):raise ValueError('RENDER_PARENT_REPORT_REJECTED')
            review=review or report.status=='review';states[key]=1
            edges[key]=report.parent_reports or []
            pending.append((ref,depth,True))
            for ancestor in reversed(report.parent_reports or []):pending.append((ancestor,depth+1,False))
    check_active();return RenderReportEvidence(tuple(references),review)
