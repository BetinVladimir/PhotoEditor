"""Distinct automatic authorization; never impersonates a human review."""
from typing import Annotated, Literal
from pydantic import Field,model_validator
from .base import Contract, ArtifactRef, SHA256
from .images import MaskRef

POLICY = 'conservative_auto_v1'

class DefectAutoRepairParams(Contract):
    policy: Literal['conservative_auto_v1']
    image: ArtifactRef
    proposal: ArtifactRef
    proposal_report: ArtifactRef
    resolved: ArtifactRef

class DustAutoRepairParams(Contract):
    policy: Literal['conservative_auto_v1']
    image: ArtifactRef
    proposal: ArtifactRef | None
    resolved: ArtifactRef
    protected: Annotated[list[MaskRef], Field(min_length=1,max_length=64)]

class ConservativeRepairReport(Contract):
    version: Literal['conservative_repair_report_v1']='conservative_repair_report_v1'
    policy: Literal['conservative_auto_v1']
    operation: Literal['defects.auto_repair','sensor.dust_auto_repair']
    image: ArtifactRef
    proposal: ArtifactRef | None
    resolved: ArtifactRef
    selected_candidate_ids: Annotated[list[SHA256],Field(max_length=4096)]
    applied_candidate_ids: Annotated[list[SHA256],Field(max_length=4096)]
    skipped_candidate_ids: Annotated[list[SHA256],Field(max_length=4096)]

    @model_validator(mode='after')
    def coherent(self):
        selected,applied,skipped=self.selected_candidate_ids,self.applied_candidate_ids,self.skipped_candidate_ids
        if any(values!=sorted(set(values)) for values in (selected,applied,skipped)) or not set(applied)<=set(selected) or set(applied)&set(skipped):
            raise ValueError('CONSERVATIVE_EVIDENCE_INVALID')
        if self.proposal is None and (selected or applied or skipped):raise ValueError('CONSERVATIVE_EVIDENCE_INVALID')
        return self
