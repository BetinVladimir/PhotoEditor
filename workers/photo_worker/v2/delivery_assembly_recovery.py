"""Acknowledge assembled delivery after its running adoption context has closed."""
from .adoption import AdoptionInvalid, require_adoption_active


def delivery_assembly_adoption_committed(connection, tenant_id, task_id, *, check_active):
    if connection.autocommit is not True or not callable(check_active):
        raise ValueError('DELIVERY_ASSEMBLY_RECOVERY_CONFIGURATION')
    require_adoption_active(check_active)
    row = connection.execute('SELECT photo.v30_delivery_assembly_adoption_committed(%s,%s)',
                             (tenant_id, task_id)).fetchone()
    require_adoption_active(check_active)
    if row not in ((True,), (False,)) or type(row[0]) is not bool:
        raise AdoptionInvalid('DELIVERY_ASSEMBLY_RECOVERY_INVALID')
    return row[0]
