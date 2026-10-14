"""Read-only acknowledgement of the exact committed canonical signer result."""
from uuid import UUID
from .adoption import AdoptionInvalid


def signing_adoption_committed(connection, tenant_id, task_id, *, check_active):
    if (connection.autocommit is not True or not callable(check_active)
            or not isinstance(tenant_id, UUID) or tenant_id.int == 0
            or not isinstance(task_id, UUID) or task_id.int == 0):
        raise ValueError('SIGNING_RECOVERY_CONFIGURATION')
    def require_active():
        if check_active() is False:
            raise AdoptionInvalid('SIGNING_RECOVERY_INACTIVE')
    require_active()
    row = connection.execute('SELECT photo.v31_signing_adoption_committed(%s,%s)',
                             (tenant_id, task_id)).fetchone()
    require_active()
    if row is None or len(row) != 1 or type(row[0]) is not bool:
        raise AdoptionInvalid('SIGNING_RECOVERY_INVALID')
    return row[0]
