"""Process-owned V23 identity runtime authority."""
from __future__ import annotations

from copy import deepcopy
from uuid import UUID

from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.semantic_qualification_pin import _sha


def identity_runtime_authority(measure, *, model_lock_sha: str,
                               runtime_lock_sha: str,
                               device_profile_sha: str, backend: str):
    expected = dict(model_lock_sha=model_lock_sha,
                    runtime_lock_sha=runtime_lock_sha,
                    device_profile_sha=device_profile_sha, backend=backend)
    if (not callable(measure) or backend not in ('cpu', 'cuda') or
            any(not _sha(expected[key]) for key in
                ('model_lock_sha', 'runtime_lock_sha', 'device_profile_sha'))):
        raise ValueError('IDENTITY_RUNTIME_INSTALLATION_REQUIRED')

    def valid_uuid(value):
        try:
            return isinstance(value, str) and str(UUID(value)) == value and UUID(value).int != 0
        except (ValueError, TypeError, AttributeError):
            return False

    def valid_ref(value):
        return (isinstance(value, dict) and set(value) == {'artifact_id', 'sha256'} and
                valid_uuid(value['artifact_id']) and _sha(value['sha256']))

    def verify(ctx):
        ctx.check_active()
        lease = deepcopy(ctx.lease)
        profile = lease.execution_profile
        if (any(profile.get(key) != value for key, value in expected.items()) or
                not all(isinstance(profile.get(key), str) and
                        _sha(profile[key]) for key in ('dataset_sha',)) or
                not all(valid_uuid(profile.get(key)) for key in
                        ('qualification_policy_id', 'qualification_id')) or
                not valid_ref(profile.get('qualification_report')) or
                not valid_ref(profile.get('calibration'))):
            raise OperationFailure('IDENTITY_RUNTIME_UNQUALIFIED')
        measured = deepcopy(measure())
        ctx.check_active()
        if measured != expected or ctx.lease != lease:
            raise OperationFailure('IDENTITY_RUNTIME_UNQUALIFIED')
        return True

    return verify
