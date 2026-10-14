"""Qualified original-input negative reconstruction before atomic adoption.

This proves bytes only. The adopter must re-lock completed-task authority and
the exact qualification snapshot in its final SQL transaction.
"""
from copy import deepcopy
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.images import WorkingImage
from .adoption import AdoptionInvalid
from .lens_bypass import _ref, _document
from .negative_adoption import verify_negative_result_metadata, verify_pinned_negative_replay
from .negative_qualification import NegativeQualificationPin, verify_negative_qualification


def verify_qualified_negative_replay(reader, params_ref, root_ref, *, profile, policy,
        runtime_lock, runtime_sha, scratch_root, max_bytes, ram_max_bytes,
        current_pin, source_authority, verify_color, check_active):
    if any(not callable(value) for value in
            (current_pin, source_authority, verify_color, check_active)):
        raise ValueError('NEGATIVE_ADOPTION_AUTHORITY_REQUIRED')
    params_ref=deepcopy(params_ref);root_ref=deepcopy(root_ref)
    owned_profile=parse_json(canonical_bytes(profile),1<<20)
    owned_policy=parse_json(canonical_bytes(policy),1<<20)
    owned_lock=deepcopy(runtime_lock)
    if (not isinstance(owned_profile,dict) or owned_profile.get('backend')!='cpu' or owned_profile.get('renderer_version')!='v2.2'
            or owned_profile.get('negative_runtime_sha')!=runtime_sha):
        raise AdoptionInvalid('NEGATIVE_ADOPTION_PROFILE_INVALID')
    pin=NegativeQualificationPin.from_value(owned_profile.get('negative_qualification'))
    if pin.runtime_lock_sha!=runtime_sha:
        raise AdoptionInvalid('NEGATIVE_RUNTIME_MISMATCH')
    def active():
        check_active()
        if current_pin()!=pin:
            raise AdoptionInvalid('NEGATIVE_QUALIFICATION_CHANGED')
        check_active()
    active()
    params,result=verify_negative_result_metadata(reader,params_ref,root_ref,check_active=active)
    source=_document(reader,_ref(params.base.source),WorkingImage,'input')
    params_body=canonical_bytes(params);source_body=canonical_bytes(source)
    def qualified_active():
        active()
        if (source_authority(parse_json(params_body,65536),source_body) is not True
                or verify_color(WorkingImage.model_validate_json(source_body)) is not True):
            raise AdoptionInvalid('NEGATIVE_ADOPTION_SOURCE_INVALID')
        active()
    qualified_active()
    from photo_worker.operations.optical.negative_resources import admit_negative
    estimate=admit_negative(owned_profile,source.canvas.width,source.canvas.height,
        has_alpha=source.alpha is not None)
    if (type(ram_max_bytes) is not int or ram_max_bytes<estimate['ram_bytes']
            or type(max_bytes) is not int or max_bytes<estimate['scratch_bytes']):
        raise AdoptionInvalid('NEGATIVE_ADOPTION_RESOURCE_EXCEEDED')
    def original(ref,limit):
        qualified_active();evidence=reader.evidence(ref)
        if (evidence.ref!=ref or evidence.source!='input' or evidence.media_type!='application/json'
                or type(evidence.bytes) is not int or not 2<=evidence.bytes<=limit):
            raise AdoptionInvalid('NEGATIVE_QUALIFICATION_OBJECT_INVALID')
        raw=reader.read_json(ref,limit)
        if (not isinstance(raw,bytes) or len(raw)!=evidence.bytes
                or sha256(raw).hexdigest()!=ref.sha256
                or canonical_bytes(parse_json(raw,limit))!=raw):
            raise AdoptionInvalid('NEGATIVE_QUALIFICATION_BYTES_INVALID')
        qualified_active();return raw
    calibration_raw=original(pin.calibration,65536)
    from photo_worker.operations.optical.negative_calibration import verify_negative_calibration
    verify_negative_calibration(params,calibration_raw,input_icc_sha=source.color.icc_sha256)
    report_raw=original(pin.report,8<<20)
    verify_negative_qualification(params,calibration_raw,report_raw,
        owned_profile['negative_qualification'],owned_policy,input_icc_sha=source.color.icc_sha256,
        runtime_sha=runtime_sha,check_active=qualified_active)
    replay=verify_pinned_negative_replay(reader,params_ref,root_ref,runtime_lock=owned_lock,
        runtime_sha=runtime_sha,scratch_root=scratch_root,max_bytes=max_bytes,
        ram_max_bytes=ram_max_bytes,check_active=qualified_active)
    qualified_active();return replay
