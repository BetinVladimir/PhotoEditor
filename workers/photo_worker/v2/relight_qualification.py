"""Exact relight qualification evidence; trusted catalog/lease scope is external."""
from uuid import UUID
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.relight import ImageRelightParams
from photo_worker.operations.session.contracts import Calibration
from .adoption import AdoptionInvalid, ArtifactRef
from .semantic_qualification_pin import SemanticQualificationPin, _sha
from .semantic_qualification_report import _verify_qualification_report


class RelightQualificationPin(SemanticQualificationPin):
    def __post_init__(self):
        if (self.backend!='cpu' or any(not isinstance(value,UUID) or value.int==0 for value in
                (self.policy_id,self.qualification_id,self.issuer_id))
                or any(not _sha(value) for value in (self.policy_sha,self.model_lock_sha,
                    self.runtime_lock_sha,self.device_profile_sha,self.dataset_sha))
                or any(not isinstance(ref,ArtifactRef) or not isinstance(ref.artifact_id,UUID)
                    or ref.artifact_id.int==0 or not _sha(ref.sha256) for ref in (self.report,self.calibration))
                or self.report.artifact_id==self.calibration.artifact_id):
            raise AdoptionInvalid('RELIGHT_QUALIFICATION_PIN_INVALID')


def verify_relight_qualification(pin_value, report_raw, calibration_raw, policy, *,
                                 params, expected_runtime_sha, check_active):
    """Verify canonical report/calibration against independently trusted policy.

    This does not import reports or establish issuer/catalog authority. Caller
    must obtain pin/policy/object bytes from a live authorized catalog and bind
    expected_runtime_sha to the actual installed build before execution.
    """
    if not callable(check_active):raise ValueError('RELIGHT_AUTHORITY_INVALID')
    check_active()
    pin=RelightQualificationPin.from_value(pin_value)
    if (not isinstance(params,ImageRelightParams) or not _sha(expected_runtime_sha)
            or pin.runtime_lock_sha!=expected_runtime_sha
            or str(pin.calibration.artifact_id)!=str(params.calibration.artifact_id)
            or pin.calibration.sha256!=params.calibration.sha256):
        raise AdoptionInvalid('RELIGHT_QUALIFICATION_BINDING_INVALID')
    _verify_qualification_report(report_raw,pin,policy,operation='image.relight',
                                 backend='cpu',check_active=check_active)
    if not isinstance(calibration_raw,bytes):
        raise AdoptionInvalid('RELIGHT_CALIBRATION_INVALID')
    value=parse_json(calibration_raw,1<<20)
    if (not isinstance(value,dict) or type(value.get('schema_version')) is not int
            or canonical_bytes(value)!=calibration_raw
            or sha256(calibration_raw).hexdigest()!=pin.calibration.sha256):
        raise AdoptionInvalid('RELIGHT_CALIBRATION_INVALID')
    calibration=Calibration.model_validate_json(calibration_raw)
    if (not calibration.qualified or calibration.model_lock_sha!=pin.model_lock_sha
            or calibration.dataset_sha!=pin.dataset_sha):
        raise AdoptionInvalid('RELIGHT_CALIBRATION_INVALID')
    check_active()
    return pin,calibration


def relight_runtime_authority(load_pin, *, frozen_pin, check_active):
    """Re-resolve exact current qualification each time; never cache a grant."""
    if (type(frozen_pin) is not RelightQualificationPin or not callable(load_pin)
            or not callable(check_active)):
        raise ValueError('RELIGHT_AUTHORITY_INVALID')
    expected=frozen_pin.fingerprint()
    def verify():
        check_active()
        current=RelightQualificationPin.from_value(load_pin())
        check_active()
        if current.fingerprint()!=expected:
            raise AdoptionInvalid('RELIGHT_QUALIFICATION_CHANGED')
        return current
    return verify
