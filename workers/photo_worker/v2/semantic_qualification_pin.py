"""Owned frozen semantic qualification identity for explicitly installed workers.

The loader must resolve live SQL authority and verify exact report bytes. A pin
alone is not qualification evidence and this module does not install defaults.
"""
from dataclasses import dataclass,asdict
from uuid import UUID
from photo_worker.contracts.base import canonical_hash
from .adoption import AdoptionInvalid,ArtifactRef


def _uuid(value):
    if not isinstance(value,str):raise AdoptionInvalid('SEMANTIC_QUALIFICATION_PIN_INVALID')
    try:result=UUID(value)
    except (ValueError,AttributeError) as error:
        raise AdoptionInvalid('SEMANTIC_QUALIFICATION_PIN_INVALID') from error
    if result.int==0 or str(result)!=value:
        raise AdoptionInvalid('SEMANTIC_QUALIFICATION_PIN_INVALID')
    return result


def _sha(value):
    return isinstance(value,str) and len(value)==64 and all(c in '0123456789abcdef' for c in value)


@dataclass(frozen=True)
class SemanticQualificationPin:
    policy_id: UUID
    qualification_id: UUID
    policy_sha: str
    report: ArtifactRef
    calibration: ArtifactRef
    model_lock_sha: str
    runtime_lock_sha: str
    device_profile_sha: str
    dataset_sha: str
    issuer_id: UUID
    backend: str

    def __post_init__(self):
        if (any(not isinstance(value,UUID) or value.int==0 for value in
                (self.policy_id,self.qualification_id,self.issuer_id))
                or self.backend!='cuda' or any(not _sha(value) for value in
                (self.policy_sha,self.model_lock_sha,self.runtime_lock_sha,self.device_profile_sha,self.dataset_sha))
                or any(not isinstance(ref,ArtifactRef) or not isinstance(ref.artifact_id,UUID)
                       or ref.artifact_id.int==0 or not _sha(ref.sha256) for ref in (self.report,self.calibration))
                or self.report.artifact_id==self.calibration.artifact_id):
            raise AdoptionInvalid('SEMANTIC_QUALIFICATION_PIN_INVALID')

    @classmethod
    def from_value(cls,value):
        if not isinstance(value,dict) or set(value)!=set(cls.__dataclass_fields__):
            raise AdoptionInvalid('SEMANTIC_QUALIFICATION_PIN_INVALID')
        fields=dict(value)
        for key in ('policy_id','qualification_id','issuer_id'):fields[key]=_uuid(fields[key])
        try:
            for key in ('report','calibration'):
                raw=fields[key]
                if not isinstance(raw,dict):raise ValueError('invalid ref')
                _uuid(raw.get('artifact_id'))
                fields[key]=ArtifactRef.parse(raw)
            return cls(**fields)
        except (TypeError,ValueError,AttributeError) as error:
            raise AdoptionInvalid('SEMANTIC_QUALIFICATION_PIN_INVALID') from error

    def fingerprint(self):
        value=asdict(self)
        for key in ('policy_id','qualification_id','issuer_id'):value[key]=str(value[key])
        for key in ('report','calibration'):value[key]['artifact_id']=str(value[key]['artifact_id'])
        return canonical_hash(value)

    def bind(self,model_lock_sha,calibration):
        """Exact identity, including artifact ID even for identical bytes."""
        if model_lock_sha!=self.model_lock_sha or calibration!=self.calibration:
            raise AdoptionInvalid('SEMANTIC_QUALIFICATION_BINDING_INVALID')


def semantic_runtime_authority(load_pin,*,frozen_pin,model_lock_sha,runtime_lock_sha,
                               device_profile_sha,dataset_sha,check_active=lambda:None):
    """Create the no-argument callback required by semantic transfer execution.

    Re-resolve live authority on every call; never cache a successful admission.
    Deployment measurements are trusted installer inputs, never API values.
    """
    if (not isinstance(frozen_pin,SemanticQualificationPin) or not callable(load_pin)
            or not callable(check_active) or any(not _sha(value) for value in
            (model_lock_sha,runtime_lock_sha,device_profile_sha,dataset_sha))):
        raise ValueError('SEMANTIC_RUNTIME_AUTHORITY_INVALID')
    frozen_sha=frozen_pin.fingerprint()
    expected=(model_lock_sha,runtime_lock_sha,device_profile_sha,dataset_sha)
    def verify():
        check_active()
        value=load_pin()
        check_active()
        pin=SemanticQualificationPin.from_value(value)
        if (pin.fingerprint()!=frozen_sha
                or (pin.model_lock_sha,pin.runtime_lock_sha,pin.device_profile_sha,pin.dataset_sha)!=expected):
            raise AdoptionInvalid('SEMANTIC_RUNTIME_QUALIFICATION_CHANGED')
        return pin
    return verify
