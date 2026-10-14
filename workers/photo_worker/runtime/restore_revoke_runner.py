"""Execute a real pre-provisioned learning revoke/freeze/access lifecycle."""
from dataclasses import dataclass,field
from hashlib import sha256
from uuid import UUID
from photo_worker.runtime.restore_revoke_lifecycle import RestoreConsentRevokeTarget
from photo_worker.runtime.restore_revoke_access import RestoreRevokeDocumentTarget
from photo_worker.runtime.restore_revoke_freeze import (
    RestoreFreezeDenialTarget, RestoreFreezeBaselineBinding, observe_fixture_freeze_baseline)
from photo_worker.runtime.restore_freeze_catalogue import (
    verify_fixture_freeze_catalogue, observe_guarded_fixture_freeze_denial)
from photo_worker.runtime.restore_freeze_result import verify_fixture_frozen_result
from photo_worker.runtime.restore_revoke_observation import observe_fixture_revoke_lifecycle
from photo_worker.runtime.restore_revoke_probe import observe_fixture_learning_consent
from .restore_revoke_batch import RestoreRenderBatchTarget
from .restore_batch_catalogue import verify_fixture_batch_not_adopted,observe_guarded_fixture_batch_refusal
from .restore_batch_baseline import run_fixture_batch_baseline
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRevokeBatchInstallation:
    positive: RestoreRenderBatchTarget
    negative: RestoreRenderBatchTarget
    session_id: UUID
    maximum_seconds: int
    source_raw: bytes = field(repr=False)


@dataclass(frozen=True)
class RestoreRevokeFreezeLifecycle:
    schema_version: str
    baseline_freeze: object
    physical_freeze: object
    revoke: object
    denied_freeze: object
    final_consent: object
    positive_batch: object = None
    negative_batch: object = None


