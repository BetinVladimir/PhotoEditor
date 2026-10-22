"""Pre-Apply proposal inputs; never a human approval or child-version receipt."""
from typing import Annotated, Literal
from uuid import UUID
from pydantic import Field, model_validator
from .base import ArtifactRef, Contract, SHA256
from .copy_fence import CopyFence
from photo_worker.operations.session.contracts import VersionBinding

Revision = Annotated[str, Field(pattern=r'^[1-9][0-9]{0,18}$')]

class SemanticProposalPreviewInput(Contract):
    schema_version: Annotated[int,Field(strict=True,ge=1,le=1)]
    tenant_id: UUID
    intent_id: UUID
    target_id: UUID
    workflow_id: UUID
    workflow_revision: Revision
    target_revision: Revision
    snapshot_id: UUID
    plan: ArtifactRef
    intent: ArtifactRef
    candidate: ArtifactRef
    source: VersionBinding
    protection: ArtifactRef
    model_lock_sha: SHA256
    runtime_lock_sha: SHA256
    device_profile_sha: SHA256

    @model_validator(mode='after')
    def bindings(self):
        ids = (self.tenant_id,self.intent_id,self.target_id,self.workflow_id,
               self.snapshot_id,self.source.asset_version_id)
        refs = (self.plan,self.intent,self.candidate,self.source.source,self.protection)
        if (any(value.int == 0 for value in ids)
                or any(ref.artifact_id.int == 0 for ref in refs)
                or len({ref.artifact_id for ref in refs}) != len(refs)
                or any(int(value) >= 2**63-1 for value in (self.workflow_revision,self.target_revision))):
            raise ValueError('SEMANTIC_PREVIEW_BINDING_INVALID')
        return self


class SemanticExposurePreviewInput(SemanticProposalPreviewInput):
    """Existing exposure wire format; no additional fields."""
    pass


class SemanticRemovalPreviewInput(SemanticProposalPreviewInput):
    """Proposed object-removal computation, never a confirmed InpaintParams."""
    kind: Literal['remove_object']
    model_id: Literal['big_lama']
    context_pixels: Annotated[int,Field(strict=True,ge=32,le=512)]
    feather_pixels: Annotated[int,Field(strict=True,ge=1,le=64)]


class SemanticBackgroundCleanupPreviewInput(SemanticProposalPreviewInput):
    """Background-only proposal; no approval and no removal-kind coercion."""
    kind: Literal['background_cleanup']
    model_id: Literal['big_lama']
    context_pixels: Annotated[int, Field(strict=True, ge=32, le=512)]
    feather_pixels: Annotated[int, Field(strict=True, ge=1, le=64)]


class SemanticRetouchPreviewInput(SemanticProposalPreviewInput):
    """Region-retouch proposal; downstream qualification owns the retouch adapter.

    Candidate/intent pin the same transfer mask and strength. Inpaint model and
    context/feather controls are not part of this operation's wire contract.
    """
    kind: Literal['retouch_region']


class ExposurePreviewNumericalEvidence(Contract):
    """Exact compute evidence, not photographic qualification or Apply authority."""
    schema_version: Annotated[int,Field(strict=True,ge=1,le=1)]
    params: SemanticExposurePreviewInput
    working_image: ArtifactRef
    pixels: ArtifactRef
    algorithm: Literal['local_exposure_exact_replay_v1']
    protected_pixels_unchanged: Literal[True]
    outside_mask_max_error: Annotated[float,Field(ge=0,le=0)]

    @model_validator(mode='before')
    @classmethod
    def strict_protection(cls,value):
        if isinstance(value,dict) and value.get('protected_pixels_unchanged') is not True:
            raise ValueError('SEMANTIC_PREVIEW_PROTECTION_INVALID')
        return value

    @model_validator(mode='after')
    def distinct_outputs(self):
        inputs={ref.artifact_id for ref in (self.params.plan,self.params.intent,
            self.params.candidate,self.params.source.source,self.params.protection)}
        if (self.working_image.artifact_id.int==0 or self.pixels.artifact_id.int==0
                or self.working_image.artifact_id==self.pixels.artifact_id
                or self.working_image.artifact_id in inputs or self.pixels.artifact_id in inputs):
            raise ValueError('SEMANTIC_PREVIEW_ARTIFACT_ALIAS')
        return self


