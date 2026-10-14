"""Immutable complete measured qualification identity shared with Go/SQL."""
from dataclasses import dataclass,asdict
from photo_worker.contracts.base import canonical_hash
from .adoption import AdoptionInvalid


@dataclass(frozen=True)
class RenderQualificationPin:
    model_lock_sha: str
    runtime_lock_sha: str
    backend: str
    device_profile_sha: str
    dataset_sha: str
    calibration_sha: str

    def __post_init__(self):
        if (self.backend not in ('cpu','cuda','mps') or any(not isinstance(value,str)
                or len(value)!=64 or any(c not in '0123456789abcdef' for c in value)
                for name,value in asdict(self).items() if name!='backend')):
            raise AdoptionInvalid('RENDER_QUALIFICATION_PIN_INVALID')

    @classmethod
    def from_value(cls, value):
        if not isinstance(value,dict) or set(value)!=set(cls.__dataclass_fields__):
            raise AdoptionInvalid('RENDER_QUALIFICATION_PIN_INVALID')
        return cls(**value)

    def fingerprint(self):
        return canonical_hash(asdict(self))


def load_render_adoption_qualification(connection,tenant_id,task_id,context):
    if context.operation!='render.compose' or context.version!='1':
        raise AdoptionInvalid('RENDER_QUALIFICATION_PIN_INVALID')
    row=connection.execute('SELECT photo.v26_render_adoption_qualification(%s,%s,%s,%s,%s)',
        (tenant_id,task_id,context.attempt,context.fence,context.result_id)).fetchone()
    if row is None:
        raise AdoptionInvalid('RENDER_ADOPTION_INPUTS_STALE')
    if not isinstance(row,(tuple,list)) or len(row)!=1:
        raise AdoptionInvalid('RENDER_QUALIFICATION_PIN_INVALID')
    if row[0] is None:
        raise AdoptionInvalid('RENDER_ADOPTION_INPUTS_STALE')
    return RenderQualificationPin.from_value(row[0])
