"""Independent restore/run pin for the installed running-consent lifecycle."""
from dataclasses import asdict
from hashlib import sha256
from uuid import UUID,uuid5
import re
from photo_worker.contracts.base import canonical_bytes
from .restore_report import _json
from .restore_replay_receipt import VerifiedRestoreReplay
from .restore_revoke_lifecycle import RestoreConsentRevokeTarget
from .restore_revoke_probe import RestoreRevokeFixture
from .restore_revoke_running_lifecycle import run_fixture_running_revoke
from .restore_render_barrier import RestoreBarrierRenderer
from .restore_authority import require_restore_fixture


def running_revoke_fixture_descriptor(replay,target,child_run_id,maximum_seconds,*,barrier_required=False):
    """Credentials excluded; consent fixture and request identity are pinned."""
    if (not isinstance(replay,VerifiedRestoreReplay) or not isinstance(target,RestoreConsentRevokeTarget)
            or not isinstance(target.fixture,RestoreRevokeFixture) or not isinstance(child_run_id,UUID)
            or child_run_id.int==0 or type(maximum_seconds) is not int or not 1<=maximum_seconds<=300
            or not isinstance(target.idempotency_key,str) or not target.idempotency_key
            or type(barrier_required) is not bool):
        raise ValueError('RESTORE_RUNNING_INSTALLATION')
    workflow=uuid5(child_run_id,'workflow')
    return _json({'schema_version':'restore-running-revoke-fixture/v1','run_id':replay.run_id,
        'database':replay.database,'build_sha256':replay.build_sha256,
        'recovery_sha256':replay.recovery_sha256,'replay_sha256':replay.replay_sha256,
        'fixture':asdict(target.fixture),'origin':target.origin,
        'revoke_key_sha256':sha256(target.idempotency_key.encode()).hexdigest(),
        'child_run_id':child_run_id,'workflow_id':workflow,'task_id':uuid5(workflow,'render.compose:0'),
        'maximum_seconds':maximum_seconds,'barrier_required':barrier_required})


def run_pinned_running_revoke(replay,fixture_sha256,connection,target,child_run_id,*,maximum_seconds,
                              tls_context,assert_installed_fixture,check_active,renderer_barrier=None):
    """Reject mismatched operator pin before IO; does not emit acceptance."""
    if (not isinstance(fixture_sha256,str) or re.fullmatch('[0-9a-f]{64}',fixture_sha256) is None
            or not callable(assert_installed_fixture) or not callable(check_active)
            or (renderer_barrier is not None and not isinstance(renderer_barrier,RestoreBarrierRenderer))):
        raise ValueError('RESTORE_RUNNING_INSTALLATION')
    descriptor=running_revoke_fixture_descriptor(replay,target,child_run_id,maximum_seconds,
        barrier_required=renderer_barrier is not None)
    if sha256(canonical_bytes(descriptor)).hexdigest()!=fixture_sha256:
        raise ValueError('RESTORE_RUNNING_FIXTURE_PIN')
    require_restore_fixture(check_active,assert_installed_fixture)
    return run_fixture_running_revoke(connection,target,child_run_id,expected_database=replay.database,
        maximum_seconds=maximum_seconds,tls_context=tls_context,
        assert_installed_fixture=assert_installed_fixture,check_active=check_active,renderer_barrier=renderer_barrier)
