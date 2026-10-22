"""Private artifact-backed retouch orchestration; registration is separate."""
from contextlib import contextmanager
from copy import deepcopy
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import WorkingImage
from .preview_retouch_context_loader import load_retouch_preview_face_context
from .preview_retouch_loader import load_retouch_preview_arrays,load_retouch_preview_alpha
from .preview_retouch_compute import compute_retouch_preview
from .preview_retouch_inputs import retouch_declared_inputs


@contextmanager
def execute_artifact_retouch_preview(ctx,artifacts,params,context_ref,*,verify_admission,
        verify_qualification,verify_face_origin,verify_protection_origin,verify_color,max_ram_bytes):
    if not all(callable(fn) for fn in (verify_admission,verify_qualification,verify_face_origin,
            verify_protection_origin,verify_color)):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    context_ref=ArtifactRef.model_validate_json(canonical_bytes(context_ref))
    lease=deepcopy(ctx.lease)
    def active():
        ctx.check_active()
        if ctx.lease!=lease:raise ValueError('SEMANTIC_RETOUCH_LEASE_CHANGED')
    active()
    inputs=retouch_declared_inputs(artifacts,check_active=active)
    declared={ref.artifact_id:ref.sha256 for ref in inputs}
    def required(ref):
        if declared.get(ref.artifact_id)!=ref.sha256:raise ValueError('SEMANTIC_RETOUCH_UNDECLARED_INPUT')
    class DeclaredArtifacts:
        def declared_input_refs(self,maximum=512):
            active()
            if len(inputs)>maximum:raise ValueError('SEMANTIC_RETOUCH_INPUTS_INVALID')
            return tuple(ref.model_copy(deep=True) for ref in inputs)
        def contract_bytes(self,ref,model):
            required(ref);active();raw=artifacts.contract_bytes(ref.model_copy(deep=True),model);active();return raw
        @contextmanager
        def input(self,ref,**kwargs):
            required(ref);active()
            with artifacts.input(ref.model_copy(deep=True),**kwargs) as local:
                active();yield local;active()
    scoped=DeclaredArtifacts()
    owned,candidate,intent,context,image,faces=load_retouch_preview_face_context(ctx,scoped,params,context_ref,
        verify_admission=verify_admission,verify_qualification=verify_qualification,verify_face_origin=verify_face_origin)
    def admitted(value=None):
        active()
        if value is not None and canonical_bytes(value)!=canonical_bytes(owned):
            raise ValueError('SEMANTIC_RETOUCH_COMPUTE_BINDING_INVALID')
        accepted=verify_admission(deepcopy(lease),owned.model_copy(deep=True))
        active()
        if accepted is not True:raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
        return True
    def qualified():
        admitted()
        accepted=verify_qualification(deepcopy(lease),owned.model_copy(deep=True),
            deepcopy(context_ref),canonical_bytes(context))
        admitted()
        if accepted is not True:raise ValueError('SEMANTIC_RETOUCH_QUALIFICATION_DENIED')
    qualified()
    arrays=load_retouch_preview_arrays(scoped,owned,verify_admission=admitted,
        verify_origin=verify_protection_origin,verify_color=verify_color,max_bytes=max_ram_bytes,check_active=admitted)
    qualified()
    source_raw=scoped.contract_bytes(owned.source.source,WorkingImage)
    qualified()
    resident=arrays.original.nbytes+arrays.mask.nbytes+arrays.protected.nbytes
    alpha=load_retouch_preview_alpha(scoped,owned,source_raw,verify_admission=admitted,
        max_bytes=max_ram_bytes-resident,check_active=admitted)
    qualified()
    def numeric_admission(value,binding):
        if canonical_bytes(binding)!=canonical_bytes(context):return False
        return admitted(value)
    with compute_retouch_preview(owned,context,image,arrays.original,arrays.mask,arrays.protected,
            faces,artifacts.scratch,strength=intent.strength,max_ram_bytes=max_ram_bytes,
            verify_admission=numeric_admission,check_active=admitted,alpha=alpha) as computed:
        qualified()
        yield computed
        qualified()
    qualified()
