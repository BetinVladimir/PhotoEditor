"""Resolve a process installer only after the scheduler connection is open."""

from .interchange_import_adopter_installation import SchedulerImportRecipeAdoptionInstallation


def build_interchange_import_adopter(builder, read_connection, stop):
    if not callable(builder):
        raise ValueError('INTERCHANGE_IMPORT_ADOPTER_BUILDER')
    installation = builder(read_connection, stop)
    if (type(installation) is not SchedulerImportRecipeAdoptionInstallation
            or installation.connection is not read_connection):
        raise ValueError('INTERCHANGE_IMPORT_ADOPTER_BUILDER_RESULT')
    return installation
