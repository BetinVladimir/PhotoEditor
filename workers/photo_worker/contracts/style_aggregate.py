from typing import Annotated
from pydantic import Field, model_validator
from .base import Contract, ArtifactRef, SHA256, UInt32
from .style import ModelReference


class StyleAggregateParams(Contract):
    style_id: Annotated[str, Field(min_length=1, max_length=120)]
    color_fits: Annotated[list[ArtifactRef], Field(min_length=1, max_length=32)]
    session_ids: Annotated[list[Annotated[str, Field(min_length=1, max_length=120)]], Field(min_length=1, max_length=32)]
    seed: UInt32 = 42
    pair_manifest_sha256: SHA256
    runtime_lock_id: SHA256
    models: list[ModelReference] = Field(default_factory=list, max_length=32)
    beauty_fits: Annotated[list[ArtifactRef], Field(max_length=32)] = Field(default_factory=list)
    defect_evidence: Annotated[list[ArtifactRef], Field(max_length=32)] = Field(default_factory=list)

    @model_validator(mode="after")
    def consistent(self):
        if len(self.session_ids) != len(self.color_fits):
            raise ValueError("Each fit requires one session id")
        if len({(str(ref.artifact_id), ref.sha256) for ref in self.color_fits}) != len(self.color_fits):
            raise ValueError("Duplicate color fit")
        if len({model.id for model in self.models}) != len(self.models):
            raise ValueError("Duplicate model reference")
        for refs in (self.beauty_fits,self.defect_evidence):
            if len({(str(ref.artifact_id),ref.sha256) for ref in refs})!=len(refs):raise ValueError('Duplicate effect evidence')
            if len(refs)>len(self.color_fits):raise ValueError('Effect evidence exceeds pair count')
        return self


class StyleAggregateReport(Contract):
    pair_ids: list[str]
    pair_count: Annotated[int, Field(ge=1, le=32)]
    session_count: Annotated[int, Field(ge=1, le=32)]
    accepted_local_fits: Annotated[int, Field(ge=0, le=32)]
    median_holdout_loss: Annotated[float, Field(ge=0)]
    median_delta_e: Annotated[float, Field(ge=0)]
    p95_delta_e: Annotated[float, Field(ge=0)]
    transfer_confidence: str
    warnings: list[str]
