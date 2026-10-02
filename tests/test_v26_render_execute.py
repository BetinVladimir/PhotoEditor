from contextlib import contextmanager
from hashlib import sha256
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.operations.optical.contracts import ImageReport, ImageResult
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_execute import execute_authored_render, execute_pinned_authored_render, execute_profiled_authored_render, MeasuredAuthoredRender
from test_v26_render_authored import fixture


@pytest.mark.parametrize('entry',[execute_pinned_authored_render,execute_profiled_authored_render,'task-renderer'])
@pytest.mark.parametrize('status',['pass','review','fail'])
@pytest.mark.parametrize('base_review',[False,True])
def test_authored_execution_stages_exact_result_inventory_only_after_measurement(tmp_path,status,base_review,entry):
    artifacts,inputs,overrides,events,arguments,path=fixture(tmp_path)
    from test_v26_render_runtime import lock
    from photo_worker.contracts.base import canonical_hash
    runtime=lock();runtime_path=tmp_path/'render-lock.json';runtime_path.write_bytes(canonical_bytes(runtime))
    arguments.update(runtime_lock_path=runtime_path,runtime_lock_sha=canonical_hash(runtime))
    if entry is execute_profiled_authored_render or entry == 'task-renderer':
        arguments['backend']='cuda'
        arguments['execution_profile'].update(backend='cuda',renderer_version='v2.2',
            render_runtime_sha=canonical_hash(runtime),model_lock_sha=inputs.recipe.scalars.model_lock_sha,
            icc_lock_sha=inputs.recipe.scalars.icc_lock_sha,tile_size=arguments.get('tile_size',512),
            alpha_mode=arguments.get('alpha_mode','require_float32'))
    documents={};staged={}
    def stage(path,owner,role,mime):
        owner.validate_file(path)
        raw=path.read_bytes();ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
        staged[ref.artifact_id]=raw;return ref
    def stage_contract(value,role):
        raw=canonical_bytes(value);ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
        staged[ref.artifact_id]=raw;documents[ref.artifact_id]=value;return ref
    artifacts.stage=stage;artifacts.stage_contract=stage_contract
    @contextmanager
    def base(state,style,original,masks):
        assert style.merged.strength==.4
        pixels=AdjustmentPixels(np.full_like(original.rgb,.5),None)
        if base_review:
            from photo_worker.v2.render_portrait import RenderPortraitBase
            from photo_worker.v2.render_effects import RenderEffectChain
            from photo_worker.v2.render_beauty import RenderBeautyStage
            yield RenderEffectChain(pixels,(RenderPortraitBase(pixels,(),True),RenderBeautyStage(pixels,(),False)))
        else:yield pixels
    def measure(state,prepared):
        assert prepared.style.effective_delta.ev==.1 and prepared.render.composition.changes.changed_pixels==2
        assert not staged
        assert (prepared.render.effect_evidence is not None)==base_review
        # Controlled measured evidence for orchestration; no model qualification.
        report=ImageReport(schema_version=1,status=status,operation='render.compose',skipped_count=0,
            reason=None,outside_mask_max_error=0,clipping_fraction=0,identity=None,transform=None,
            sources=[state.recipe.recipe.source],registrations=[],parent_reports=[])
        return MeasuredAuthoredRender(report,())
    if entry == 'task-renderer':
        from types import SimpleNamespace
        from photo_worker.v2.render_task import ProfiledTaskRenderer
        options={key:value for key,value in arguments.items() if key not in {
            'backend','runtime_lock_path','runtime_lock_sha','execution_profile','provider_peak'}}
        renderer=ProfiledTaskRenderer(backend='cuda',runtime_path=runtime_path,
            runtime_sha256=canonical_hash(runtime),model_lock_sha=inputs.recipe.scalars.model_lock_sha,
            icc_lock_sha=inputs.recipe.scalars.icc_lock_sha,provider_peak=arguments['provider_peak'],
            verify_qualification=lambda:None,load_providers=lambda *args:dict(
                operators=(),scalar_overrides=overrides,render_base=base,render_proposal=None,
                measure=measure,preparation=options))
        ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile=arguments['execution_profile']))
        entry=lambda *args,**kwargs:renderer.execute(ctx,artifacts,inputs)
    try:
        if status=='fail':
            with pytest.raises(ValueError,match='MEASURED_INVALID'):
                entry(artifacts,inputs,(),overrides,base,None,lambda:None,measure,**arguments)
            assert not staged
        else:
            result=entry(artifacts,inputs,(),overrides,base,None,lambda:None,measure,**arguments)
            root=documents[result.document.artifact_id]
            assert isinstance(root,ImageResult)
            published=documents[root.report.artifact_id]
            assert published.status==('review' if base_review else status)
            if base_review:
                assert published.reason=='RENDER_EFFECT_REVIEW' and result.review_required
            assert result.bytes_written==sum(map(len,staged.values()))
            assert {ref.artifact_id for ref in result.outputs}==set(staged)
        assert events==['pixels-open','pixels-close']
        assert set(tmp_path.rglob('*.npy'))=={path}
    finally:artifacts.scratch.cleanup()


