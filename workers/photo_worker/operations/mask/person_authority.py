"""Deployment measurement boundary for qualification-pinned person execution."""
from copy import deepcopy
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.semantic_qualification_pin import _sha


def person_runtime_authority(measure, *, model_lock_sha, runtime_lock_sha, device_profile_sha, backend):
    """measure obtains live trusted installed identities, never task values.

    This factory does not synthesize measurements or qualify scientific metrics.
    The provider must verify actual installed bytes/device and fail on mismatch.
    """
    expected=dict(model_lock_sha=model_lock_sha,runtime_lock_sha=runtime_lock_sha,
                  device_profile_sha=device_profile_sha,backend=backend)
    if not callable(measure) or backend not in ('cpu','cuda','mps') or any(
            not _sha(expected[key]) for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha')):
        raise ValueError('PERSON_RUNTIME_INSTALLATION_REQUIRED')
    def verify(ctx):
        ctx.check_active()
        lease=deepcopy(ctx.lease)
        if any(lease.execution_profile.get(key)!=value for key,value in expected.items()):
            raise OperationFailure('PERSON_RUNTIME_UNQUALIFIED')
        measured=deepcopy(measure())
        ctx.check_active()
        if measured!=expected or ctx.lease!=lease:
            raise OperationFailure('PERSON_RUNTIME_UNQUALIFIED')
        return True
    return verify
