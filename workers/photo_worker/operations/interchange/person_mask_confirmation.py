"""Live, leased SQL authority for an imported prepared whole-person mask."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import MaskRef
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.interchange_plan import InterchangeImportParams
from .import_adapter import ImportAuthority


class SQLImportPersonMaskConfirmation:
    def __init__(self, database):
        if (not callable(getattr(database, 'interchange_import_person_mask_live', None))
                or not callable(getattr(database, 'interchange_import_person_mask_ready', None))
                or database.interchange_import_person_mask_ready() is not True):
            raise ValueError('INTERCHANGE_MASK_CONFIRMATION_INSTALLATION')
        self.database = database

    def __call__(self, ctx, artifacts, authority, mask, binding):
        ctx.check_active()
        if (not isinstance(authority, ImportAuthority)
                or not isinstance(mask, MaskRef)
                or not isinstance(binding, VersionBinding)
                or mask.selector != 'person' or mask.object_id is not None
                or mask.model_lock_id is None
                or binding.asset_version_id.int == 0):
            return None
        params = artifacts.params(InterchangeImportParams)
        raw = canonical_bytes(params)
        command = ctx.lease.command
        if (params.evidence is None or len(raw) > 65536
                or sha256(raw).hexdigest() != command.params.sha256):
            return None
        epoch = self.database.interchange_import_person_mask_live(
            ctx.lease, raw, authority.workflow_id, authority.project_id,
            binding.asset_version_id, mask.artifact.artifact_id,
            mask.artifact.sha256)
        ctx.check_active()
        return epoch if type(epoch) is int and epoch > 0 else None
