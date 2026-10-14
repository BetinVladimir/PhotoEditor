"""Operator configured CPU parser with independent SQL lease authority."""
from .parse import ParseInstallation
from .admission import SQLParseAdmission


def interchange_enabled_from_env(env, resource_class):
    value = env.get("PHOTO_INTERCHANGE_ENABLED", "false")
    if value not in ("true", "false"):
        raise ValueError("PHOTO_INTERCHANGE_ENABLED must be true or false")
    enabled = value == "true"
    if enabled and resource_class != "cpu":
        raise ValueError("Interchange runtime requires CPU worker")
    return enabled


def resolve_interchange_installation(database, enabled, supplied=None):
    if type(enabled) is not bool:
        raise ValueError("INTERCHANGE_CONFIGURATION_INVALID")
    if enabled and supplied is not None:
        raise ValueError("INTERCHANGE_INSTALLATION_CONFLICT")
    if not enabled:
        if supplied is not None and not isinstance(supplied, ParseInstallation):
            raise ValueError("INTERCHANGE_INSTALLATION_INVALID")
        return supplied
    return ParseInstallation(SQLParseAdmission(database))


def resolve_interchange_import_installation(supplied=None):
    """Explicit injection only; parser enablement never enables import dispatch."""
    from .import_adapter import ImportInstallation
    if supplied is None:
        return None
    if (not isinstance(supplied, ImportInstallation)
            or not callable(getattr(supplied.admission, "admit", None))
            or supplied.recipe_qualifier_factory is not None
            and not callable(supplied.recipe_qualifier_factory)):
        raise ValueError("INTERCHANGE_IMPORT_INSTALLATION_INVALID")
    return supplied


def sql_import_installation(database, recipe_qualifier_factory):
    """Construct SQL authority without IO; production requires recipe qualification."""
    from .import_adapter import ImportInstallation
    from .import_admission import SQLImportAdmission
    if not callable(recipe_qualifier_factory):
        raise ValueError("INTERCHANGE_RECIPE_QUALIFIER_REQUIRED")
    return ImportInstallation(SQLImportAdmission(database), recipe_qualifier_factory)


def sql_prepared_import_installation(database, *, schemas_jcs, schemas_sha, work_icc_sha,
        qualify_runtime, extra_validators=None, max_file_bytes=128 << 30, operation_schemas=None):
    """Explicit fully typed factory composition; no environment auto-enablement."""
    from .recipe_factory import PreparedImportRecipeFactory
    from .person_mask_confirmation import SQLImportPersonMaskConfirmation
    confirmation = SQLImportPersonMaskConfirmation(database)
    factory = PreparedImportRecipeFactory(schemas_jcs=schemas_jcs, schemas_sha=schemas_sha,
        work_icc_sha=work_icc_sha, qualify_runtime=qualify_runtime,
        extra_validators=extra_validators, max_file_bytes=max_file_bytes,
        operation_schemas=operation_schemas, qualify_confirmation=confirmation)
    return sql_import_installation(database, factory)


def resolve_interchange_export_installation(supplied=None):
    """Explicit injection; parser enablement never implies export readiness."""
    from .export_adapter import ExportInstallation
    if supplied is None:
        return None
    if (not isinstance(supplied, ExportInstallation)
            or not callable(getattr(supplied.admission, 'admit', None))
            or not callable(supplied.prepare)):
        raise ValueError('INTERCHANGE_EXPORT_INSTALLATION_INVALID')
    return supplied


def sql_prepared_export_installation(database, *, schemas_jcs, schemas_sha, work_icc_sha,
        qualify_runtime, extra_validators=None, max_file_bytes=128 << 30, qualify_occurrence_other=None, operation_schemas=None):
    """Compose concrete formats and SQL authority without enabling dispatch.

    Operator deployment must qualify native dependencies and install the SQL
    export admission migration before supplying this installation to runtime.
    """
    from .recipe_factory import PreparedImportRecipeFactory
    from .export_factory import ExportPreparationFactory
    from .export_adapter import ExportInstallation
    from .export_admission import SQLExportAdmission
    recipes=PreparedImportRecipeFactory(schemas_jcs=schemas_jcs,schemas_sha=schemas_sha,
        work_icc_sha=work_icc_sha,qualify_runtime=qualify_runtime,
        extra_validators=extra_validators,max_file_bytes=max_file_bytes,operation_schemas=operation_schemas)
    occurrences=None
    if qualify_occurrence_other is not None:
        from photo_worker.v2.interchange_descriptor_installation import SQLExportDescriptorOccurrenceFactory
        occurrences=SQLExportDescriptorOccurrenceFactory(database,qualify_other=qualify_occurrence_other)
    return ExportInstallation(SQLExportAdmission(database),
        ExportPreparationFactory(recipes,occurrence_factory=occurrences))
