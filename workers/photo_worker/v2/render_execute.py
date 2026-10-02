"""Run full authored preparation and measured, quality-gated attempt staging."""
from dataclasses import dataclass
from photo_worker.operations.optical.contracts import ImageReport, ImageResult
from .render_authored import prepare_authored_render
from .render_publish import publish_prepared_render


@dataclass(frozen=True)
class MeasuredAuthoredRender:
    report: ImageReport
    parents: tuple[ImageResult, ...]


def execute_authored_render(artifacts, inputs, operators, scalar_overrides, render_base,
                            render_proposal, verify_base_runtime, measure, *,
                            check_active=lambda: None, **preparation):
    """Stage a complete result while input/output owners remain live.

    The qualified measurement provider receives the exact prepared style and
    final pixels; it must supply actual metrics and every provider parent.
    This function does not synthesize pass metrics, qualify providers, register
    operations or commit/adopt a result in the database.
    """
    check_active()
    if not callable(measure):
        raise ValueError('RENDER_MEASUREMENT_PROVIDER_INVALID')
    with prepare_authored_render(artifacts, inputs, operators, scalar_overrides,
            render_base, render_proposal, verify_base_runtime,
            check_active=check_active, **preparation) as prepared:
        check_active()
        evidence = measure(inputs, prepared)
        check_active()
        if (not isinstance(evidence, MeasuredAuthoredRender)
                or not isinstance(evidence.report, ImageReport)
                or not isinstance(evidence.parents, tuple)
                or any(not isinstance(parent, ImageResult) for parent in evidence.parents)):
            raise ValueError('RENDER_MEASUREMENT_EVIDENCE_INVALID')
        return publish_prepared_render(artifacts, inputs, prepared.render,
            evidence.report, evidence.parents,
            tile_size=preparation.get('tile_size', 512), check_active=check_active)


def execute_pinned_authored_render(artifacts, inputs, operators, scalar_overrides,
                                   render_base, render_proposal, verify_base_runtime,
                                   measure, *, runtime_lock_path, runtime_lock_sha,
                                   check_active=lambda: None, **preparation):
    """Verify composition build and providers after admission, before input IO.

    The externally approved composition pin is mandatory for this entry point.
    Provider/model qualification remains the required independent callback.
    """
    from .render_runtime import load_render_runtime_lock
    if not callable(verify_base_runtime):
        raise ValueError('RENDER_BASE_PROVIDER_INVALID')
    def verify():
        check_active()
        load_render_runtime_lock(runtime_lock_path, runtime_lock_sha)
        check_active()
        verify_base_runtime()
        check_active()
    return execute_authored_render(artifacts, inputs, operators, scalar_overrides,
        render_base, render_proposal, verify, measure, check_active=check_active,
        **preparation)


def execute_profiled_authored_render(artifacts, inputs, operators, scalar_overrides,
                                     render_base, render_proposal, verify_base_runtime,
                                     measure, *, backend, runtime_lock_path, runtime_lock_sha,
                                     check_active=lambda: None, **preparation):
    """Require admitted profile/runtime pins before pinned authored execution."""
    from .render_resources import validate_render_execution_profile
    check_active()
    validate_render_execution_profile(preparation.get('execution_profile'), inputs.recipe,
        backend=backend, runtime_sha=runtime_lock_sha,
        tile_size=preparation.get('tile_size', 512),
        alpha_mode=preparation.get('alpha_mode', 'require_float32'))
    check_active()
    return execute_pinned_authored_render(artifacts, inputs, operators, scalar_overrides,
        render_base, render_proposal, verify_base_runtime, measure,
        runtime_lock_path=runtime_lock_path, runtime_lock_sha=runtime_lock_sha,
        check_active=check_active, **preparation)
