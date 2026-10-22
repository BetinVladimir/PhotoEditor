"""Publish an identity operation over an independently admitted executed base.

Pixel editing already happened in the canonical Apply stage trace. This stage
retains its original recipe/style/quality provenance and measures the processed
pixels; it never reruns style or treats missing generated coverage as zero.
"""
from contextlib import ExitStack, contextmanager
from dataclasses import dataclass
import numpy as np
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.editor_effects import EditorEffectsSealParams, EditorExecutedBaseReceipt
from photo_worker.contracts.images import WorkingImage, MaskRef
from photo_worker.contracts.quality import QualityReport
from photo_worker.contracts.style import StyleV2
from photo_worker.operations.optical.contracts import ImageReport, ImageResult
from photo_worker.runtime.arrays import read_array, read_alpha_array
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation, OperationFailure, OperationResult
from photo_worker.v2.render_recipe import FrozenRenderRecipe


from .effects_compute import (PreparedEffectsSeal, ComputedEffectsInputs,
    ComputedCoverageStep, prepare_computed_effects)


@contextmanager
def prepare_effects_seal(artifacts, params, profile, check_active=lambda: None):
    # Normal runtime retains strict Params, SQL admission and both admission
    # checks in execute_effects_seal. Only pure byte computation is shared.
    params=EditorEffectsSealParams.model_validate_json(canonical_bytes(params))
    value=ComputedEffectsInputs(original_recipe=params.original_recipe,
        original_source=params.original_source,processed_source=params.processed_source,
        quality=params.quality,executed_style=params.executed_style,
        generative_requested=params.generative_requested,generated=params.generated,
        inherited_review=params.inherited_review,coverage_trace=tuple(
            ComputedCoverageStep(step.task_id,step.operation,step.source,step.output,step.report)
            for step in params.coverage_trace))
    with prepare_computed_effects(artifacts,value,profile,check_active) as prepared:
        yield prepared


def execute_effects_seal(ctx, artifacts, admission):
    params = artifacts.params(EditorEffectsSealParams)
    admission.admit(ctx, params)
    with prepare_effects_seal(artifacts, params, ctx.lease.execution_profile, ctx.check_active) as prepared:
        # Admission repeats after physical IO and before the first upload.
        admission.admit(ctx, params)
        pixels = artifacts.stage(prepared.changed_path, prepared.reservation, 'changed_pixels', 'application/octet-stream')
        changed = MaskRef(artifact=pixels, canvas=prepared.image.canvas, transform_chain=prepared.image.transform_chain,
            object_id=None, selector='manual', confidence=1, model_lock_id=None, protected=False)
        changed_ref = artifacts.stage_contract(changed, 'changed_mask')
        generated=prepared.generated;generated_pixels_ref=None;generated_written=0
        if params.generative_requested and prepared.generated_path is not None:
            generated_pixels_ref=artifacts.stage(prepared.generated_path,prepared.generated_reservation,'generated_pixels','application/octet-stream')
            generated_written=prepared.generated_path.stat().st_size
            generated=MaskRef(artifact=generated_pixels_ref,canvas=prepared.image.canvas,transform_chain=prepared.image.transform_chain,
                object_id=None,selector='generated',confidence=1,model_lock_id=None,protected=False)
        generated_ref = None if generated is None else artifacts.stage_contract(generated, 'generated_mask')
        report_ref = artifacts.stage_contract(prepared.report, 'image_report')
        result = ImageResult(schema_version=1, working_image=params.processed_source.source,
            report=report_ref, changed_mask=changed_ref, generated_mask=generated_ref)
        result_ref = artifacts.stage_contract(result, 'image_result')
        receipt = EditorExecutedBaseReceipt(schema_version=1, build_run_id=params.build_run_id,
            original_recipe=params.original_recipe, original_source=params.original_source,
            processed_source=params.processed_source, executed_style=params.executed_style,
            compilation_sha=params.compilation_sha, trace_sha=params.trace_sha, quality=params.quality,
            image_result=result_ref, generative_requested=params.generative_requested, inherited_review=params.inherited_review)
        receipt_ref = artifacts.stage_contract(receipt, 'executed_base_receipt')
        outputs = [pixels, changed_ref, *([] if generated_pixels_ref is None else [generated_pixels_ref]), *([] if generated_ref is None else [generated_ref]), report_ref, result_ref, receipt_ref]
        size = generated_written + prepared.changed_path.stat().st_size + sum(len(canonical_bytes(doc)) for doc in
            [changed, *([] if generated is None else [generated]), prepared.report, result, receipt])
        return OperationResult(outputs=outputs, bytes_written=size, review_required=prepared.report.status=='review')


def effects_seal_operations(config, admission):
    if not callable(getattr(admission, 'admit', None)): raise ValueError('EDITOR_EFFECTS_SEAL_ADMISSION_REQUIRED')
    def execute(ctx):
        try:
            with TaskArtifacts.create(ctx, config, frozenset({'changed_pixels','changed_mask','generated_pixels','generated_mask','image_report','image_result','executed_base_receipt'})) as artifacts:
                return execute_effects_seal(ctx, artifacts, admission)
        except ValueError as error: raise OperationFailure('INPUT_INVALID') from error
    return [Operation('editor.effects_seal', 'cpu', execute)]
