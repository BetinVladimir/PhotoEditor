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


@pytest.mark.parametrize('retain_parent',[True,False])
def test_profiled_worker_inherits_primary_generated_mask_and_review(tmp_path,retain_parent):
    from io import BytesIO
    from types import SimpleNamespace
    from photo_worker.contracts.base import canonical_hash
    from photo_worker.contracts.images import MaskRef
    from photo_worker.v2.adoption import ArtifactRef as ScopedRef
    from photo_worker.v2.render_adoption_inputs import RenderAdoptionInputs
    from photo_worker.v2.render_qualification_pin import RenderQualificationPin
    from photo_worker.v2.render_task import ProfiledTaskRenderer
    from test_v26_render_runtime import lock
    artifacts,inputs,overrides,_,options,original_path=fixture(tmp_path)
    runtime=lock();runtime_sha=canonical_hash(runtime)
    runtime_path=tmp_path/'runtime.json';runtime_path.write_bytes(canonical_bytes(runtime))
    pin=RenderQualificationPin(inputs.recipe.scalars.model_lock_sha,runtime_sha,'cpu','c'*64,'d'*64,'e'*64)
    profile=dict(options['execution_profile'],backend='cpu',renderer_version='v2.2',render_runtime_sha=runtime_sha,
        model_lock_sha=pin.model_lock_sha,icc_lock_sha=inputs.recipe.scalars.icc_lock_sha,tile_size=512,alpha_mode='require_float32')
    documents={};payloads={}
    def document(value):
        raw=canonical_bytes(value);ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
        documents[ref.artifact_id]=value;return ref
    generated_path=tmp_path/'generated.npy';np.save(generated_path,np.array([[0,1]],np.float32))
    generated_pixels=ArtifactRef(artifact_id=uuid4(),sha256=sha256(generated_path.read_bytes()).hexdigest())
    generated_ref=document(MaskRef(artifact=generated_pixels,canvas=inputs.source.canvas,
        transform_chain=inputs.source.transform_chain,selector='generated',confidence=1,model_lock_id=None,protected=False))
    binding=inputs.recipe.recipe.source
    report_ref=document(ImageReport(schema_version=1,status='review',operation='asset.decode',skipped_count=0,
        reason='SOURCE_REVIEW',outside_mask_max_error=0,clipping_fraction=0,identity=None,transform=None,
        sources=[binding],registrations=[],parent_reports=[]))
    parent=ImageResult(schema_version=1,working_image=binding.source,report=report_ref,
        changed_mask=generated_ref,generated_mask=generated_ref)
    parent_ref=document(parent)
    recipe_ref=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(inputs.recipe))
    scoped=RenderAdoptionInputs(ScopedRef(recipe_ref.artifact_id,recipe_ref.sha256),canonical_bytes(profile),
        (ScopedRef(parent_ref.artifact_id,parent_ref.sha256),),pin)
    def contract(ref,model):return model.model_validate_json(canonical_bytes(documents[ref.artifact_id]))
    artifacts.contract=contract
    @contextmanager
    def input_file(ref,**kwargs):
        assert ref in (inputs.source.pixels,generated_pixels)
        yield SimpleNamespace(path=original_path if ref==inputs.source.pixels else generated_path)
    artifacts.input=input_file
    def stage(path,owner,role,mime):
        owner.validate_file(path);raw=path.read_bytes()
        ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest());payloads[ref.artifact_id]=raw;return ref
    artifacts.stage=stage;artifacts.stage_contract=lambda value,role:document(value)
    @contextmanager
    def base(state,style,original,masks):
        np.testing.assert_array_equal(original.generated,[[0,1]])
        yield AdjustmentPixels(np.full_like(original.rgb,.5),original.alpha,original.generated)
    def measure(state,prepared):
        assert prepared.render.source_reports.review_required
        assert prepared.render.source_reports.reports==(report_ref,)
        np.testing.assert_array_equal(prepared.render.composition.pixels.generated,[[0,1]])
        measured=ImageReport(schema_version=1,status='pass',operation='render.compose',skipped_count=0,
            reason=None,outside_mask_max_error=0,clipping_fraction=0,identity=None,transform=None,
            sources=[binding],registrations=[],parent_reports=[report_ref] if retain_parent else [])
        return MeasuredAuthoredRender(measured,(parent,) if retain_parent else ())
    preparation={k:v for k,v in options.items() if k not in ('execution_profile','provider_peak')}
    renderer=ProfiledTaskRenderer(backend='cpu',runtime_path=runtime_path,runtime_sha256=runtime_sha,
        model_lock_sha=pin.model_lock_sha,icc_lock_sha=inputs.recipe.scalars.icc_lock_sha,
        provider_peak=options['provider_peak'],qualification_pin=pin,verify_qualification=lambda supplied:None,
        load_providers=lambda *args:dict(operators=(),scalar_overrides=overrides,render_base=base,
            render_proposal=None,measure=measure,preparation=preparation))
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile=profile,
        command=SimpleNamespace(params=recipe_ref)))
    try:
        if not retain_parent:
            with pytest.raises(ValueError,match='SOURCE_REPORTS_DROPPED'):
                renderer.execute(ctx,artifacts,inputs,authoritative_inputs=scoped)
            assert not payloads
        else:
            result=renderer.execute(ctx,artifacts,inputs,authoritative_inputs=scoped)
            root=documents[result.document.artifact_id]
            report=documents[root.report.artifact_id]
            assert report.status=='review' and report.parent_reports==[report_ref] and result.review_required
            generated=documents[root.generated_mask.artifact_id]
            np.testing.assert_array_equal(np.load(BytesIO(payloads[generated.artifact.artifact_id]),allow_pickle=False),[[0,1]])
    finally:artifacts.scratch.cleanup()


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
        from photo_worker.v2.render_qualification_pin import RenderQualificationPin
        from photo_worker.v2.render_adoption_inputs import RenderAdoptionInputs
        from photo_worker.v2.adoption import ArtifactRef as ScopedRef
        options={key:value for key,value in arguments.items() if key not in {
            'backend','runtime_lock_path','runtime_lock_sha','execution_profile','provider_peak'}}
        pin=RenderQualificationPin(inputs.recipe.scalars.model_lock_sha,canonical_hash(runtime),
            'cuda','c'*64,'d'*64,'e'*64)
        renderer=ProfiledTaskRenderer(backend='cuda',runtime_path=runtime_path,
            runtime_sha256=canonical_hash(runtime),model_lock_sha=inputs.recipe.scalars.model_lock_sha,
            icc_lock_sha=inputs.recipe.scalars.icc_lock_sha,provider_peak=arguments['provider_peak'],
            qualification_pin=pin,verify_qualification=lambda supplied:None,load_providers=lambda *args:dict(
                operators=(),scalar_overrides=overrides,render_base=base,render_proposal=None,
                measure=measure,preparation=options))
        ref=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(inputs.recipe))
        scoped=RenderAdoptionInputs(ScopedRef(ref.artifact_id,ref.sha256),canonical_bytes(arguments['execution_profile']),(),pin)
        ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile=arguments['execution_profile'],
            command=SimpleNamespace(params=ref)))
        entry=lambda *args,**kwargs:renderer.execute(ctx,artifacts,inputs,authoritative_inputs=scoped)
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