class ExposurePreviewPresentation(Contract):
    """Complete display linkage, not photo qualification or an Apply receipt."""
    schema_version: Annotated[int,Field(strict=True,ge=1,le=1)]
    params: SemanticExposurePreviewInput
    numerical_evidence: ArtifactRef
    working_image: ArtifactRef
    pixels: ArtifactRef
    before: ArtifactRef
    after: ArtifactRef
    affected: ArtifactRef
    protected: ArtifactRef
    width: Annotated[int,Field(strict=True,ge=1,le=4096)]
    height: Annotated[int,Field(strict=True,ge=1,le=4096)]
    icc_lock_sha: SHA256
    srgb_sha: SHA256
    resize: Literal['area_linear_premult_v1']
    overlay: Literal['max_coverage_ceil_alpha_v1']

    @model_validator(mode='after')
    def distinct_artifacts(self):
        refs=(self.params.plan,self.params.intent,self.params.candidate,
            self.params.source.source,self.params.protection,self.numerical_evidence,
            self.working_image,self.pixels,self.before,self.after,self.affected,self.protected)
        if any(ref.artifact_id.int==0 for ref in refs) or len({ref.artifact_id for ref in refs})!=len(refs):
            raise ValueError('SEMANTIC_PREVIEW_PRESENTATION_ALIAS')
        return self


class ShownExposureAdoptionInput(Contract):
    """Frozen copy task inputs; SQL must prove human decision and receipt origin."""
    schema_version: Annotated[int,Field(strict=True,ge=1,le=1)]
    tenant_id: UUID
    child_run_id: UUID
    target_id: UUID
    approved_edit: ArtifactRef
    preview_run_id: UUID
    preview_task_id: UUID
    preview_result_id: UUID
    preview_attempt: Annotated[int,Field(strict=True,ge=1,le=2147483647)]
    preview_fence: CopyFence
    presentation: ArtifactRef
    numerical_evidence: ArtifactRef
    working_image: ArtifactRef
    pixels: ArtifactRef
    model_lock_sha: SHA256
    runtime_lock_sha: SHA256
    device_profile_sha: SHA256

    @model_validator(mode='after')
    def bindings(self):
        from uuid import uuid5
        ids=(self.tenant_id,self.child_run_id,self.target_id,self.preview_run_id,
             self.preview_task_id,self.preview_result_id)
        refs=(self.approved_edit,self.presentation,self.numerical_evidence,self.working_image,self.pixels)
        all_ids=(*ids,*(ref.artifact_id for ref in refs))
        if (any(value.int==0 for value in all_ids) or len(set(all_ids))!=len(all_ids)
                or self.preview_task_id!=uuid5(self.preview_run_id,'semantic.preview_local_exposure:'+str(self.target_id))):
            raise ValueError('SEMANTIC_PREVIEW_ADOPTION_BINDING_INVALID')
        return self


class RemovalPreviewNumericalEvidence(Contract):
    """Preservation evidence only; no claim of photographic qualification."""
    schema_version: Annotated[int, Field(strict=True, ge=1, le=1)]
    params: SemanticRemovalPreviewInput
    working_image: ArtifactRef
    pixels: ArtifactRef
    algorithm: Literal['big_lama_work_srgb_delta_v1']
    changed_pixels: Annotated[int, Field(strict=True, ge=0, le=200000000)]
    protected_pixels_unchanged: Literal[True]
    outside_mask_unchanged: Literal[True]
    global_pass: Annotated[bool, Field(strict=True)]
    roi: Annotated[list[Annotated[int, Field(strict=True, ge=0, le=9007199254740991)]], Field(min_length=4, max_length=4)]

    @model_validator(mode='before')
    @classmethod
    def strict_proof(cls, value):
        if isinstance(value, dict) and any(value.get(k) is not True for k in
                ('protected_pixels_unchanged', 'outside_mask_unchanged')):
            raise ValueError('SEMANTIC_PREVIEW_PROTECTION_INVALID')
        return value

    @model_validator(mode='after')
    def output_binding(self):
        refs = (self.params.plan, self.params.intent, self.params.candidate,
            self.params.source.source, self.params.protection, self.working_image, self.pixels)
        if any(r.artifact_id.int == 0 for r in refs) or len({r.artifact_id for r in refs}) != len(refs):
            raise ValueError('SEMANTIC_PREVIEW_ARTIFACT_ALIAS')
        x0, y0, x1, y1 = self.roi
        if x1 <= x0 or y1 <= y0 or self.changed_pixels > (x1-x0)*(y1-y0):
            raise ValueError('SEMANTIC_PREVIEW_ROI_INVALID')
        return self


