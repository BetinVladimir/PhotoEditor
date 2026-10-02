from contextlib import contextmanager
from types import SimpleNamespace
import numpy as np
import pytest
from test_layer_mask_inputs import fixture
from test_layer_prepare import scratch
from test_v26_render_masks import inputs
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_prepare import prepare_render_pixels


def test_actual_source_masks_composition_and_change_evidence_share_owned_scope(tmp_path):
    image,_,ref,_,mask_artifacts,_=fixture(tmp_path)
    state=inputs(image,ref)
    for layer in state.layers.layers:
        layer.operation='test.adjust';layer.blend='normal'
    loaded=tuple(SimpleNamespace(layer=layer) for layer in state.layers.layers)
    rgb_path=tmp_path/'source.npy';np.save(rgb_path,np.zeros((1,2,3),np.float32))
    space=scratch(tmp_path);events=[]
    class Artifacts:
        scratch=space
        contract=mask_artifacts.contract
        @contextmanager
        def input(self,reference,**kw):
            if reference==image.pixels:
                events.append('source-open')
                try:yield SimpleNamespace(path=rgb_path)
                finally:events.append('source-close')
            else:
                with mask_artifacts.input(reference,**kw) as local:yield local
    @contextmanager
    def base(inputs,original):
        events.append('base-open')
        try:yield original
        finally:events.append('base-close')
    @contextmanager
    def proposal(item,current):
        yield AdjustmentPixels(np.ones_like(current.rgb),None)
    try:
        with prepare_render_pixels(Artifacts(),state,loaded,base,proposal,max_input_bytes=100000,max_mask_bytes=100000,max_tile_bytes=4096) as result:
            np.testing.assert_allclose(result.composition.pixels.rgb[0,:,0],[.36,.96],atol=1e-7)
            assert result.composition.changes.changed_pixels==2
            assert events==['source-open','base-open']
            assert mask_artifacts.opens==1 and mask_artifacts.closes==0
        assert events==['source-open','base-open','base-close','source-close']
        assert mask_artifacts.closes==1
        events.clear()
        with pytest.raises(ValueError,match='INVENTORY_CHANGED'):
            with prepare_render_pixels(Artifacts(),state,loaded[:1],base,proposal,max_input_bytes=100000,max_mask_bytes=100000,max_tile_bytes=4096):pass
        assert events==[]
    finally:space.cleanup()


@pytest.mark.parametrize('failure',['base','consumer','geometry'])
def test_preparation_failure_closes_every_materialized_input(tmp_path,failure):
    image,_,ref,_,mask_artifacts,_=fixture(tmp_path)
    state=inputs(image,ref)
    for layer in state.layers.layers:
        layer.operation='test.adjust';layer.blend='normal'
    if failure=='geometry':state.layers.layers[0].operation='optical.lens'
    loaded=tuple(SimpleNamespace(layer=layer) for layer in state.layers.layers)
    rgb_path=tmp_path/'source.npy';np.save(rgb_path,np.zeros((1,2,3),np.float32))
    space=scratch(tmp_path);events=[]
    class Artifacts:
        scratch=space
        contract=mask_artifacts.contract
        @contextmanager
        def input(self,reference,**kw):
            if reference==image.pixels:
                events.append('open')
                try:yield SimpleNamespace(path=rgb_path)
                finally:events.append('close')
            else:
                with mask_artifacts.input(reference,**kw) as local:yield local
    @contextmanager
    def base(inputs,original):
        if failure=='base':raise RuntimeError('base failed')
        yield original
    @contextmanager
    def proposal(item,current):yield AdjustmentPixels(np.ones_like(current.rgb),None)
    try:
        with pytest.raises(ValueError if failure=='geometry' else RuntimeError):
            with prepare_render_pixels(Artifacts(),state,loaded,base,proposal,max_input_bytes=100000,max_mask_bytes=100000,max_tile_bytes=4096):
                raise RuntimeError('consumer failed')
        assert events==([] if failure=='geometry' else ['open','close'])
        assert mask_artifacts.opens==mask_artifacts.closes==(0 if failure=='geometry' else 1)
        assert set(tmp_path.rglob('*.npy'))=={tmp_path/'mask.npy',rgb_path}
    finally:space.cleanup()


@pytest.mark.parametrize('status',['review','fail','unavailable'])
def test_source_quality_report_is_checked_before_pixels_and_review_is_retained(tmp_path,status):
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef,canonical_hash
    from photo_worker.operations.session.contracts import VersionBinding
    from photo_worker.operations.optical.contracts import ImageResult,ImageReport
    image,*_=fixture(tmp_path)
    ref=lambda:ArtifactRef(artifact_id=uuid4(),sha256='f'*64)
    binding=VersionBinding(asset_version_id=uuid4(),source=ref(),canvas_hash=canonical_hash(image.canvas))
    parent=ImageResult(schema_version=1,working_image=binding.source,report=ref(),changed_mask=ref(),generated_mask=None)
    report=ImageReport(schema_version=1,status=status,operation='test.adjust',skipped_count=0,reason=None,outside_mask_max_error=0,clipping_fraction=0,identity=None,transform=None,sources=[binding],registrations=[])
    state=SimpleNamespace(source=image,layers=None,protection=None,recipe=SimpleNamespace(recipe=SimpleNamespace(source=binding,protected_mask=None)))
    rgb_path=tmp_path/'source.npy';np.save(rgb_path,np.zeros((1,2,3),np.float32))
    space=scratch(tmp_path);reads=[]
    class Artifacts:
        scratch=space
        def contract(self,reference,model):
            assert reference==parent.report and model is ImageReport
            reads.append('report');return report
        @contextmanager
        def input(self,reference,**kw):
            reads.append('pixels')
            yield SimpleNamespace(path=rgb_path)
    @contextmanager
    def base(inputs,original):yield original
    try:
        if status=='review':
            with prepare_render_pixels(Artifacts(),state,(),base,None,source_result=parent,max_input_bytes=100000,max_mask_bytes=100000,max_tile_bytes=4096) as result:
                assert result.source_reports.review_required and result.source_reports.reports==(parent.report,)
            assert reads==['report','pixels']
        else:
            with pytest.raises(ValueError,match='PARENT_REPORT_REJECTED'):
                with prepare_render_pixels(Artifacts(),state,(),base,None,source_result=parent,max_input_bytes=100000,max_mask_bytes=100000,max_tile_bytes=4096):pass
            assert reads==['report']
    finally:space.cleanup()


