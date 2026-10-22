"""Generation-scoped SQL admission; complete renderer qualification is separate."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.database import TaskDatabase
from photo_worker.runtime.executor import OperationFailure
from .contracts import RegionPreviewParams


class SQLRegionAdmission:
    def __init__(self, database: TaskDatabase):
        self.database = database

    def admit(self, ctx, params: RegionPreviewParams) -> None:
        ctx.check_active()
        lease = ctx.lease
        command = lease.command
        if (command.operation != 'region.preview' or command.operation_version != '1'
                or command.resource_class != 'gpu'
                or params.tenant_id != command.tenant_id
                or lease.execution_profile.get('backend') not in ('cuda', 'mps')
                or lease.execution_profile.get('renderer_version') != 'v2.2'):
            raise OperationFailure('REGION_SCOPE_STALE')
        raw = canonical_bytes(params)
        if len(raw) > 1048576 or sha256(raw).hexdigest() != command.params.sha256:
            raise OperationFailure('INPUT_INVALID')
        if not self.database.admit_region_preview(lease, raw):
            raise OperationFailure('REGION_SCOPE_STALE')
        ctx.check_active()