class RemovalPreviewPresentation(ExposurePreviewPresentation):
    """Same display linkage rules, with removal-specific input/evidence type."""
    params: SemanticRemovalPreviewInput
    width: Annotated[int, Field(strict=True, ge=1, le=2560)]
    height: Annotated[int, Field(strict=True, ge=1, le=2560)]


class ShownRemovalAdoptionInput(Contract):
    """Frozen copy task inputs; SQL must prove human decision and receipt origin."""
    schema_version: Annotated[int,Field(strict=True,ge=1,le=1)]
    tenant_id: UUID
    child_run_id: UUID
    target_id: UUID
    approved_edit: ArtifactRef
    preview_run_id: UUID
    preview_task_id: UUID
    preview_result_id: UUID
    preview_attempt: Annotated[int,Field(strict=True,ge=1,le=2147483647)]
    preview_fence: CopyFence
    presentation: ArtifactRef
    numerical_evidence: ArtifactRef
    working_image: ArtifactRef
    pixels: ArtifactRef
    model_lock_sha: SHA256
    runtime_lock_sha: SHA256
    device_profile_sha: SHA256

    @model_validator(mode='after')
    def bindings(self):
        from uuid import uuid5
        ids=(self.tenant_id,self.child_run_id,self.target_id,self.preview_run_id,
             self.preview_task_id,self.preview_result_id)
        refs=(self.approved_edit,self.presentation,self.numerical_evidence,self.working_image,self.pixels)
        all_ids=(*ids,*(ref.artifact_id for ref in refs))
        if (any(value.int==0 for value in all_ids) or len(set(all_ids))!=len(all_ids)
                or self.preview_task_id!=uuid5(self.preview_run_id,'semantic.preview_remove_object:'+str(self.target_id))):
            raise ValueError('SEMANTIC_PREVIEW_ADOPTION_BINDING_INVALID')
        return self



class BackgroundCleanupPreviewNumericalEvidence(RemovalPreviewNumericalEvidence):
    """Exact background cleanup evidence, not human approval or qualification."""
    params: SemanticBackgroundCleanupPreviewInput


class BackgroundCleanupPreviewPresentation(RemovalPreviewPresentation):
    """Background cleanup display linkage with the same 2560 edge bound."""
    params: SemanticBackgroundCleanupPreviewInput


class ShownBackgroundCleanupAdoptionInput(Contract):
    """Frozen copy task inputs; SQL must prove human decision and receipt origin."""
    schema_version: Annotated[int,Field(strict=True,ge=1,le=1)]
    tenant_id: UUID
    child_run_id: UUID
    target_id: UUID
    approved_edit: ArtifactRef
    preview_run_id: UUID
    preview_task_id: UUID
    preview_result_id: UUID
    preview_attempt: Annotated[int,Field(strict=True,ge=1,le=2147483647)]
    preview_fence: CopyFence
    presentation: ArtifactRef
    numerical_evidence: ArtifactRef
    working_image: ArtifactRef
    pixels: ArtifactRef
    model_lock_sha: SHA256
    runtime_lock_sha: SHA256
    device_profile_sha: SHA256

    @model_validator(mode='after')
    def bindings(self):
        from uuid import uuid5
        ids=(self.tenant_id,self.child_run_id,self.target_id,self.preview_run_id,
             self.preview_task_id,self.preview_result_id)
        refs=(self.approved_edit,self.presentation,self.numerical_evidence,self.working_image,self.pixels)
        all_ids=(*ids,*(ref.artifact_id for ref in refs))
        if (any(value.int==0 for value in all_ids) or len(set(all_ids))!=len(all_ids)
                or self.preview_task_id!=uuid5(self.preview_run_id,'semantic.preview_background_cleanup:'+str(self.target_id))):
            raise ValueError('SEMANTIC_PREVIEW_ADOPTION_BINDING_INVALID')
        return self





