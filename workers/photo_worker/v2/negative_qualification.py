"""CPU negative qualification byte evidence; live SQL authority remains mandatory."""
from uuid import UUID
from .semantic_qualification_pin import SemanticQualificationPin,_sha
from .adoption import AdoptionInvalid,ArtifactRef


class NegativeQualificationPin(SemanticQualificationPin):
    def __post_init__(self):
        if (self.backend!='cpu' or any(not isinstance(value,UUID) or value.int==0 for value in
                (self.policy_id,self.qualification_id,self.issuer_id))
                or any(not _sha(value) for value in (self.policy_sha,self.model_lock_sha,
                    self.runtime_lock_sha,self.device_profile_sha,self.dataset_sha))
                or any(not isinstance(ref,ArtifactRef) or not isinstance(ref.artifact_id,UUID)
                    or ref.artifact_id.int==0 or not _sha(ref.sha256) for ref in (self.report,self.calibration))
                or self.report.artifact_id==self.calibration.artifact_id):
            raise AdoptionInvalid('NEGATIVE_QUALIFICATION_PIN_INVALID')

from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.optical.negative_calibration import verify_negative_calibration
from .semantic_qualification_report import _verify_qualification_report


def verify_negative_qualification(params,calibration_raw,report_raw,pin_value,policy,*,
        input_icc_sha,runtime_sha,check_active):
    """Original byte/policy proof; current catalog/issuer SQL is external."""
    if not callable(check_active):raise ValueError('NEGATIVE_ACTIVITY_REQUIRED')
    pin=NegativeQualificationPin.from_value(pin_value)
    if pin.runtime_lock_sha!=runtime_sha:raise AdoptionInvalid('NEGATIVE_RUNTIME_MISMATCH')
    check_active()
    calibration=verify_negative_calibration(params,calibration_raw,input_icc_sha=input_icc_sha)
    from photo_worker.contracts.optical_negative import OpticalNegativeParams
    owned=OpticalNegativeParams.model_validate_json(canonical_bytes(params))
    if (_ref_pair(owned.calibration)!=(pin.calibration.artifact_id,pin.calibration.sha256)
            or _ref_pair(calibration.qualification)!=(pin.report.artifact_id,pin.report.sha256)):
        raise AdoptionInvalid('NEGATIVE_QUALIFICATION_BINDING_INVALID')
    _verify_qualification_report(report_raw,pin,policy,operation='optical.negative',backend='cpu',check_active=check_active)
    check_active();return pin,calibration


def _ref_pair(ref):return ref.artifact_id,ref.sha256
