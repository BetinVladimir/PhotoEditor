"""Preserve exact source binding and all ancestor review gates."""
from photo_worker.v2.render_reports import collect_render_parent_reports


def parent_report_evidence(artifacts,source_result,proposal_result,binding):
    try:
        return collect_render_parent_reports(artifacts,(source_result,proposal_result),binding)
    except ValueError as error:
        code=str(error)
        if code=='RENDER_REPORT_SOURCE_CHANGED':
            raise ValueError('LAYER_PARENT_REPORT_SOURCE_INVALID') from error
        if code=='RENDER_PARENT_REPORT_REJECTED':
            raise ValueError('LAYER_PARENT_REPORT_REJECTED') from error
        raise


def parent_review(artifacts,source_result,proposal_result,binding):
    return parent_report_evidence(artifacts,source_result,proposal_result,binding).review_required
