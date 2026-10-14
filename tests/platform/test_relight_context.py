from types import SimpleNamespace
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.portrait.relight_context import verified_relight_context
from test_portrait_manual_admission import context,ref


@pytest.mark.parametrize('mode',['ok','missing','changed','fence','legacy'])
def test_verified_closure_expands_local_view_without_mutating_transport(mode):
    ctx=context();manifest=ref();dependency=ref()
    ctx.lease.command.inputs=[manifest]
    ctx.lease.execution_profile['renderer_version']='legacy' if mode=='legacy' else 'v2.2'
    refs=[manifest,dependency]+([] if mode=='missing' else [ctx.lease.command.params])
    artifacts=SimpleNamespace(declared_input_refs=lambda maximum:tuple(refs))
    if mode in ('legacy','missing'):
        with pytest.raises(RuntimeError):verified_relight_context(ctx,artifacts)
        return
    view=verified_relight_context(ctx,artifacts)
    assert view.lease is not ctx.lease and view.lease.command is not ctx.lease.command
    assert view.lease.command.inputs==refs and ctx.lease.command.inputs==[manifest]
    assert view.lease.command.params==ctx.lease.command.params
    if mode=='changed':ctx.lease.command.inputs.append(ref())
    if mode=='fence':ctx.lease.fence+=1
    if mode in ('changed','fence'):
        with pytest.raises(RuntimeError,match='RELIGHT_TRANSPORT_CHANGED'):view.check_active()
    else:view.check_active()


def test_real_task_artifacts_disk_index_exposes_dependencies_absent_from_transport(tmp_path):
    from test_v2_input_closure import fixture
    from photo_worker.v2.input_closure import V2InputIndex
    from photo_worker.runtime.artifacts import TaskArtifacts
    task,entries,manifest,params,scratch,profile,open_stream=fixture(tmp_path)
    ctx=context();ctx.lease.command.task_id=task;ctx.lease.command.params=params
    ctx.lease.command.inputs=[manifest];ctx.lease.execution_profile=profile
    try:
        index=V2InputIndex.build(task_id=task,inputs=[manifest],params=params,execution_profile=profile,
            scratch=scratch,open_stream=open_stream,check_active=ctx.check_active)
        artifacts=TaskArtifacts(ctx,None,scratch,index)
        view=verified_relight_context(ctx,artifacts)
        assert all(entry.ref in view.lease.command.inputs for entry in entries)
        assert all(entry.ref not in ctx.lease.command.inputs for entry in entries)
        for entry in entries:artifacts.require_declared_input(entry.ref)
        assert ctx.lease.command.inputs==[manifest]
        index.close()
    finally:scratch.cleanup()
