"""Explicit operator installation of the read-only line evidence producer."""
from dataclasses import dataclass
import re

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.runtime.executor import Operation, OperationFailure
from .perspective_lines_runtime_lock import PerspectiveLinesRuntimeLock, verify_installed_runtime
from .perspective_line_detection import perspective_line_detection_operations


@dataclass(frozen=True)
class PerspectiveLinesInstallation:
    runtime_lock: PerspectiveLinesRuntimeLock
    runtime_sha: str
    working_icc_sha: str


def execution_policy(working_icc_sha, runtime_sha):
    if any(not isinstance(value, str) or not re.fullmatch('[0-9a-f]{64}', value)
           for value in (working_icc_sha, runtime_sha)):
        raise ValueError('PERSPECTIVE_LINES_INSTALLATION_INVALID')
    return dict(schema_version=1, operation='optical.perspective_lines', operation_version=1,
        backend='cpu', precision='float32', renderer_version='v2.2', working_icc_sha=working_icc_sha,
        runtime_lock_sha=runtime_sha, max_pixels=200000000, proxy_edge=1600, max_lines=128)


def installed_perspective_line_operations(config, installation):
    if not isinstance(installation, PerspectiveLinesInstallation):
        raise ValueError('PERSPECTIVE_LINES_INSTALLATION_INVALID')
    # Own serialized bytes and scalar pins; caller mutation cannot repin tasks.
    runtime_sha = installation.runtime_sha
    working_icc_sha = installation.working_icc_sha
    policy_sha = canonical_hash(execution_policy(working_icc_sha, runtime_sha))
    lock_bytes = canonical_bytes(installation.runtime_lock)
    lock = PerspectiveLinesRuntimeLock.model_validate_json(lock_bytes)
    verify_installed_runtime(lock, runtime_sha)
    producer = perspective_line_detection_operations(config, working_icc_sha=working_icc_sha)[0]

    def run(ctx):
        command = ctx.lease.command
        profile = ctx.lease.execution_profile
        if (command.operation != 'optical.perspective_lines' or command.operation_version != '1'
                or command.resource_class != 'cpu' or not isinstance(profile, dict)
                or profile.get('backend') != 'cpu' or profile.get('precision') != 'float32'
                or profile.get('renderer_version') != 'v2.2'
                or profile.get('runtime_lock_sha') != runtime_sha
                or profile.get('execution_policy_sha') != policy_sha):
            raise OperationFailure('CONFIG_INVALID')
        ctx.check_active()
        verify_installed_runtime(PerspectiveLinesRuntimeLock.model_validate_json(lock_bytes), runtime_sha)
        ctx.check_active()
        result = producer.execute(ctx)
        ctx.check_active()
        verify_installed_runtime(PerspectiveLinesRuntimeLock.model_validate_json(lock_bytes), runtime_sha)
        ctx.check_active()
        return result

    return [Operation('optical.perspective_lines', 'cpu', run)]
