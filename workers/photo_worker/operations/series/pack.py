"""Bounded immutable source statistics, without undeclared nested IO."""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.series import SeriesPackParams,SeriesStats,SeriesStatsPack,PackedSeriesStats
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation,OperationFailure,OperationResult


def execute(ctx,artifacts):
    if ctx.lease.execution_profile.get('backend')!='cpu':raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(SeriesPackParams);items=[]
        for ref in p.stats:
            ctx.check_active();items.append(PackedSeriesStats(source=ref,value=artifacts.contract(ref,SeriesStats)))
        result=SeriesStatsPack(stats=items)
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.check_active();ref=artifacts.stage_contract(result,'series_stats_pack')
    return OperationResult(outputs=[ref],bytes_written=len(canonical_bytes(result)))


def pack_operations(config):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'series_stats_pack'})) as artifacts:return execute(ctx,artifacts)
    return [Operation('series.pack','cpu',run)]
