from contextlib import contextmanager
from types import SimpleNamespace as NS
import pytest
from photo_worker.contracts.base import canonical_hash
from photo_worker.operations.optical import adapter
from photo_worker.runtime.executor import OperationFailure,OperationResult


def test_unconfigured_or_empty_registry_installs_no_lens():
    assert adapter.lens_operations(None,None)==[]
    assert adapter.lens_operations(None,NS(profiles=[]))==[]


@pytest.mark.parametrize('fail',[False,True])
def test_lens_operation_builds_admission_and_closes_artifacts(monkeypatch,fail):
    from photo_worker.runtime.artifacts import TaskArtifacts
    from photo_worker.operations.optical.metadata_source import ArtifactLensMetadataResolver
    from photo_worker.operations.optical import admission
    events=[];artifacts=object();resolver=object();provider=object();config=object()
    runtime={'schema_version':1};pins=('pin',)
    registry=NS(profiles=['pin'],runtime=runtime,admission_pins=lambda:pins)
    ctx=NS(check_active=lambda:None)
    @contextmanager
    def create(actual_ctx,actual_config,roles):
        assert actual_ctx is ctx and actual_config is config
        assert roles==frozenset({'document','working_image','pixels','alpha','warp_field','mask_pixels','mask','report'})
        events.append('open')
        try:yield artifacts
        finally:events.append('close')
    def from_inputs(actual,active):
        assert actual is artifacts and active==ctx.check_active
        events.append('binding');return resolver
    def build(actual,actual_pins,actual_resolver,digest,*,runtime_lock):
        assert actual is artifacts and actual_pins==pins and actual_resolver is resolver
        assert digest==canonical_hash(runtime) and runtime_lock is runtime
        events.append('admission');return provider
    def execute(actual_ctx,actual,actual_admission):
        assert actual_ctx is ctx and actual is artifacts and actual_admission is provider
        events.append('execute')
        if fail:raise OperationFailure('LENS_PROFILE_UNAVAILABLE')
        return OperationResult(outputs=[],bytes_written=0)
    monkeypatch.setattr(TaskArtifacts,'create',create)
    monkeypatch.setattr(ArtifactLensMetadataResolver,'from_declared_inputs',from_inputs)
    monkeypatch.setattr(admission,'PinnedLensAdmission',build)
    monkeypatch.setattr(adapter,'execute',execute)
    operation,=adapter.lens_operations(config,registry)
    assert (operation.name,operation.resource_class,operation.version)==('optical.lens','cpu','1')
    if fail:
        with pytest.raises(OperationFailure):operation.execute(ctx)
    else:assert operation.execute(ctx).outputs==[]
    assert events==['open','binding','admission','execute','close']