class RetouchPreviewNumericalEvidence(Contract):
    """Preservation proof for retouch; neither qualification nor Apply authority."""
    schema_version: Annotated[int, Field(strict=True, ge=1, le=1)]
    params: SemanticRetouchPreviewInput
    working_image: ArtifactRef
    pixels: ArtifactRef
    algorithm: Literal['beautyfilter_v1']
    changed_pixels: Annotated[int, Field(strict=True, ge=0, le=200000000)]
    protected_pixels_unchanged: Literal[True]
    outside_mask_unchanged: Literal[True]
    alpha_unchanged: Literal[True]
    roi: Annotated[list[Annotated[int, Field(strict=True, ge=0, le=200000000)]], Field(min_length=4, max_length=4)]

    @model_validator(mode='before')
    @classmethod
    def strict_preservation(cls,value):
        if isinstance(value,dict) and any(value.get(key) is not True for key in
                ('protected_pixels_unchanged','outside_mask_unchanged','alpha_unchanged')):
            raise ValueError('SEMANTIC_PREVIEW_PROTECTION_INVALID')
        return value

    @model_validator(mode='after')
    def output_binding(self):
        refs=(self.params.plan,self.params.intent,self.params.candidate,
            self.params.source.source,self.params.protection,self.working_image,self.pixels)
        if any(ref.artifact_id.int==0 for ref in refs) or len({ref.artifact_id for ref in refs})!=len(refs):
            raise ValueError('SEMANTIC_PREVIEW_ARTIFACT_ALIAS')
        x0,y0,x1,y1=self.roi
        if x1<=x0 or y1<=y0 or self.changed_pixels>(x1-x0)*(y1-y0):
            raise ValueError('SEMANTIC_PREVIEW_ROI_INVALID')
        return self


class RetouchPreviewPresentation(ExposurePreviewPresentation):
    """Own retouch input with pinned display/media linkage and bounded edges."""
    params: SemanticRetouchPreviewInput
    width: Annotated[int, Field(strict=True, ge=1, le=2560)]
    height: Annotated[int, Field(strict=True, ge=1, le=2560)]


class ShownRetouchAdoptionInput(Contract):
    """Frozen copy task inputs; SQL must prove human decision and receipt origin."""
    schema_version: Annotated[int,Field(strict=True,ge=1,le=1)]
    tenant_id: UUID
    child_run_id: UUID
    target_id: UUID
    approved_edit: ArtifactRef
    preview_run_id: UUID
    preview_task_id: UUID
    preview_result_id: UUID
    preview_attempt: Annotated[int,Field(strict=True,ge=1,le=2147483647)]
    preview_fence: CopyFence
    presentation: ArtifactRef
    numerical_evidence: ArtifactRef
    working_image: ArtifactRef
    pixels: ArtifactRef
    model_lock_sha: SHA256
    runtime_lock_sha: SHA256
    device_profile_sha: SHA256

    @model_validator(mode='after')
    def bindings(self):
        from uuid import uuid5
        ids=(self.tenant_id,self.child_run_id,self.target_id,self.preview_run_id,
             self.preview_task_id,self.preview_result_id)
        refs=(self.approved_edit,self.presentation,self.numerical_evidence,self.working_image,self.pixels)
        all_ids=(*ids,*(ref.artifact_id for ref in refs))
        if (any(value.int==0 for value in all_ids) or len(set(all_ids))!=len(all_ids)
                or self.preview_task_id!=uuid5(self.preview_run_id,'semantic.preview_retouch_region:'+str(self.target_id))):
            raise ValueError('SEMANTIC_PREVIEW_ADOPTION_BINDING_INVALID')
        return self





