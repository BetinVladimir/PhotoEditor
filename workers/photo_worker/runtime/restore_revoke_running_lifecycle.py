"""Bounded native revoke of an independently installed running render fixture."""
from dataclasses import dataclass
from time import monotonic,sleep
from .restore_revoke_lifecycle import RestoreConsentRevokeTarget,request_fixture_consent_revoke
from .restore_revoke_probe import observe_fixture_learning_consent
from .restore_revoke_running import observe_fixture_running_render,observe_fixture_cancelled_render
from .restore_render_barrier import RestoreBarrierRenderer
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRunningRevokeObservation:
    running: object
    receipt: object
    committed_consent: object
    cancelled: object
    polls: int
    barrier_cancelled: bool = False


def run_fixture_running_revoke(connection,target,child_run_id,*,expected_database,tls_context,
                               maximum_seconds,assert_installed_fixture,check_active,renderer_barrier=None):
    """One revoke POST, readonly cancellation polling; no successful probe report.

    Observing running before POST cannot prove the worker is held at a precise
    execution barrier during commit. That stronger race qualification remains a
    separate requirement. Interrupted POST must be independently reconciled.
    """
    if (not isinstance(target,RestoreConsentRevokeTarget)
            or (renderer_barrier is not None and not isinstance(renderer_barrier,RestoreBarrierRenderer))
            or type(maximum_seconds) is not int or not 1<=maximum_seconds<=300
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_RUNNING_REVOKE_INSTALLATION')
    def guard(*_):require_restore_fixture(check_active,assert_installed_fixture)
    common=dict(expected_database=expected_database,assert_fixture_scope=guard,check_active=check_active)
    guard()
    observe_fixture_learning_consent(connection,target.fixture,revoked=False,**common)
    running=observe_fixture_running_render(connection,target.fixture,child_run_id,**common)
    current=observe_fixture_running_render(connection,target.fixture,child_run_id,**common)
    if current!=running:raise ValueError('RESTORE_RUNNING_REVOKE_CHANGED')
    if renderer_barrier is not None:renderer_barrier.assert_held(running)
    receipt=request_fixture_consent_revoke(target,tls_context=tls_context,
        assert_fixture_scope=guard,check_active=check_active)
    committed=observe_fixture_learning_consent(connection,target.fixture,revoked=True,**common)
    deadline=monotonic()+maximum_seconds;polls=0
    while True:
        guard()
        if monotonic()>=deadline:raise ValueError('RESTORE_RUNNING_REVOKE_TIMEOUT')
        polls+=1
        try:
            cancelled=observe_fixture_cancelled_render(connection,target.fixture,running,
                assert_fixture_scope=guard,check_active=check_active)
        except ValueError as error:
            if str(error)!='RESTORE_CANCELLED_RENDER_NOT_SETTLED':raise
            if monotonic()>=deadline:raise ValueError('RESTORE_RUNNING_REVOKE_TIMEOUT') from None
            sleep(min(.25,max(0,deadline-monotonic())))
            continue
        if monotonic()>=deadline:raise ValueError('RESTORE_RUNNING_REVOKE_TIMEOUT')
        break
    final=observe_fixture_learning_consent(connection,target.fixture,revoked=True,**common)
    if final!=committed:raise ValueError('RESTORE_RUNNING_REVOKE_CONSENT_CHANGED')
    if renderer_barrier is not None:renderer_barrier.assert_cancelled(running)
    guard()
    return RestoreRunningRevokeObservation(running,receipt,committed,cancelled,polls,renderer_barrier is not None)
