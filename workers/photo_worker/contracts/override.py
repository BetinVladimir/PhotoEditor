from typing import Annotated, Literal

from pydantic import Field, model_validator

from .base import Contract, SHA256, Unit
from .images import Face68, MaskRef
from .manual_mask import Polygon
from .style import Beauty, Defects, Disabled, Extensions, Geometry, GlobalColor, LocalModule

class OverrideModules(Contract):
    strength: Unit | None = None
    global_: GlobalColor | None = Field(default=None, alias="global")
    local: LocalModule | None = None
    beauty: Annotated[Disabled | Beauty, Field(discriminator="enabled")] | None = None
    defects: Annotated[Disabled | Defects, Field(discriminator="enabled")] | None = None
    geometry: Annotated[Disabled | Geometry, Field(discriminator="enabled")] | None = None
    extensions: Extensions | None = None

    @model_validator(mode='after')
    def nonempty(self):
        values = (self.strength, self.global_, self.local, self.beauty, self.defects,
                  self.geometry, self.extensions)
        if all(value is None for value in values):
            raise ValueError("Empty override modules")
        return self


class OverrideMaskBinding(Contract):
    binding_id: Annotated[str, Field(pattern=r"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$")]
    purpose: Literal[
        "portrait_teeth", "portrait_sclera", "portrait_shine", "portrait_under_eye",
        "portrait_cloth", "portrait_glasses", "inpaint_target", "generation_target", "protected", "person_subject",
    ]
    mask: MaskRef
    confirmed: bool = False
    strength: Unit = 1

    @model_validator(mode="after")
    def coherent(self):
        selectors = {
            "portrait_teeth": {"teeth", "manual"}, "portrait_sclera": {"sclera", "manual"},
            "portrait_shine": {"skin", "manual"}, "portrait_under_eye": {"skin", "manual"},
            "portrait_cloth": {"clothing", "manual"}, "portrait_glasses": {"glasses", "manual"},
            "inpaint_target": {"manual"}, "generation_target": {"manual"},
        }
        if self.purpose == "protected":
            if not self.mask.protected:
                raise ValueError("Protected override binding requires a protected mask")
        elif self.purpose == "person_subject":
            if (self.mask.selector != "person" or self.mask.protected or self.mask.object_id is not None
                    or self.mask.model_lock_id is None or not self.confirmed):
                raise ValueError("Person source evidence requires a confirmed original person mask")
        elif self.mask.protected or self.mask.selector not in selectors[self.purpose]:
            raise ValueError("Override mask selector does not match its purpose")
        if self.purpose in {"portrait_cloth", "portrait_glasses", "inpaint_target", "generation_target"} and not self.confirmed:
            raise ValueError("Destructive or content-sensitive mask use requires confirmation")
        return self

class OverrideV1(Contract):
    schema_version: Literal[1]
    input_sha256: SHA256
    object_id: Annotated[str, Field(min_length=1, max_length=160)] | None = None
    modules: OverrideModules
    accepted_candidate_ids: Annotated[list[str], Field(max_length=128)] = Field(default_factory=list)
    rejected_candidate_ids: Annotated[list[str], Field(max_length=128)] = Field(default_factory=list)
    mask_refs: list[OverrideMaskBinding] = Field(default_factory=list, max_length=128)
    protected_polygons: list[Polygon] = Field(default_factory=list, max_length=128)
    manual_landmarks: Face68 | None = None
    revision: Annotated[int, Field(ge=1)]

    @model_validator(mode='after')
    def decisions(self):
        if set(self.accepted_candidate_ids) & set(self.rejected_candidate_ids):
            raise ValueError("Candidate decision conflict")
        if len({item.binding_id for item in self.mask_refs}) != len(self.mask_refs):
            raise ValueError("Duplicate override mask binding")
        if any(item.mode != 'protect' for item in self.protected_polygons):
            raise ValueError("Override polygon must protect")
        if len({item.sequence for item in self.protected_polygons}) != len(self.protected_polygons):
            raise ValueError("Duplicate protected polygon sequence")
        if sum(len(item.points) for item in self.protected_polygons) > 10000:
            raise ValueError("Too many protected polygon points")
        if (self.manual_landmarks is not None and
                (self.manual_landmarks.object_id.int == 0 or
                 self.manual_landmarks.canvas.source_sha256 != self.input_sha256)):
            raise ValueError("Manual landmarks source mismatch")
        return self

class JobOverrideV1(Contract):
    schema_version: Literal[1]
    modules: OverrideModules
