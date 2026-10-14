"""Resolve a process installer only after the scheduler connection is open."""

from .interchange_import_adopter_installation import SchedulerImportRecipeAdoptionInstallation
from .adoption import require_adoption_active


def build_interchange_import_adopter(builder, read_connection, stop):
    if not callable(builder) or not callable(getattr(stop, 'is_set', None)):
        raise ValueError('INTERCHANGE_IMPORT_ADOPTER_BUILDER')
    installation = builder(read_connection, stop)
    if (type(installation) is not SchedulerImportRecipeAdoptionInstallation
            or installation.connection is not read_connection):
        raise ValueError('INTERCHANGE_IMPORT_ADOPTER_BUILDER_RESULT')
    original_active = installation.active
    def check_active():
        if stop.is_set():
            raise InterruptedError('Adopter stopped')
        require_adoption_active(original_active)
        if stop.is_set():
            raise InterruptedError('Adopter stopped')
    installation.active = check_active
    installation.active()
    return installation
