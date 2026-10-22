"""Verify a sealed identity base without reapplying already executed style.

The caller must bind the receipt through live task/replay authority. Possession
of JSON or equality of image hashes alone is never sufficient authority.
"""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.editor_effects import EditorExecutedBaseReceipt
from photo_worker.operations.optical.contracts import ImageResult, ImageReport
from .render_recipe import FrozenRenderRecipe


def qualify_executed_base(artifacts, frozen, *, authorize, check_active):
    ref = frozen.recipe.executed_base_receipt
    if ref is None:
        return None
    if not callable(authorize):
        raise ValueError('RENDER_EXECUTED_BASE_AUTHORITY_REQUIRED')
    def owned(reference, model):
        check_active()
        result = model.model_validate_json(canonical_bytes(artifacts.contract(reference, model)))
        check_active()
        return result
    receipt = owned(ref, EditorExecutedBaseReceipt)
    origin = owned(receipt.original_recipe, FrozenRenderRecipe)
    result = owned(receipt.image_result, ImageResult)
    report = owned(result.report, ImageReport)
    recipe = frozen.recipe
    if (receipt.processed_source != recipe.source or origin.recipe.source != receipt.original_source
            or recipe.base_style != receipt.executed_style
            or result.working_image != recipe.source.source
            or report.operation != 'editor.effects_seal' or report.status not in ('pass', 'review')
            or report.sources != [recipe.source] or report.outside_mask_max_error != 0
            or report.identity is not None or report.transform is not None
            or report.registrations or report.parent_reports
            or receipt.generative_requested != (result.generated_mask is not None)
            or (receipt.generative_requested or receipt.inherited_review) and report.status != 'review'
            or recipe.layer_stack is not None or recipe.protected_mask is not None
            or recipe.overrides or recipe.session_corrections is not None
            or recipe.learning_version_id is not None or frozen.learning is not None):
        raise ValueError('RENDER_EXECUTED_BASE_BINDING_INVALID')
    delta = recipe.parameter_delta
    if any(value != 0 for value in delta.model_dump().values()):
        raise ValueError('RENDER_EXECUTED_BASE_DELTA_REAPPLIED')
    if authorize(ref, receipt, origin, result, report) is not True:
        raise ValueError('RENDER_EXECUTED_BASE_AUTHORITY_REQUIRED')
    check_active()
    return receipt, result, report


def qualify_scoped_executed_base(artifacts, inputs, scoped, check_active):
    """Authority is the primary DB snapshot, never provider supplied evidence."""
    recipe_ref = inputs.recipe.recipe.executed_base_receipt
    authority = scoped.executed_base_receipt
    if recipe_ref is None and authority is None:
        return None
    if (recipe_ref is None or authority is None
            or recipe_ref.artifact_id != authority.artifact_id or recipe_ref.sha256 != authority.sha256):
        raise ValueError('RENDER_EXECUTED_BASE_AUTHORITY_REQUIRED')
    def authorize(ref, receipt, origin, result, report):
        return any(parent.artifact_id == receipt.image_result.artifact_id
            and parent.sha256 == receipt.image_result.sha256 for parent in scoped.parent_roots)
    return qualify_executed_base(artifacts, inputs.recipe, authorize=authorize, check_active=check_active)
