"""Qualification-to-preparation bridge; installation must supply live SQL reader."""
from contextlib import contextmanager
from copy import deepcopy
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.operations.optical.negative_prepare import prepare_pinned_negative_files
from .negative_qualification import NegativeQualificationPin,verify_negative_qualification
from .adoption import AdoptionInvalid


@contextmanager
def prepare_qualified_negative_files(scan,params,calibration_raw,report_raw,policy,pin_value,
        scratch,*,input_icc_sha,runtime_lock,runtime_sha,ram_max_bytes,load_current_pin,check_active):
    """Refuse stale current pins at each activity boundary, retaining all owners.

    load_current_pin must resolve original task/tenant scope and current importer
    authorization in SQL; this helper supplies no implementation or cached grant.
    Source lease/array authority remains a separate required installer gate.
    """
    if not callable(load_current_pin) or not callable(check_active):raise ValueError('NEGATIVE_AUTHORITY_REQUIRED')
    owned_params=parse_json(canonical_bytes(params),65536)
    owned_policy=deepcopy(policy);owned_pin=deepcopy(pin_value);owned_lock=deepcopy(runtime_lock)
    calibration=bytes(calibration_raw) if isinstance(calibration_raw,(bytes,bytearray)) else calibration_raw
    report=bytes(report_raw) if isinstance(report_raw,(bytes,bytearray)) else report_raw
    frozen=NegativeQualificationPin.from_value(owned_pin)
    def active():
        check_active()
        current=NegativeQualificationPin.from_value(load_current_pin())
        if current!=frozen:raise AdoptionInvalid('NEGATIVE_QUALIFICATION_STALE')
        check_active()
    verify_negative_qualification(owned_params,calibration,report,owned_pin,owned_policy,
        input_icc_sha=input_icc_sha,runtime_sha=runtime_sha,check_active=active)
    with prepare_pinned_negative_files(scan,owned_params,calibration,scratch,input_icc_sha=input_icc_sha,
            runtime_lock=owned_lock,runtime_sha=runtime_sha,ram_max_bytes=ram_max_bytes,check_active=active) as files:
        active();yield files;active()


@contextmanager
def prepare_located_negative_files(connection,objects,tenant,project,actor,session,
        scan,params,policy,pin_value,scratch,*,bucket,input_icc_sha,runtime_lock,
        runtime_sha,ram_max_bytes,check_active):
    """Load original qualification and retain live catalog identity through use.

    The scheduler connection must be autocommit. Object reads finish before
    preparation; every subsequent activity boundary reloads the scoped catalog.
    The installer still must verify the source lease and linear scan authority.
    """
    from .negative_qualification_objects import load_negative_qualification_objects
    from .negative_qualification_catalog import resolve_negative_qualification_objects
    if not callable(check_active):raise ValueError('NEGATIVE_AUTHORITY_REQUIRED')
    p=parse_json(canonical_bytes(params),65536)
    trusted=deepcopy(policy);pin=deepcopy(pin_value);runtime=deepcopy(runtime_lock)
    located=load_negative_qualification_objects(connection,objects,tenant,project,actor,session,
        p,pin,trusted,bucket=bucket,input_icc_sha=input_icc_sha,
        runtime_sha=runtime_sha,check_active=check_active)
    def current():
        locations=resolve_negative_qualification_objects(connection,tenant,project,actor,session,
            pin,check_active=check_active)
        if canonical_bytes(locations)!=located.locations_jcs:
            raise AdoptionInvalid('NEGATIVE_QUALIFICATION_CHANGED')
        return pin
    with prepare_qualified_negative_files(scan,p,located.calibration_raw,located.report_raw,
            trusted,pin,scratch,input_icc_sha=input_icc_sha,runtime_lock=runtime,
            runtime_sha=runtime_sha,ram_max_bytes=ram_max_bytes,
            load_current_pin=current,check_active=check_active) as files:
        yield files