def test_prepared_pixels_materialize_and_stage_complete_image_mask_inventory(tmp_path):
    from hashlib import sha256
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
    from photo_worker.operations.session.contracts import VersionBinding
    from photo_worker.v2.render_files import prepare_render_output_files
    from photo_worker.v2.render_stage import stage_render_image
    from photo_worker.contracts.images import WorkingImage,MaskRef
    from photo_worker.operations.optical.contracts import ImageReport,ImageResult
    from photo_worker.v2.render_reports import RenderReportEvidence
    from photo_worker.v2.render_result import stage_render_result
    from photo_worker.v2.render_publish import publish_prepared_render
    image,*_=fixture(tmp_path)
    binding=VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256='f'*64),canvas_hash=canonical_hash(image.canvas))
    state=SimpleNamespace(source=image,layers=None,protection=None,recipe=SimpleNamespace(recipe=SimpleNamespace(source=binding,protected_mask=None)))
    rgb_path=tmp_path/'source.npy';np.save(rgb_path,np.zeros((1,2,3),np.float32))
    space=scratch(tmp_path);staged={};documents={}
    class Artifacts:
        scratch=space
        @contextmanager
        def input(self,ref,**kw):
            assert ref==image.pixels
            yield SimpleNamespace(path=rgb_path)
        def stage(self,path,owner,role,mime):
            owner.validate_file(path);body=path.read_bytes()
            ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest());staged[ref.artifact_id]=body;return ref
        def stage_contract(self,value,role):
            body=canonical_bytes(value)
            ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest());staged[ref.artifact_id]=body;documents[ref.artifact_id]=value;return ref
    artifacts=Artifacts()
    @contextmanager
    def base(inputs,original):
        yield AdjustmentPixels(np.full_like(original.rgb,2),None,np.ones((1,2),np.float32))
    try:
        with prepare_render_pixels(artifacts,state,(),base,None,max_input_bytes=100000,max_mask_bytes=100000,max_tile_bytes=4096) as prepared:
            with prepare_render_output_files(prepared,space) as files:
                result=stage_render_image(artifacts,image,binding,files)
                assert isinstance(documents[result.working_image.artifact_id],WorkingImage)
                assert isinstance(documents[result.changed_mask.artifact_id],MaskRef)
                assert documents[result.generated_mask.artifact_id].selector=='generated'
                assert result.bytes_written==sum(map(len,staged.values()))
                assert {ref.artifact_id for ref in result.outputs}==set(staged)
                # Controlled provider metrics for this fixture, not ML qualification.
                report=ImageReport(schema_version=1,status='pass',operation='render.compose',skipped_count=0,
                    reason=None,outside_mask_max_error=0,clipping_fraction=0,identity=None,transform=None,
                    sources=[binding],registrations=[],parent_reports=[])
                root=stage_render_result(artifacts,result,report,binding,RenderReportEvidence((),False))
                document=documents[root.document.artifact_id]
                assert isinstance(document,ImageResult) and document.working_image==result.working_image
                assert documents[document.report.artifact_id].sources==[binding]
                assert root.bytes_written==sum(map(len,staged.values()))
                assert {ref.artifact_id for ref in root.outputs}==set(staged)
            staged.clear();documents.clear()
            published=publish_prepared_render(artifacts,state,prepared,report,())
            assert isinstance(documents[published.document.artifact_id],ImageResult)
            assert published.bytes_written==sum(map(len,staged.values()))
            assert {ref.artifact_id for ref in published.outputs}==set(staged)


        assert set(tmp_path.rglob('*.npy'))=={tmp_path/'mask.npy',rgb_path}
    finally:space.cleanup()


def test_resource_profile_rejects_before_any_materialization(tmp_path):
    from photo_worker.runtime.executor import OperationFailure
    image,_,ref,_,_,_=fixture(tmp_path);state=inputs(image,ref)
    for layer in state.layers.layers:layer.operation='test.adjust'
    loaded=tuple(SimpleNamespace(layer=layer) for layer in state.layers.layers)
    with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
        with prepare_render_pixels(SimpleNamespace(),state,loaded,lambda *args:None,None,
                max_input_bytes=100000,max_mask_bytes=100000,max_tile_bytes=4096,
                execution_profile={'resource_estimate':{}},
                provider_peak=dict.fromkeys(['ram_bytes','scratch_bytes','vram_bytes','output_bytes'],0)):pass
