"""Canonical numeric StyleV2 package for immutable acceptance/export."""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.style import StyleV2
from photo_worker.contracts.style_validation import PackageParams,StylePackage,StyleQualityReport
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<268435456:raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(PackageParams);style=artifacts.contract(params.style,StyleV2);report=artifacts.contract(params.quality_report,StyleQualityReport)
        if report.style!=params.style or report.config_hash!=style.config_hash or report.resulting_quality!=style.quality:raise ValueError('Style validation binding mismatch')
        packaged=style
        if report.accepted_by_metrics:
            if style.quality.status!='measured' or style.quality.validation is None or style.quality.transfer_confidence!='experimental' or style.quality.warnings:raise ValueError('Accepted validation is not promotable')
            packaged=style.model_copy(update={'quality':style.quality.model_copy(update={'transfer_confidence':'validated'})})
        data=canonical_bytes(packaged)
        if len(data)>104857600:raise ValueError('Style package too large')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(0,3)
    with artifacts.scratch.reserve(len(data)+4096) as reservation:
        path=reservation.path('style-package','.json');path.write_bytes(data);reservation.validate()
        style_ref=artifacts.stage(path,reservation,'style_package','application/json')
    ctx.progress(1,3)
    manifest=StylePackage(style=style_ref,quality_report=params.quality_report,config_hash=packaged.config_hash,bytes=len(data));manifest_ref=artifacts.stage_contract(manifest,'package_manifest');ctx.progress(3,3)
    return OperationResult(outputs=[style_ref,manifest_ref],review_required=not report.accepted_by_metrics,bytes_written=len(data)+len(canonical_bytes(manifest)))


def package_operations(config:ArtifactConfig)->list[Operation]:
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'style_package','package_manifest'})) as artifacts:return execute(ctx,artifacts)
    return [Operation('style.package','cpu',run)]
