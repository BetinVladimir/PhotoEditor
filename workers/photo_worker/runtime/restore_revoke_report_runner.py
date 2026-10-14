"""Concrete combined consent restore probe; never authorizes writers."""
from dataclasses import dataclass,asdict
from datetime import datetime,timezone
from hashlib import sha256
import os
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes
from .restore_replay_receipt import VerifiedRestoreReplay
from .restore_probe_report import validate_probe_report_root,persist_restore_probe_report
from .restore_revoke_installation import revoke_fixture_descriptor,run_pinned_revoke_observations
from .restore_running_installation import running_revoke_fixture_descriptor,run_pinned_running_revoke
from .restore_revoke_probe import observe_fixture_learning_consent
from .restore_render_barrier import RestoreBarrierRenderer
from .restore_revoke_independence import verify_revoke_fixture_independence
from .restore_revoke_running import observe_fixture_cancelled_render
from .restore_revoke_runner import validate_fixture_revoke_installation,RestoreRevokeBatchInstallation
from .restore_acceptance import _require_restore_guard


@dataclass(frozen=True)
class RestoreAdmissionRevokeInstallation:
    target: object
    document: object
    positive_freeze: object
    negative_freeze: object
    binding: object
    batch: object


def combined_revoke_descriptor(replay,admission,running_target,child_run_id,maximum_seconds):
    return {'schema_version':'restore-combined-revoke-fixture/v1',
        'admission':revoke_fixture_descriptor(replay,admission.target,admission.document,
            admission.positive_freeze,admission.negative_freeze,admission.binding,admission.batch),
        'running':running_revoke_fixture_descriptor(replay,running_target,child_run_id,maximum_seconds,barrier_required=True)}


def run_revoke_negative_probe(replay,fixture_sha256,*,report_root,connection,client,buckets,
                              admission,running_target,child_run_id,maximum_seconds,renderer_barrier,
                              tls_context,assert_installed_fixture,check_active):
    """Two distinct grants: held running cancellation and fresh admission denial.

    Both descriptors are pinned before mutation. On interruption, reconcile each
    consent and request separately; this entire runner must not be retried blindly.
    """
    if (not isinstance(replay,VerifiedRestoreReplay)
            or not isinstance(admission,RestoreAdmissionRevokeInstallation)
            or not isinstance(admission.batch,RestoreRevokeBatchInstallation)
            or not isinstance(renderer_barrier,RestoreBarrierRenderer)
            or admission.target.fixture.consent_id==running_target.fixture.consent_id
            or admission.target.fixture.tenant_id!=running_target.fixture.tenant_id
            or admission.target.fixture.project_id!=running_target.fixture.project_id
            or admission.target.fixture.actor_id!=running_target.fixture.actor_id
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_COMBINED_REVOKE_INSTALLATION')
    _require_restore_guard(check_active,'RESTORE_COMBINED_REVOKE_INACTIVE')
    root=validate_probe_report_root(report_root)
    validate_fixture_revoke_installation(admission.target,admission.document,admission.positive_freeze,
        admission.negative_freeze,admission.binding,batch_installation=admission.batch,
        assert_installed_fixture=assert_installed_fixture,check_active=check_active)
    descriptor=combined_revoke_descriptor(replay,admission,running_target,child_run_id,maximum_seconds)
    if sha256(canonical_bytes(descriptor)).hexdigest()!=fixture_sha256:
        raise ValueError('RESTORE_COMBINED_REVOKE_PIN')
    stem=str(replay.run_id)+'.revoke_negative'
    if any(os.path.lexists(root/(stem+suffix)) for suffix in ('.pending','.json')):
        raise ValueError('RESTORE_COMBINED_REVOKE_ALREADY_REPORTED')
    def guard(*_):
        _require_restore_guard(check_active,'RESTORE_COMBINED_REVOKE_INACTIVE')
        _require_restore_guard(assert_installed_fixture,'RESTORE_COMBINED_REVOKE_FIXTURE')
        _require_restore_guard(check_active,'RESTORE_COMBINED_REVOKE_INACTIVE')
    guard();started=datetime.now(timezone.utc)
    if started<replay.completed_at:raise ValueError('RESTORE_COMBINED_REVOKE_TIME')
    verify_revoke_fixture_independence(connection,admission.target.fixture,running_target.fixture,
        expected_database=replay.database,check_active=guard)
    # Consume the already-held barrier first; do not spend its budget on a
    # separate positive batch execution.
    running=run_pinned_running_revoke(replay,sha256(canonical_bytes(descriptor['running'])).hexdigest(),
        connection,running_target,child_run_id,maximum_seconds=maximum_seconds,renderer_barrier=renderer_barrier,
        tls_context=tls_context,assert_installed_fixture=guard,check_active=check_active)
    if running.barrier_cancelled is not True:raise ValueError('RESTORE_COMBINED_REVOKE_BARRIER')
    observed=run_pinned_revoke_observations(replay,sha256(canonical_bytes(descriptor['admission'])).hexdigest(),
        connection,client,buckets,admission.target,admission.document,admission.positive_freeze,
        admission.negative_freeze,admission.binding,batch_installation=admission.batch,
        tls_context=tls_context,assert_installed_fixture=guard,check_active=check_active)
    if observed.positive_batch is None or observed.negative_batch is None:
        raise ValueError('RESTORE_COMBINED_REVOKE_BATCH')
    for target,expected in ((running_target,running.committed_consent),(admission.target,observed.final_consent)):
        current=observe_fixture_learning_consent(connection,target.fixture,expected_database=replay.database,
            revoked=True,assert_fixture_scope=guard,check_active=check_active)
        if current!=expected:raise ValueError('RESTORE_COMBINED_REVOKE_CHANGED')
    settled=observe_fixture_cancelled_render(connection,running_target.fixture,running.running,
        assert_fixture_scope=guard,check_active=check_active)
    if settled!=running.cancelled:raise ValueError('RESTORE_COMBINED_REVOKE_RESULT_CHANGED')
    renderer_barrier.assert_cancelled(running.running)
    guard();finished=datetime.now(timezone.utc)
    return persist_restore_probe_report(root,replay,'revoke_negative',fixture_sha256,started,finished,
        {'fixture':descriptor,'running':asdict(running),'admission':asdict(observed)},check_active=guard)
