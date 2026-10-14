"""Lease-bound adapter for the approved local-exposure execution boundary.

The installed database authority must independently check the committed child,
source pins and worker lease. This adapter never grants authority by itself.
"""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic import ApprovedSemanticEdit


class SQLLocalExposureAdmission:
    def __init__(self, database, ctx):
        authority = getattr(database, 'admit_semantic_local_exposure', None)
        if not callable(authority):
            raise ValueError('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
        self.authority = authority
        self.ctx = ctx

    def __call__(self, edit: ApprovedSemanticEdit) -> bool:
        self.ctx.check_active()
        lease = self.ctx.lease
        command = lease.command
        raw = canonical_bytes(edit)
        owned = ApprovedSemanticEdit.model_validate_json(raw)
        if (owned.kind != 'local_exposure'
                or command.operation != 'semantic.local_exposure'
                or command.operation_version != '1'
                or command.resource_class != 'cpu'
                or lease.execution_profile.get('backend') != 'cpu'
                or lease.execution_profile.get('renderer_version') != 'v2.2'
                or len(raw) > 1048576
                or sha256(raw).hexdigest() != command.params.sha256):
            return False
        accepted = self.authority(lease, raw) is True
        self.ctx.check_active()
        return accepted
