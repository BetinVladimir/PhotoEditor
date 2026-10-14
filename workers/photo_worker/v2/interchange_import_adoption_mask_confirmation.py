"""Live scheduler authority for confirmed imported whole-person masks."""

from photo_worker.contracts.images import MaskRef
from photo_worker.operations.session.contracts import VersionBinding


class SchedulerImportPersonMaskConfirmation:
    def __init__(self, connection, scope, check_active, extra=None):
        if (not connection.autocommit or not isinstance(scope, tuple) or len(scope) != 5
                or not callable(check_active) or extra is not None and not callable(extra)):
            raise ValueError('INTERCHANGE_IMPORT_MASK_CONFIRMATION_INSTALLATION')
        self.connection, self.scope = connection, scope
        self.active, self.extra = check_active, extra
        self.epochs = {}

    def __call__(self, context, mask, binding):
        if (not isinstance(mask, MaskRef) or not isinstance(binding, VersionBinding)
                or mask.selector != 'person' or mask.object_id is not None
                or mask.model_lock_id is None):
            return False
        self.active()
        row = self.connection.execute(
            'SELECT photo.v30_import_adoption_person_mask_live(%s,%s,%s,%s,%s,%s,%s,%s)',
            self.scope + (binding.asset_version_id, mask.artifact.artifact_id,
                          mask.artifact.sha256)).fetchone()
        self.active()
        if row is None or type(row[0]) is not int or row[0] < 1:
            return False
        key = (binding.asset_version_id, mask.artifact.artifact_id,
               mask.artifact.sha256)
        if self.epochs.setdefault(key, row[0]) != row[0]:
            return False
        if self.extra is None:
            return True
        accepted = self.extra(context, mask, binding)
        self.active()
        return accepted is True
