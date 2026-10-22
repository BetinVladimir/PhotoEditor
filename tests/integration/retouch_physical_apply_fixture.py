"""Reproducible input for the real Go retouch resolver/Apply integration driver.

Physical CPU output and SQL completion are real; imported qualification reports
and detector coordinates are fixture data, not photographic certification.
The Go provider must discover/verify outputs and seal its own receipt.
"""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.color.icc import ICCRegistry
from retouch_physical_worker_fixture import completed_physical_preview_fixture
from semantic_qualification_fixture import frozen_qualification


def physical_apply_document(db, objects, root, *, with_alpha=False):
    from pathlib import Path
    prepared, _, context, result = completed_physical_preview_fixture(
        db, objects, root, with_alpha=with_alpha)
    args, scope = prepared['args'], prepared['scope']
    params = scope['retouch_params'].model_dump(mode='json')
    pin_options = dict(objects=objects, backend='cpu',
        model_lock_sha=params['model_lock_sha'], runtime_lock_sha=params['runtime_lock_sha'],
        device_profile_sha=params['device_profile_sha'],
        calibration_ref=scope['qualification']['calibration'])
    execution_pin = frozen_qualification(db, args[0], scope['project'], args[2],
        operation='semantic.local_exposure', **pin_options)
    copy_pin = frozen_qualification(db, args[0], scope['project'], args[2],
        operation='semantic.adopt_shown_retouch_region', **pin_options)

    def budget(pin):
        # Structural fixture budgets, not deployment measurements. The actual
        # task cost is computed by the Go compiler inside these generous caps.
        return dict(qualification=pin,
            owner_costs=dict(runtime_ram_bytes=1, input_index_scratch_bytes=16*1024**2),
            limits=dict(ram_bytes=8*1024**3, vram_bytes=0, scratch_bytes=8*1024**3,
                        output_bytes=8*1024**3, pixel_count=1000000),
            **{key:pin[key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')})

    upstream = scope['qualification']
    catalog = dict(schema_version=1, tenants=[dict(tenant_id=str(args[0]),
        review=dict(schema_version=1, review_model_sha=upstream['model_lock_sha'],
                    review_dataset_sha=upstream['dataset_sha'], document_budget_bytes=64*1024**2),
        execution=budget(execution_pin), shown_retouch_execution=budget(copy_pin))])
    icc = ICCRegistry(Path(__file__).resolve().parents[2] / 'resources' / 'icc')
    # PreviewRequest has exported Go field names, deliberately no snake-case tags.
    fence = dict(ExpectedRevision=params['workflow_revision'], TargetRevision=params['target_revision'],
                 PlanSHA=params['plan']['sha256'], CandidateSHA=params['candidate']['sha256'])
    document = dict(tenant_id=str(args[0]), intent_id=str(args[1]), actor_id=str(args[2]),
        session_id=str(args[3]), target_id=str(prepared['target']),
        srgb_sha=icc.resolve('srgb_v1').sha256, catalog=catalog, preview_fence=fence)
    # Separate processes must see imported policies as well as worker outputs.
    db.commit()
    return canonical_bytes(document), prepared, context, result