def validate_fixture_revoke_installation(revoke_target, document_target, baseline_target, denial_target, binding, *,
                                        assert_installed_fixture, check_active, batch_installation=None):
    """Pure binding validation, shared before any combined probe mutation."""
    if (not isinstance(revoke_target, RestoreConsentRevokeTarget)
            or not isinstance(document_target, RestoreRevokeDocumentTarget)
            or not isinstance(baseline_target, RestoreFreezeDenialTarget)
            or not isinstance(denial_target, RestoreFreezeDenialTarget)
            or not isinstance(binding, RestoreFreezeBaselineBinding)
            or revoke_target.fixture != document_target.fixture
            or binding.actor_id != revoke_target.fixture.actor_id
            or baseline_target.idempotency_key == denial_target.idempotency_key
            or any((t.tenant_id != revoke_target.fixture.tenant_id
                or t.origin != revoke_target.origin or t.authorization != revoke_target.authorization
                or t.document_id != document_target.document_id) for t in (baseline_target, denial_target))
            or baseline_target.asset_id != denial_target.asset_id or baseline_target.revision != denial_target.revision
            or document_target.access.origin != revoke_target.origin
            or document_target.access.authorization != revoke_target.authorization
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_REVOKE_RUNNER_BINDING')
    f = revoke_target.fixture
    if batch_installation is not None:
        b=batch_installation
        if (not isinstance(b,RestoreRevokeBatchInstallation)
                or not isinstance(b.positive,RestoreRenderBatchTarget) or not isinstance(b.negative,RestoreRenderBatchTarget)
                or not isinstance(b.session_id,UUID) or b.session_id.int==0
                or type(b.maximum_seconds) is not int or not 1<=b.maximum_seconds<=300
                or not isinstance(b.source_raw,bytes) or not 2<=len(b.source_raw)<=1048576
                or sha256(b.source_raw).hexdigest()!=binding.source_sha256
                or b.positive.idempotency_key==b.negative.idempotency_key
                or b.positive.snapshot_id!=b.negative.snapshot_id or b.positive.policy_sha256!=b.negative.policy_sha256
                or any(t.tenant_id!=f.tenant_id or t.project_id!=f.project_id or t.origin!=revoke_target.origin
                    or t.authorization!=revoke_target.authorization for t in (b.positive,b.negative))):
            raise ValueError('RESTORE_REVOKE_RUNNER_BATCH_BINDING')
    return f


def run_fixture_revoke_freeze_lifecycle(connection, client, buckets, revoke_target, document_target,
                                        baseline_target, denial_target, binding, *, expected_database,
                                        tls_context, assert_installed_fixture, check_active, batch_installation=None):
    """Fresh positive freeze -> physical proof -> revoke/access -> fresh freeze refusal.

    Optional batch installation requires completed physical worker output before
    revoke and guarded fresh admission refusal afterwards. Neither mode qualifies
    concurrent revoke races or authorizes restore acceptance/writers. On interruption
    independently reconcile committed operations; never blindly replay runner.
    """
    f=validate_fixture_revoke_installation(revoke_target,document_target,baseline_target,denial_target,binding,
        assert_installed_fixture=assert_installed_fixture,check_active=check_active,batch_installation=batch_installation)
    b=batch_installation
    def guard():
        require_restore_fixture(check_active,assert_installed_fixture)
    def scope(*_): guard()
    def catalogue(t):
        return verify_fixture_freeze_catalogue(connection, t, f, binding.asset_version_id,
            document_target.document_sha256, expected_database=expected_database,
            assert_fixture_scope=scope, check_active=check_active)
    def positive_guard(t):
        guard()
        observe_fixture_learning_consent(connection, f, expected_database=expected_database,
            revoked=False, assert_fixture_scope=scope, check_active=check_active)
        catalogue(t)
    guard()
    # Both keys must be unused before the first mutation.
    positive_guard(baseline_target); catalogue(denial_target)
    if batch_installation is not None:
        for t in (b.positive,b.negative):
            verify_fixture_batch_not_adopted(connection,t,binding,expected_database=expected_database,session_id=b.session_id,
                assert_fixture_scope=scope,check_active=check_active)
    # The baseline key becomes committed during POST; only preflight checks it.
    baseline = observe_fixture_freeze_baseline(baseline_target, binding, tls_context=tls_context,
        assert_current_fixture=scope, check_active=check_active)
    physical = verify_fixture_frozen_result(connection, client, buckets, baseline_target, binding, f, baseline,
        document_target.document_sha256, expected_database=expected_database,
        assert_fixture_scope=scope, check_active=check_active)
    catalogue(denial_target)
    positive_batch=None;negative_batch=None
    if batch_installation is not None:
        positive_batch=run_fixture_batch_baseline(connection,client,buckets,b.positive,binding,baseline,b.source_raw,
            expected_database=expected_database,session_id=b.session_id,tls_context=tls_context,maximum_seconds=b.maximum_seconds,
            assert_installed_fixture=guard,check_active=check_active)
    revoked = observe_fixture_revoke_lifecycle(connection, revoke_target, document_target,
        expected_database=expected_database, tls_context=tls_context,
        assert_installed_fixture=guard, check_active=check_active)
    denied = observe_guarded_fixture_freeze_denial(connection, denial_target, f, binding.asset_version_id,
        document_target.document_sha256, expected_database=expected_database, tls_context=tls_context,
        assert_fixture_scope=scope, check_active=check_active)
    if batch_installation is not None:
        negative_batch=observe_guarded_fixture_batch_refusal(connection,b.negative,binding,baseline,f,
            expected_database=expected_database,session_id=b.session_id,tls_context=tls_context,
            assert_fixture_scope=scope,check_active=check_active)
    final = observe_fixture_learning_consent(connection, f, expected_database=expected_database,
        revoked=True, assert_fixture_scope=scope, check_active=check_active)
    if final != revoked.final_consent: raise ValueError('RESTORE_REVOKE_RUNNER_CHANGED')
    guard()
    return RestoreRevokeFreezeLifecycle('restore-revoke-freeze-lifecycle/v1', baseline, physical, revoked, denied, final,
        positive_batch,negative_batch)


def run_fixture_revoke_batch_lifecycle(*args,batch_installation,**kwargs):
    if not isinstance(batch_installation,RestoreRevokeBatchInstallation):
        raise ValueError('RESTORE_REVOKE_RUNNER_BATCH_REQUIRED')
    return run_fixture_revoke_freeze_lifecycle(*args,batch_installation=batch_installation,**kwargs)
