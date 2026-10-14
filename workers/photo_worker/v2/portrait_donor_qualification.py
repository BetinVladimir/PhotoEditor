"""Distinct CPU donor qualification; live SQL authority remains mandatory."""
from uuid import UUID
from .semantic_qualification_pin import SemanticQualificationPin,_sha
from .adoption import AdoptionInvalid,ArtifactRef


class DonorQualificationPin(SemanticQualificationPin):
    def __post_init__(self):
        if (self.backend!='cpu' or any(not isinstance(value,UUID) or value.int==0 for value in
                (self.policy_id,self.qualification_id,self.issuer_id))
                or any(not _sha(value) for value in (self.policy_sha,self.model_lock_sha,
                    self.runtime_lock_sha,self.device_profile_sha,self.dataset_sha))
                or any(not isinstance(ref,ArtifactRef) or not isinstance(ref.artifact_id,UUID)
                    or ref.artifact_id.int==0 or not _sha(ref.sha256) for ref in (self.report,self.calibration))
                or self.report.artifact_id==self.calibration.artifact_id):
            raise AdoptionInvalid('DONOR_QUALIFICATION_PIN_INVALID')

from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.operations.session.contracts import Calibration
from .semantic_qualification_report import _verify_qualification_report


def donor_compiler_profile(profile):
    """Own the compiler portion; validate any complete SQL transport envelope.

    Original full lease/profile must still go to SQL authority and input-index
    validation. This extractor is not input closure or attempt authorization.
    """
    from photo_worker.contracts.base import ArtifactRef as WireRef
    from .contract_lock import SCHEMAS_SHA256,DETAIL_MANIFEST_PREVIOUS_SCHEMAS_SHA256,DETAIL_LINK_PREVIOUS_SCHEMAS_SHA256,HOLDOUT_EVIDENCE_PREVIOUS_SCHEMAS_SHA256,SQL_TRANSPORT_SCHEMAS_SHA256,PREVIOUS_SCHEMAS_SHA256
    owned=parse_json(canonical_bytes(profile),1<<20)
    compiler={'backend','renderer_version','portrait_runtime_sha','resource_estimate','donor_qualification'}
    transport={'contracts_sha','v2_input_manifest','v2_entry_count','v2_entries_sha'}
    if not isinstance(owned,dict) or set(owned) not in (compiler,compiler|transport):
        raise AdoptionInvalid('DONOR_QUALIFICATION_PROFILE_INVALID')
    if set(owned)==compiler|transport:
        if (owned['contracts_sha'] not in (SCHEMAS_SHA256,DETAIL_MANIFEST_PREVIOUS_SCHEMAS_SHA256,DETAIL_LINK_PREVIOUS_SCHEMAS_SHA256,HOLDOUT_EVIDENCE_PREVIOUS_SCHEMAS_SHA256,SQL_TRANSPORT_SCHEMAS_SHA256,PREVIOUS_SCHEMAS_SHA256)
                or type(owned['v2_entry_count']) is not int or not 1<=owned['v2_entry_count']<=127
                or not _sha(owned['v2_entries_sha'])):
            raise AdoptionInvalid('DONOR_TRANSPORT_PROFILE_INVALID')
        ref=WireRef.model_validate_json(canonical_bytes(owned['v2_input_manifest']))
        if ref.artifact_id.int==0:
            raise AdoptionInvalid('DONOR_TRANSPORT_PROFILE_INVALID')
    return {key:owned[key] for key in compiler}


def verify_donor_qualification(profile,report_raw,calibration_raw,policy,*,check_active):
    """Exact original documents; catalog/lease/object-version authority is external."""
    if not callable(check_active):raise ValueError('DONOR_AUTHORITY')
    check_active()
    owned=donor_compiler_profile(profile)
    if (not isinstance(owned,dict) or set(owned)!= {'backend','renderer_version','portrait_runtime_sha','resource_estimate','donor_qualification'}
            or owned['backend']!='cpu' or owned['renderer_version']!='v2.2'):
        raise AdoptionInvalid('DONOR_QUALIFICATION_PROFILE_INVALID')
    pin=DonorQualificationPin.from_value(owned['donor_qualification'])
    if pin.runtime_lock_sha!=owned['portrait_runtime_sha']:
        raise AdoptionInvalid('DONOR_QUALIFICATION_PROFILE_INVALID')
    if not isinstance(report_raw,bytes) or not isinstance(calibration_raw,bytes):
        raise AdoptionInvalid('DONOR_QUALIFICATION_DOCUMENT_INVALID')
    _verify_qualification_report(report_raw,pin,policy,operation='portrait.open_eyes',backend='cpu',check_active=check_active)
    value=parse_json(calibration_raw,1<<20)
    if (not isinstance(value,dict) or type(value.get('schema_version')) is not int
            or canonical_bytes(value)!=calibration_raw or sha256(calibration_raw).hexdigest()!=pin.calibration.sha256):
        raise AdoptionInvalid('DONOR_CALIBRATION_INVALID')
    calibration=Calibration.model_validate_json(calibration_raw)
    if not calibration.qualified or calibration.model_lock_sha!=pin.model_lock_sha or calibration.dataset_sha!=pin.dataset_sha:
        raise AdoptionInvalid('DONOR_CALIBRATION_INVALID')
    check_active()
    return pin,calibration


def donor_runtime_authority(load_pin,*,frozen_pin,check_active):
    """Re-resolve live exact qualification on each call; no cached grant."""
    if type(frozen_pin) is not DonorQualificationPin or not callable(load_pin) or not callable(check_active):
        raise ValueError('DONOR_AUTHORITY')
    expected=frozen_pin.fingerprint()
    def verify():
        check_active()
        current=DonorQualificationPin.from_value(load_pin())
        if current.fingerprint()!=expected:raise AdoptionInvalid('DONOR_QUALIFICATION_CHANGED')
        check_active()
        return current
    return verify
