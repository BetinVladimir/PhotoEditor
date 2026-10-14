"""Acknowledge an exact historical parse commit, without new authority."""
from uuid import UUID
from .adoption import AdoptionInvalid, require_adoption_active


def interchange_parse_adoption_committed(connection, tenant_id, task_id, *, check_active):
    if (connection.autocommit is not True or not callable(check_active)
            or not isinstance(tenant_id, UUID) or tenant_id.int == 0
            or not isinstance(task_id, UUID) or task_id.int == 0):
        raise ValueError('INTERCHANGE_PARSE_RECOVERY_CONFIGURATION')
    require_adoption_active(check_active)
    row = connection.execute('SELECT photo.v30_interchange_parse_adoption_committed(%s,%s)',
                             (tenant_id, task_id)).fetchone()
    require_adoption_active(check_active)
    if row not in ((True,), (False,)) or type(row[0]) is not bool:
        raise AdoptionInvalid('INTERCHANGE_PARSE_RECOVERY_INVALID')
    return row[0]
