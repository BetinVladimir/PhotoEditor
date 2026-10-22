"""Shared deterministic preview-schema projection for both contract generators.

Projection only: no operation enablement or contract-pin compatibility changes.
"""
from copy import deepcopy

from .semantic_preview import (
 ShownBackgroundCleanupAdoptionInput,
    SemanticRetouchPreviewInput, RetouchPreviewNumericalEvidence,
    RetouchPreviewPresentation, ShownRetouchAdoptionInput,
    SemanticExposurePreviewInput, ExposurePreviewNumericalEvidence,
    ExposurePreviewPresentation, ShownExposureAdoptionInput,
    SemanticRemovalPreviewInput, RemovalPreviewNumericalEvidence,
    RemovalPreviewPresentation, ShownRemovalAdoptionInput,
    SemanticBackgroundCleanupPreviewInput, BackgroundCleanupPreviewNumericalEvidence,
    BackgroundCleanupPreviewPresentation,
)

from .semantic_retouch import RetouchPreviewAdapterContext

PREVIEW_MODELS = (
    ('semantic-retouch-preview-input', SemanticRetouchPreviewInput),
    ('retouch-preview-numerical-evidence', RetouchPreviewNumericalEvidence),
    ('retouch-preview-presentation', RetouchPreviewPresentation),
    ('shown-retouch-adoption-input', ShownRetouchAdoptionInput),
    ('retouch-preview-adapter-context', RetouchPreviewAdapterContext),
 ('shown-background-cleanup-adoption-input', ShownBackgroundCleanupAdoptionInput),
    ('semantic-exposure-preview-input', SemanticExposurePreviewInput),
    ('exposure-preview-numerical-evidence', ExposurePreviewNumericalEvidence),
    ('exposure-preview-presentation', ExposurePreviewPresentation),
    ('shown-exposure-adoption-input', ShownExposureAdoptionInput),
    ('semantic-removal-preview-input', SemanticRemovalPreviewInput),
    ('removal-preview-numerical-evidence', RemovalPreviewNumericalEvidence),
    ('removal-preview-presentation', RemovalPreviewPresentation),
    ('shown-removal-adoption-input', ShownRemovalAdoptionInput),
    ('semantic-background-cleanup-preview-input', SemanticBackgroundCleanupPreviewInput),
    ('background-cleanup-preview-numerical-evidence', BackgroundCleanupPreviewNumericalEvidence),
    ('background-cleanup-preview-presentation', BackgroundCleanupPreviewPresentation),
)


def standalone_schema(name, model):
    return model.model_json_schema(by_alias=True) | {
        '$schema': 'https://json-schema.org/draft/2020-12/schema',
        '$id': f'https://photoeditor.local/contracts/{name}.schema.json',
    }


def rewrite_refs(value):
    if isinstance(value, list):
        return [rewrite_refs(item) for item in value]
    if not isinstance(value, dict):
        return value
    result = {key: rewrite_refs(item) for key, item in value.items()}
    ref = result.get('$ref')
    if isinstance(ref, str) and ref.startswith('#/$defs/'):
        result['$ref'] = '#/$defs/SemanticPreview_' + ref.removeprefix('#/$defs/')
    return result


def project_bundle(bundle):
    """Own the output and preserve definitions outside this model family.

    Shared dependency definitions must agree across all preview models. Otherwise
    regeneration fails before either entrypoint writes any output.
    """
    result = deepcopy(bundle)
    projected = {}
    for _, model in PREVIEW_MODELS:
        schema = rewrite_refs(model.model_json_schema(by_alias=True))
        dependencies = schema.pop('$defs', {})
        definitions = {'SemanticPreview_' + key: value for key, value in dependencies.items()}
        definitions[model.__name__] = schema
        for key, value in definitions.items():
            if key in projected and projected[key] != value:
                raise ValueError('PREVIEW_SCHEMA_DEPENDENCY_CONFLICT: ' + key)
            projected[key] = value
    result['$defs'].update(projected)
    return result
