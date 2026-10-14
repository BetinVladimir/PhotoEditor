"""Original perspective report proof with independently installed CPU pins.

A trusted resolver must prove current SQL lease/project/artifact authority and
versioned-object provenance. A report or its passed flag is never that proof.
"""
from copy import deepcopy
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,canonical_hash,parse_json
from photo_worker.operations.session.contracts import Calibration
from photo_worker.v2.negative_qualification import NegativeQualificationPin
from photo_worker.v2.semantic_qualification_report import _verify_qualification_report
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.runtime.executor import OperationFailure
from .perspective_runtime_lock import PerspectiveRuntimeLock,verify_installed_runtime


@dataclass(frozen=True)
class PerspectiveEvidence:
    pin:dict
    policy:dict
    report_raw:bytes
    calibration_raw:bytes
    asset_version_id:object
    source:object
    canvas_hash:str
    lines:object


class PinnedPerspectiveAdmission:
    def __init__(self,*,runtime_lock,runtime_sha,pin,policy,max_pixels,resolve_evidence,verify_color):
        if (type(max_pixels) is not int or not 1<=max_pixels<=200000000 or
            not callable(resolve_evidence) or not callable(verify_color)):
            raise ValueError('PERSPECTIVE_INSTALLATION_INVALID')
        self.runtime_lock=PerspectiveRuntimeLock.model_validate_json(canonical_bytes(runtime_lock))
        self.runtime_sha=runtime_sha
        self.pin=NegativeQualificationPin.from_value(deepcopy(pin))
        self.policy=parse_json(canonical_bytes(policy),8<<20)
        if self.pin.runtime_lock_sha!=runtime_sha or canonical_hash(self.runtime_lock)!=runtime_sha:
            raise ValueError('PERSPECTIVE_RUNTIME_MISMATCH')
        self.max_pixels=max_pixels;self.resolve_evidence=resolve_evidence;self.verify_color=verify_color

    def admit(self,ctx,params,source,lines):
        ctx.check_active();verify_installed_runtime(self.runtime_lock,self.runtime_sha)
        record=deepcopy(self.resolve_evidence(ctx,params.model_copy(deep=True)))
        if not isinstance(record,PerspectiveEvidence):raise OperationFailure('PERSPECTIVE_QUALIFICATION_UNAVAILABLE')
        pin=NegativeQualificationPin.from_value(deepcopy(record.pin))
        if (pin!=self.pin or canonical_bytes(record.policy)!=canonical_bytes(self.policy) or
            record.asset_version_id!=params.base.asset_version_id or record.source!=params.base.source or
            record.canvas_hash!=params.base.canvas_hash or record.lines!=params.lines):
            raise OperationFailure('PERSPECTIVE_QUALIFICATION_SCOPE_STALE')
        report=_verify_qualification_report(record.report_raw,pin,self.policy,
            operation='optical.perspective',backend='cpu',check_active=ctx.check_active)
        raw=record.calibration_raw
        if not isinstance(raw,bytes) or not 2<=len(raw)<=1<<20:
            raise AdoptionInvalid('PERSPECTIVE_CALIBRATION_INVALID')
        value=parse_json(raw,1<<20)
        if canonical_bytes(value)!=raw or sha256(raw).hexdigest()!=pin.calibration.sha256:
            raise AdoptionInvalid('PERSPECTIVE_CALIBRATION_INVALID')
        calibration=Calibration.model_validate_json(raw)
        if not calibration.qualified or calibration.model_lock_sha!=pin.model_lock_sha or calibration.dataset_sha!=pin.dataset_sha:
            raise AdoptionInvalid('PERSPECTIVE_CALIBRATION_INVALID')
        estimate=ctx.lease.execution_profile.get('resource_estimate')
        if not isinstance(estimate,dict):raise OperationFailure('CONFIG_INVALID')
        for metric,budget in [('peak_rss_bytes','ram_bytes'),('scratch_bytes','scratch_bytes')]:
            reserved=estimate.get(budget)
            if type(reserved) is not int or reserved<report['metrics'][metric]:raise OperationFailure('QUALIFICATION_RESOURCE_EXCEEDED')
        if report['metrics']['peak_vram_bytes']!=0:raise OperationFailure('CONFIG_INVALID')
        if self.verify_color(source.color.model_copy(deep=True)) is not True:
            raise OperationFailure('PERSPECTIVE_ICC_MISMATCH')
        ctx.check_active()
        # Re-resolve live grant after original bytes/ICC verification. The
        # resolver must use exact versioned objects; no cached grant is allowed.
        current=self.resolve_evidence(ctx,params.model_copy(deep=True))
        if current!=record:raise OperationFailure('PERSPECTIVE_QUALIFICATION_SCOPE_STALE')
        ctx.check_active();return self.max_pixels