def test_measurement_provider_required_before_preparation():
    with pytest.raises(ValueError,match='MEASUREMENT_PROVIDER_INVALID'):
        execute_authored_render(None,None,(),(),None,None,None,None)


def test_wrong_composition_pin_rejects_before_input_documents_or_pixels(tmp_path):
    from test_v26_render_runtime import lock
    from photo_worker.runtime.executor import OperationFailure
    artifacts,inputs,overrides,events,arguments,_=fixture(tmp_path)
    path=tmp_path/'render-lock.json';path.write_bytes(canonical_bytes(lock()))
    providers=[]
    try:
        with pytest.raises(OperationFailure,match='RUNTIME_LOCK_MISMATCH'):
            execute_pinned_authored_render(artifacts,inputs,(),overrides,lambda *args:None,None,
                lambda:providers.append('called'),lambda *args:None,
                runtime_lock_path=path,runtime_lock_sha='0'*64,**arguments)
        assert artifacts.reads==[] and events==[] and providers==[]
    finally:artifacts.scratch.cleanup()


def test_actual_color_provider_runs_through_pinned_authored_publication(tmp_path):
    from io import BytesIO
    from test_v26_render_runtime import lock
    from photo_worker.contracts.base import canonical_hash
    from photo_worker.v2.render_color import prepare_color_base
    from photo_worker.algorithms.color import apply_global,apply_strength
    artifacts,inputs,overrides,events,arguments,path=fixture(tmp_path)
    np.save(path,np.full((1,2,3),.25,np.float32))
    from photo_worker.v2.render_style import prepare_render_style
    from photo_worker.v2.render_color import color_provider_peak
    from photo_worker.v2.render_resources import render_resources
    prepared_style=prepare_render_style(artifacts,inputs,overrides,max_override_document_bytes=4096)
    peak=color_provider_peak(2,1,prepared_style.effective)
    arguments['provider_peak']=peak
    arguments['execution_profile']={'resource_estimate':render_resources(2,1,mask_refs=0,mask_nodes=0,provider_peak=peak)}
    runtime=lock();runtime_path=tmp_path/'render-lock.json';runtime_path.write_bytes(canonical_bytes(runtime))
    staged={};documents={};expected=[]
    def stage(path,owner,role,mime):
        owner.validate_file(path);raw=path.read_bytes()
        ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest());staged[ref.artifact_id]=raw;return ref
    def stage_contract(value,role):
        raw=canonical_bytes(value);ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
        staged[ref.artifact_id]=raw;documents[ref.artifact_id]=value;return ref
    artifacts.stage=stage;artifacts.stage_contract=stage_contract
    def base(state,style,original,masks):
        expected.append(apply_strength(original.rgb,apply_global(original.rgb,style.effective.global_),style.effective.strength))
        return prepare_color_base(artifacts,state,style,original,person_reliable=False,read_protected=masks.protection if state.protection is not None else None)
    def measure(state,prepared):
        np.testing.assert_array_equal(prepared.render.composition.pixels.rgb,expected[0])
        assert not staged
        return MeasuredAuthoredRender(ImageReport(schema_version=1,status='review',operation='render.compose',
            skipped_count=0,reason=None,outside_mask_max_error=0,clipping_fraction=0,identity=None,transform=None,
            sources=[state.recipe.recipe.source],registrations=[],parent_reports=[]),())
    try:
        result=execute_pinned_authored_render(artifacts,inputs,(),overrides,base,None,lambda:None,measure,
            runtime_lock_path=runtime_path,runtime_lock_sha=canonical_hash(runtime),**arguments)
        root=documents[result.document.artifact_id];image=documents[root.working_image.artifact_id]
        np.testing.assert_array_equal(np.load(BytesIO(staged[image.pixels.artifact_id]),allow_pickle=False),expected[0])
        changed=documents[root.changed_mask.artifact_id]
        np.testing.assert_array_equal(np.load(BytesIO(staged[changed.artifact.artifact_id]),allow_pickle=False),np.ones((1,2),np.float32))
        assert result.bytes_written==sum(map(len,staged.values())) and result.review_required
        assert set(tmp_path.rglob('*.npy'))=={path} and events==['pixels-open','pixels-close']
    finally:artifacts.scratch.cleanup()
