"""Current scheduler grant and origin fence for imported person masks."""

from uuid import UUID

from photo_worker.contracts.images import MaskRef
from photo_worker.operations.session.contracts import VersionBinding
from .adoption import AdoptionInvalid


class SQLImportAdoptionPersonAuthority:
    def __init__(self, connection, check_active):
        if not connection.autocommit or not callable(check_active):
            raise ValueError('INTERCHANGE_IMPORT_PERSON_AUTHORITY_INSTALLATION')
        self.connection, self.active = connection, check_active

    def __call__(self, scope, context, mask, binding):
        if (not isinstance(scope, tuple) or len(scope) != 5
                or not isinstance(scope[0], UUID) or scope[0].int == 0
                or not isinstance(scope[1], UUID) or scope[1].int == 0
                or type(scope[2]) is not int or scope[2] < 1
                or type(scope[3]) is not int or scope[3] < 1
                or not isinstance(scope[4], UUID) or scope[4].int == 0
                or (getattr(context, 'attempt', None), getattr(context, 'fence', None),
                    getattr(context, 'result_id', None)) != scope[2:]
                or not isinstance(mask, MaskRef) or mask.selector != 'person'
                or mask.object_id is not None or mask.model_lock_id is None
                or not isinstance(binding, VersionBinding)):
            raise AdoptionInvalid('INTERCHANGE_IMPORT_PERSON_AUTHORITY_SCOPE')
        self.active()
        row = self.connection.execute(
            'SELECT photo.v30_import_adoption_person_mask_live(%s,%s,%s,%s,%s,%s,%s,%s)',
            scope + (binding.asset_version_id, mask.artifact.artifact_id,
                     mask.artifact.sha256)).fetchone()
        self.active()
        if row is None or len(row) != 1 or type(row[0]) is not int or row[0] < 1:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_PERSON_AUTHORITY_UNAVAILABLE')
        return row[0]
