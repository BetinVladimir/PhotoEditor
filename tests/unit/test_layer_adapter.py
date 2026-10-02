from types import SimpleNamespace
import pytest
from photo_worker.operations.layers.adapter import execute
from photo_worker.runtime.executor import OperationFailure


@pytest.mark.parametrize('profile,admission,code',[
    ({'renderer_version':'v2.2','backend':'cpu'},None,'LAYER_ADMISSION_UNAVAILABLE'),
    ({'renderer_version':'v2.2','backend':'cuda'},None,'CONFIG_INVALID'),
    ({'renderer_version':'v2.1','backend':'cpu'},None,'CONFIG_INVALID'),
    ({'renderer_version':'v2.2','backend':'cpu','layer_alpha_mode':'automatic'},None,'CONFIG_INVALID')])
def test_unconfigured_adapter_does_not_read_artifacts(profile,admission,code):
    ctx=SimpleNamespace(lease=SimpleNamespace(execution_profile=profile))
    with pytest.raises(OperationFailure) as caught:execute(ctx,object(),admission)
    assert caught.value.error.code==code


def test_relational_admission_rejection_propagates_before_source_reads():
    events=[]
    ctx=SimpleNamespace(lease=SimpleNamespace(execution_profile={'renderer_version':'v2.2','backend':'cpu'}),check_active=lambda:events.append('active'))
    class Artifacts:
        def params(self,model):events.append('params');return object()
        def contract(self,*args):pytest.fail('source read before admission')
    class Admission:
        def admit(self,ctx,params):events.append('admit');raise OperationFailure('LAYER_SCOPE_STALE')
    with pytest.raises(OperationFailure) as caught:execute(ctx,Artifacts(),Admission())
    assert caught.value.error.code=='LAYER_SCOPE_STALE' and events==['params','active','admit']


def worker_fixture(tmp_path,status='pass',stage_failure=False,normalize=False):
    from contextlib import contextmanager
    from hashlib import sha256
    from uuid import uuid4
    import numpy as np
    from test_layer_parents import setup
    from photo_worker.contracts.base import ArtifactRef,canonical_bytes
    from photo_worker.runtime.scratch import ScratchSpace
    from photo_worker.operations.layers.resources import layer_resources
    params,documents,artifacts,coverage=setup(tmp_path)
    source=documents[params.base.source.artifact_id]
    proposal_parent=documents[params.proposal_result.artifact_id]
    documents[proposal_parent.report.artifact_id].status=status
    proposal=documents[proposal_parent.working_image.artifact_id]
    proposal.pixels=ArtifactRef(artifact_id=uuid4(),sha256='d'*64)
    rgb=tmp_path/'source.npy';proposal_rgb=tmp_path/'proposal.npy'
    np.save(rgb,np.full((1,2,3),.2,np.float32),allow_pickle=False)
    np.save(proposal_rgb,np.full((1,2,3),.8,np.float32),allow_pickle=False)
    generated=documents[documents[params.source_result.artifact_id].generated_mask.artifact_id]
    paths={source.pixels.artifact_id:rgb,proposal.pixels.artifact_id:proposal_rgb,generated.artifact.artifact_id:coverage}
    if normalize:
        alpha_ref=ArtifactRef(artifact_id=uuid4(),sha256='e'*64)
        source.alpha=alpha_ref;proposal.alpha=alpha_ref
        alpha_path=tmp_path/'legacy-alpha.npy'
        np.save(alpha_path,np.array([[0,32768]],np.uint16),allow_pickle=False)
        paths[alpha_ref.artifact_id]=alpha_path
    artifacts.scratch=ScratchSpace(tmp_path,uuid4(),uuid4(),1,65536,min_free_bytes=0)
    published={};roles=[];events=[]
    @contextmanager
    def input(reference,**kwargs):
        artifacts.opens+=1
        try:yield SimpleNamespace(path=paths[reference.artifact_id])
        finally:artifacts.closes+=1
    def put(body,role,value):
        if stage_failure:raise OperationFailure('TASK_CANCELLED')
        reference=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
        published[reference.artifact_id]=value;roles.append(role);return reference
    def stage(path,reservation,role,media):
        reservation.validate_file(path)
        return put(path.read_bytes(),role,np.load(path,allow_pickle=False))
    artifacts.input=input;artifacts.stage=stage
    artifacts.stage_contract=lambda value,role:put(canonical_bytes(value),role,value)
    artifacts.params=lambda model:params
    mode='normalize_uint16' if normalize else 'require_float32'
    profile=dict(renderer_version='v2.2',backend='cpu',layer_alpha_mode=mode,resource_estimate=layer_resources(2,1,mask_refs=1,mask_nodes=1,alpha_mode=mode))
    ctx=SimpleNamespace(lease=SimpleNamespace(execution_profile=profile),check_active=lambda:events.append('active'),progress=lambda a,b:events.append((a,b)))
    class Admission:
        def admit(self,context,frozen):assert context is ctx and frozen is params;events.append('admitted')
    return ctx,artifacts,Admission(),params,published,roles,events


