"""Concrete pre-provisioned purge fixture execution and durable probe report."""
from dataclasses import asdict
from datetime import datetime,timezone
from hashlib import sha256
import os
import re
from time import monotonic
from photo_worker.contracts.base import canonical_bytes
from .restore_report import _json
from .restore_replay_receipt import VerifiedRestoreReplay
from .restore_access_probe import RestoreAccessTarget
from .restore_s3_probe import RestoreS3AccessTarget
from .restore_inventory import RestoreObject
from .restore_purge_baseline import verify_purge_fixture_baseline
from .restore_purge_lifecycle import RestoreTenantDeletionTarget,request_fixture_tenant_deletion,wait_for_fixture_purge,verify_fixture_purge_start
from .restore_purge_observation import observe_completed_fixture_purge
from .restore_probe_report import validate_probe_report_root,persist_restore_probe_report
from .restore_acceptance import _require_restore_guard


def purge_fixture_descriptor(replay,buckets,references,deletion_target,api_target,api_sha256,s3_target):
    """Stable independent pin input; no bearer tokens or signed URL queries."""
    return _json({'schema_version':'restore-purge-fixture/v1','restore_run_id':replay.run_id,
        'database':replay.database,'build_sha256':replay.build_sha256,
        'recovery_sha256':replay.recovery_sha256,'replay_sha256':replay.replay_sha256,
        'tenant_id':deletion_target.tenant_id,'revision':deletion_target.revision,
        'buckets':sorted(buckets),'references':[asdict(ref) for ref in references],
        'api_origin':api_target.origin,'delete_origin':deletion_target.origin,
        'api_path_sha256':sha256(api_target.denied_path.encode()).hexdigest(),
        'api_control_path_sha256':sha256(api_target.control_path.encode()).hexdigest(),
        'api_sha256':api_sha256,'api_control_sha256':api_target.control_sha256,
        'api_denial':[api_target.expected_status,api_target.expected_code],
        's3_origin':s3_target.origin,'s3_bucket':s3_target.bucket,'s3_key':s3_target.key,
        's3_version':s3_target.version,'s3_control_key':s3_target.control_key,
        's3_control_version':s3_target.control_version,'s3_control_sha256':s3_target.control_sha256})


def run_purge_negative_probe(replay,fixture_sha256,*,report_root,connection,client,buckets,references,
                            deletion_target,api_target,api_sha256,s3_target,tls_context,maximum_seconds,
                            assert_installed_fixture,check_active):
    """No fixture provisioning or production cutover; requires installed fence.

    On unknown request outcome/interrupted purge, never retry this whole runner
    blindly: independently reconcile the exact tenant/job and retained baseline.
    """
    if (not isinstance(replay,VerifiedRestoreReplay) or not isinstance(deletion_target,RestoreTenantDeletionTarget)
            or not isinstance(api_target,RestoreAccessTarget) or not isinstance(s3_target,RestoreS3AccessTarget)
            or not isinstance(buckets,(tuple,list)) or not isinstance(references,(tuple,list))
            or not callable(assert_installed_fixture) or not callable(check_active)
            or type(maximum_seconds) is not int or not 1<=maximum_seconds<=300
            or not isinstance(fixture_sha256,str) or re.fullmatch('[0-9a-f]{64}',fixture_sha256) is None
            or deletion_target.tenant_id!=s3_target.tenant_id or deletion_target.origin!=api_target.origin):
        raise ValueError('RESTORE_PURGE_RUNNER_INSTALLATION')
    _require_restore_guard(check_active,'RESTORE_PURGE_RUNNER_INACTIVE')
    root=validate_probe_report_root(report_root)
    installed=tuple(buckets);refs=tuple(references)
    if any(not isinstance(ref,RestoreObject) for ref in refs):raise ValueError('RESTORE_PURGE_RUNNER_REFERENCES')
    descriptor=purge_fixture_descriptor(replay,installed,refs,deletion_target,api_target,api_sha256,s3_target)
    if len(canonical_bytes(descriptor))>131072 or sha256(canonical_bytes(descriptor)).hexdigest()!=fixture_sha256:
        raise ValueError('RESTORE_PURGE_RUNNER_FIXTURE_PIN')
    stem=str(replay.run_id)+'.purge_negative'
    if os.path.lexists(root/(stem+'.json')) or os.path.lexists(root/(stem+'.pending')):
        raise ValueError('RESTORE_PURGE_RUNNER_ALREADY_REPORTED')
    deadline=monotonic()+maximum_seconds
    def guard():
        _require_restore_guard(check_active,'RESTORE_PURGE_RUNNER_INACTIVE')
        _require_restore_guard(assert_installed_fixture,'RESTORE_PURGE_RUNNER_FIXTURE')
        _require_restore_guard(check_active,'RESTORE_PURGE_RUNNER_INACTIVE')
        if monotonic()>=deadline:raise ValueError('RESTORE_PURGE_RUNNER_TIMEOUT')
    guard();started=datetime.now(timezone.utc)
    if started<replay.completed_at:raise ValueError('RESTORE_PURGE_RUNNER_TIME')
    verify_fixture_purge_start(connection,deletion_target,expected_database=replay.database,
        assert_fixture_scope=lambda *_:guard(),check_active=guard)
    baseline=verify_purge_fixture_baseline(client,installed,deletion_target.tenant_id,refs,
        api_target=api_target,api_sha256=api_sha256,s3_target=s3_target,tls_context=tls_context,
        assert_installed_fixture=guard,check_active=guard)
    guard()
    verify_fixture_purge_start(connection,deletion_target,expected_database=replay.database,
        assert_fixture_scope=lambda *_:guard(),check_active=guard)
    job=request_fixture_tenant_deletion(deletion_target,tls_context=tls_context,
        assert_fixture_scope=lambda *_:guard(),check_active=guard)
    guard()
    completion=wait_for_fixture_purge(connection,deletion_target.tenant_id,job,expected_database=replay.database,
        maximum_seconds=max(1,min(maximum_seconds,int(deadline-monotonic()+0.999))),
        assert_fixture_scope=lambda *_:guard(),check_active=guard)
    observed=observe_completed_fixture_purge(connection,client,installed,deletion_target.tenant_id,job,
        expected_database=replay.database,api_target=api_target,s3_target=s3_target,tls_context=tls_context,
        assert_installed_fixture=guard,check_active=guard)
    guard();finished=datetime.now(timezone.utc)
    return persist_restore_probe_report(root,replay,'purge_negative',fixture_sha256,started,finished,
        {'fixture':descriptor,'baseline':asdict(baseline),'completion':asdict(completion),'purge':asdict(observed)},
        check_active=guard)
