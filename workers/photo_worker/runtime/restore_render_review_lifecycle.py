"""Native render review decision followed by independent SQL confirmation."""
from dataclasses import dataclass
from time import monotonic
from uuid import UUID
from .restore_batch_adoption import RestoreBatchAdoptionEvidence
from .restore_batch_physical import RestoreBatchPhysicalEvidence
from .restore_render_review_pending import find_fixture_pending_render_review
from .restore_render_review_decide import (RestoreRenderReviewTarget,
    request_fixture_render_review_decision,validate_fixture_render_review_decision)
from .restore_render_review import observe_fixture_render_review
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreCompletedRenderReview:
    pending: object
    decision: object
    observed: object


def run_fixture_render_review(connection,tenant_id,actor_id,adoption,physical,*,
                              origin,authorization,tls_context,maximum_seconds,
                              assert_installed_fixture,check_active):
    """No retry after POST: an unknown outcome requires external reconciliation."""
    if (not isinstance(tenant_id,UUID) or tenant_id.int==0
            or not isinstance(actor_id,UUID) or actor_id.int==0
            or not isinstance(adoption,RestoreBatchAdoptionEvidence)
            or not isinstance(physical,RestoreBatchPhysicalEvidence)
            or type(maximum_seconds) is not int or not 1<=maximum_seconds<=300
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_RENDER_REVIEW_LIFECYCLE_INSTALLATION')
    deadline=monotonic()+maximum_seconds
    def guard():
        require_restore_fixture(check_active,assert_installed_fixture)
        if monotonic()>=deadline:raise ValueError('RESTORE_RENDER_REVIEW_TIMEOUT')
    guard()
    pending=find_fixture_pending_render_review(connection,tenant_id,adoption,physical,
        assert_fixture_scope=guard,check_active=guard)
    if (pending.database!=adoption.database or pending.tenant_id!=tenant_id
            or pending.child_run_id!=adoption.child_run_id
            or pending.candidate!=physical.image_result.working_image):
        raise ValueError('RESTORE_RENDER_REVIEW_PENDING_CHANGED')
    target=RestoreRenderReviewTarget(origin,tenant_id,pending.child_run_id,pending.review_id,
                                     pending.revision,pending.candidate,authorization)
    validate_fixture_render_review_decision(target,tls_context=tls_context,
        assert_fixture_scope=guard,check_active=guard)
    guard()
    decision=request_fixture_render_review_decision(target,tls_context=tls_context,
        assert_fixture_scope=guard,check_active=guard)
    guard()
    observed=observe_fixture_render_review(connection,tenant_id,actor_id,adoption,physical,
        assert_fixture_scope=guard,check_active=guard)
    if (observed.review_id!=pending.review_id or observed.task_id!=pending.task_id
            or observed.candidate!=pending.candidate
            or observed.revision!=pending.revision+1
            or decision.review_id!=observed.review_id or decision.revision!=observed.revision):
        raise ValueError('RESTORE_RENDER_REVIEW_COMMIT_CHANGED')
    guard()
    return RestoreCompletedRenderReview(pending,decision,observed)
