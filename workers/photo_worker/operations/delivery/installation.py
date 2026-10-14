"""Production delivery installation: use the worker's own SQL authority."""
from .adapter import DeliveryInstallation
from .admission import SQLDeliveryAdmission
from .signed_adapter import SQLSignedDeliveryAdmission, SignedDeliveryInstallation


def delivery_enabled_from_env(env, resource_class):
    value = env.get('PHOTO_DELIVERY_ENABLED', 'false')
    if value not in ('true', 'false'):
        raise ValueError('PHOTO_DELIVERY_ENABLED must be true or false')
    enabled = value == 'true'
    if enabled and resource_class != 'cpu':
        raise ValueError('Delivery runtime requires CPU worker')
    return enabled


def resolve_delivery_installation(database, enabled, supplied=None):
    """Explicit custom installations and environment enablement are exclusive.

    Source readiness remains SQL-gated. In particular installing an assembly
    handler does not authorize assembly while its SQL source gate is closed.
    """
    if type(enabled) is not bool:
        raise ValueError('DELIVERY_CONFIGURATION_INVALID')
    if enabled and supplied is not None:
        raise ValueError('DELIVERY_INSTALLATION_CONFLICT')
    if not enabled:
        return supplied
    return DeliveryInstallation(SQLDeliveryAdmission(database),
        signed=SignedDeliveryInstallation(SQLSignedDeliveryAdmission(database)))