@pytest.mark.parametrize('normalize',[False,True])
@pytest.mark.parametrize('status',['pass','review'])
def test_successful_worker_composes_and_preserves_review(tmp_path,status,normalize):
    import numpy as np
    ctx,artifacts,admission,params,published,roles,events=worker_fixture(tmp_path,status,normalize=normalize)
    try:
        result=execute(ctx,artifacts,admission)
        assert len(result.outputs)==1 and result.bytes_written>0
        document=published[result.outputs[0].artifact_id]
        image=published[document.working_image.artifact_id]
        np.testing.assert_array_equal(published[image.pixels.artifact_id],np.array([[[.2]*3,[.8]*3]],np.float32))
        if normalize:
            from photo_worker.contracts.images import WorkingImage
            source=artifacts.contract(params.base.source,WorkingImage)
            assert image.alpha!=source.alpha
            np.testing.assert_array_equal(published[image.alpha.artifact_id],np.array([[0,32768]],np.float32)/np.float32(65535))
            np.testing.assert_array_equal(np.load(tmp_path/'legacy-alpha.npy',allow_pickle=False),[[0,32768]])
        report=published[document.report.artifact_id]
        assert report.status==status and result.review_required==(status=='review')
        generated=published[document.generated_mask.artifact_id]
        np.testing.assert_array_equal(published[generated.artifact.artifact_id],[[0,1]])
        assert events[-1]==(3,3) and events.index('admitted')<events.index((0,3))
        assert artifacts.opens==artifacts.closes and not list(artifacts.scratch.directory.rglob('*.npy'))
    finally:artifacts.scratch.cleanup()


@pytest.mark.parametrize('status',['fail','unavailable'])
def test_rejected_report_is_input_failure_without_array_reads_or_writes(tmp_path,status):
    ctx,artifacts,admission,params,published,roles,events=worker_fixture(tmp_path,status)
    try:
        with pytest.raises(OperationFailure) as caught:execute(ctx,artifacts,admission)
        assert caught.value.error.code=='INPUT_INVALID'
        assert artifacts.opens==0 and not published and (3,3) not in events
    finally:artifacts.scratch.cleanup()


@pytest.mark.parametrize("normalize",[False,True])
def test_cancelled_stage_releases_inputs_and_prepared_outputs(tmp_path,normalize):
    ctx,artifacts,admission,params,published,roles,events=worker_fixture(tmp_path,stage_failure=True,normalize=normalize)
    try:
        with pytest.raises(OperationFailure) as caught:execute(ctx,artifacts,admission)
        assert caught.value.error.code=='TASK_CANCELLED'
        assert artifacts.opens==artifacts.closes and not published and (3,3) not in events
        assert not list(artifacts.scratch.directory.rglob('*.npy'))
    finally:artifacts.scratch.cleanup()


def test_foreign_report_version_stops_worker_before_io(tmp_path):
    from uuid import uuid4
    from photo_worker.operations.optical.contracts import ImageReport
    ctx,artifacts,admission,params,published,roles,events=worker_fixture(tmp_path)
    original=artifacts.contract
    def contract(reference,model):
        result=original(reference,model)
        if model is ImageReport:
            return result.model_copy(update={'sources':[params.base.model_copy(update={'asset_version_id':uuid4()})]})
        return result
    artifacts.contract=contract
    try:
        with pytest.raises(OperationFailure) as caught:execute(ctx,artifacts,admission)
        assert caught.value.error.code=='INPUT_INVALID' and artifacts.opens==0 and not published
    finally:artifacts.scratch.cleanup()


