"""Qualification-bound preparation; publication and SQL adoption are separate."""
from contextlib import contextmanager
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.optical_multi import OpticalPanoramaParams,OpticalFocusStackParams
from photo_worker.runtime.executor import OperationFailure
from .multi_request import prepare_frozen_multi_request


@contextmanager
def prepare_admitted_multi(ctx,admission,params,images,rgbs,alphas,scratch,*,ram_max_bytes,tile=256):
    """Own metadata, admit before processing, and recheck before handing off.

    Trusted adapter must load exact versioned pixel objects and source closure.
    Caller must keep this context alive through result staging and revalidate
    SQL authority at adoption. No operation is enabled by this helper.
    """
    model=OpticalPanoramaParams if admission.operation=='optical.panorama' else OpticalFocusStackParams
    owned=model.model_validate_json(canonical_bytes(params))
    sources=tuple(WorkingImage.model_validate_json(canonical_bytes(image)) for image in images)
    request=admission.admit(ctx,owned,sources)
    estimate=ctx.lease.execution_profile.get('resource_estimate',{})
    reserved=estimate.get('ram_bytes')
    if type(ram_max_bytes) is not int or ram_max_bytes<=0 or type(reserved) is not int or ram_max_bytes>reserved:
        raise OperationFailure('QUALIFICATION_RESOURCE_EXCEEDED')
    with prepare_frozen_multi_request(request,sources,rgbs,alphas,scratch,
            verify_icc=admission.verify_color,ram_max_bytes=ram_max_bytes,tile=tile,
            check_active=ctx.check_active) as prepared:
        current=admission.admit(ctx,owned,sources)
        if current!=request:raise OperationFailure('MULTI_QUALIFICATION_SCOPE_STALE')
        ctx.check_active()
        yield prepared
