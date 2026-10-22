"""Exact CPU semantic task routing before artifact or installation IO."""
from photo_worker.runtime.executor import OperationFailure


def require_cpu_semantic_route(ctx, operation):
    lease = getattr(ctx, 'lease', None)
    command = getattr(lease, 'command', None)
    profile = getattr(lease, 'execution_profile', None)
    if (getattr(command, 'operation', None) != operation
            or getattr(command, 'operation_version', None) != '1'
            or getattr(command, 'resource_class', None) != 'cpu'
            or not isinstance(profile, dict)
            or profile.get('backend') != 'cpu'
            or profile.get('renderer_version') != 'v2.2'):
        raise OperationFailure('CONFIG_INVALID')
