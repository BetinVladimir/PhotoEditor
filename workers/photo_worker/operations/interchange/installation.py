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
        qualify_runtime, extra_validators=None, max_file_bytes=128 << 30, operation_schemas=None, historical_authority=None, historical_trusted_keys=None):
    """Explicit fully typed factory composition; no environment auto-enablement."""
    from .recipe_factory import PreparedImportRecipeFactory
    if historical_trusted_keys is not None:
        if historical_authority is not None:
            raise ValueError('INTERCHANGE_HISTORICAL_INSTALLATION_CONFLICT')
        from photo_worker.v2.interchange_historical_origin import SignedHistoricalOriginAuthority
        historical_authority = SignedHistoricalOriginAuthority(historical_trusted_keys)
    from .person_mask_confirmation import SQLImportPersonMaskConfirmation
    confirmation = SQLImportPersonMaskConfirmation(database)
    factory = PreparedImportRecipeFactory(schemas_jcs=schemas_jcs, schemas_sha=schemas_sha,
        work_icc_sha=work_icc_sha, qualify_runtime=qualify_runtime,
        extra_validators=extra_validators, max_file_bytes=max_file_bytes,
        operation_schemas=operation_schemas, qualify_confirmation=confirmation, historical_authority=historical_authority)
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
        qualify_runtime, extra_validators=None, max_file_bytes=128 << 30,
        qualify_occurrence_other=None, operation_schemas=None, person_origin_factory=None,
        person_origin_trusted_keys=None, person_origin_model_lock_sha=None, historical_authority=None):
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
        extra_validators=extra_validators,max_file_bytes=max_file_bytes,operation_schemas=operation_schemas,historical_authority=historical_authority)
    occurrences=None
    if qualify_occurrence_other is not None:
        from photo_worker.v2.interchange_descriptor_installation import SQLExportDescriptorOccurrenceFactory
        occurrences=SQLExportDescriptorOccurrenceFactory(database,qualify_other=qualify_occurrence_other)
    if person_origin_trusted_keys is not None or person_origin_model_lock_sha is not None:
        if (person_origin_factory is not None or person_origin_trusted_keys is None
                or person_origin_model_lock_sha is None):
            raise ValueError('INTERCHANGE_EXPORT_PERSON_ORIGIN_INSTALLATION')
        from .person_origin_factory import DeclaredPersonOriginFactory
        person_origin_factory = DeclaredPersonOriginFactory(
            trusted_keys=person_origin_trusted_keys,
            model_lock_sha=person_origin_model_lock_sha)
    if person_origin_factory is not None and not callable(person_origin_factory):
        raise ValueError('INTERCHANGE_EXPORT_PERSON_ORIGIN_INSTALLATION')
    return ExportInstallation(SQLExportAdmission(database),
        ExportPreparationFactory(recipes,occurrence_factory=occurrences,
            person_origin_factory=person_origin_factory))