def test_factory_owns_artifact_lifecycle_and_uses_relational_admission(monkeypatch):
    from contextlib import contextmanager
    from photo_worker.operations.layers import adapter
    from photo_worker.runtime.artifacts import TaskArtifacts
    from photo_worker.operations.layers.admission import LensUserLayerAdmission
    events=[];config=object();database=object();ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile={'layer_runtime_sha256':'a'*64}));artifacts=object()
    from photo_worker.operations.layers import runtime_lock
    monkeypatch.setattr(runtime_lock,'verify_installed_runtime',lambda *args:'a'*64)
    @contextmanager
    def create(context,configuration,roles):
        assert context is ctx and configuration is config
        assert roles==frozenset({'document','working_image','pixels','alpha','mask_pixels','mask','report'})
        events.append('open')
        try:yield artifacts
        finally:events.append('close')
    def run(context,received,admission):
        assert context is ctx and received is artifacts
        assert isinstance(admission,LensUserLayerAdmission) and admission.database is database
        events.append('execute');raise OperationFailure('LAYER_SCOPE_STALE')
    monkeypatch.setattr(TaskArtifacts,'create',create)
    monkeypatch.setattr(adapter,'execute',run)
    operations=adapter.layer_operations(config,database,object(),'a'*64)
    assert len(operations)==1
    assert (operations[0].name,operations[0].resource_class,operations[0].version)==('layer.compose','cpu','1')
    with pytest.raises(OperationFailure):operations[0].execute(ctx)
    assert events==['open','execute','close']


def test_unpinned_composition_handler_is_not_installed():
    from photo_worker.operations.layers.adapter import layer_operations
    assert layer_operations(object(),object())==[]


def test_output_metadata_preserves_lineage_and_report(tmp_path):
    from photo_worker.operations.layers.output_validation import validate_layer_output_metadata
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.operations.optical.contracts import ImageResult
    ctx,artifacts,admission,params,published,roles,events=worker_fixture(tmp_path,'review')
    try:
        source=artifacts.contract(params.base.source,WorkingImage)
        parent=artifacts.contract(params.source_result,ImageResult)
        proposal=artifacts.contract(params.proposal_result,ImageResult)
        result=execute(ctx,artifacts,admission)
        output=published[result.outputs[0].artifact_id]
        original=artifacts.contract
        artifacts.contract=lambda ref,model:published.get(ref.artifact_id) or original(ref,model)
        validate_layer_output_metadata(artifacts,params,output,source,parent,proposal)
        with pytest.raises(ValueError,match='LAYER_GENERATED_BINDING_INVALID'):
            validate_layer_output_metadata(artifacts,params,output.model_copy(update={'generated_mask':None}),source,parent,proposal)
        report=published[output.report.artifact_id]
        published[output.report.artifact_id]=report.model_copy(update={'status':'pass','reason':None})
        with pytest.raises(ValueError,match='LAYER_OUTPUT_REPORT_INVALID'):
            validate_layer_output_metadata(artifacts,params,output,source,parent,proposal)
    finally:artifacts.scratch.cleanup()


def test_runtime_drift_stops_factory_before_artifact_io(monkeypatch):
    from photo_worker.operations.layers import adapter,runtime_lock
    from photo_worker.runtime.artifacts import TaskArtifacts
    calls=[];lock={'components':{'test':'a'*64}}
    def verify(received,pin):
        calls.append(received['components']['test'])
        if len(calls)>1:raise OperationFailure('RUNTIME_BYTES_MISMATCH')
        return 'a'*64
    monkeypatch.setattr(runtime_lock,'verify_installed_runtime',verify)
    monkeypatch.setattr(TaskArtifacts,'create',lambda *args:pytest.fail('artifact IO before runtime verification'))
    operations=adapter.layer_operations(object(),object(),lock,'a'*64)
    lock['components']['test']='b'*64
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile={'layer_runtime_sha256':'a'*64}))
    with pytest.raises(OperationFailure) as caught:operations[0].execute(ctx)
    assert caught.value.error.code=='RUNTIME_BYTES_MISMATCH'
    assert calls==['a'*64,'a'*64]
