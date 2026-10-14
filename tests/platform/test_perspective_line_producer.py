from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage, Canvas, ColorSpace, TransformChain
from photo_worker.contracts.perspective_line_detection import (
    PerspectiveLineDetectionParams, PerspectiveLineDetectionResult)
from photo_worker.operations.optical.perspective_line_detection import execute
from photo_worker.operations.optical.perspective_line_resources import line_detection_resources
from photo_worker.runtime.executor import OperationFailure


def fixture():
    image = WorkingImage(pixels=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),
        alpha=None,dtype='float32',canvas=Canvas(width=100,height=80,orientation=1,
        space_id='source',source_sha256='b'*64),color=ColorSpace(profile_id='linear_srgb_v1',
        encoding='linear',icc_sha256='c'*64),transform_chain=TransformChain.create([]))
    params = PerspectiveLineDetectionParams.model_validate_json(canonical_bytes(dict(
        schema_version=1,base=dict(asset_version_id=str(uuid4()),
        source=dict(artifact_id=str(uuid4()),sha256=canonical_hash(image)),
        canvas_hash=canonical_hash(image.canvas)))))
    profile = dict(backend='cpu',precision='float32',renderer_version='v2.2',
        resource_estimate=line_detection_resources(100,80,False))
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(
        execution_profile=profile,command=SimpleNamespace(inputs=[params.base.source,image.pixels])))
    class Artifacts:
        def declared_contract_refs(self,contract,maximum):
            return (params.base.source,) if contract=='WorkingImage' else (image.pixels,)
        def params(self,model): return params
        def contract_bytes(self,ref,model): return canonical_bytes(image)
        def input(self,*args,**kwargs): raise AssertionError('pixel IO must not occur')
        def stage_contract(self,*args): raise AssertionError('publication must not occur')
    return image, params, ctx, Artifacts()


def test_unsupported_contract_is_explicit_and_source_bound():
    _,params,_,_=fixture()
    result=PerspectiveLineDetectionResult(schema_version=1,base=params.base,
        status='unsupported',lines=None)
    assert result.base==params.base
    with pytest.raises(ValueError):
        PerspectiveLineDetectionResult(schema_version=1,base=params.base,status='available',lines=None)


@pytest.mark.parametrize('mutation', ['closure','canvas','quota','icc','source_hash','source_icc'])
def test_invalid_preflight_precedes_pixel_io_and_publication(mutation):
    _,params,ctx,artifacts=fixture()
    if mutation=='closure': artifacts.declared_contract_refs=lambda *args:()
    if mutation=='canvas': params.base.canvas_hash='d'*64
    if mutation=='source_hash': params.base.source.sha256='e'*64
    if mutation=='source_icc':
        return_icc='d'*64
    else: return_icc='c'*64
    if mutation=='quota': ctx.lease.execution_profile['resource_estimate']['ram_bytes']=0
    with pytest.raises(OperationFailure):
        execute(ctx,artifacts,working_icc_sha='invalid' if mutation=='icc' else return_icc)


def test_cancellation_precedes_contract_io():
    _,_,ctx,artifacts=fixture()
    class Cancelled(Exception): pass
    def cancel(): raise Cancelled()
    ctx.check_active=cancel
    with pytest.raises(Cancelled): execute(ctx,artifacts,working_icc_sha='c'*64)


@pytest.mark.parametrize('available',[False,True])
def test_verified_hdr_input_publishes_source_bound_review_document(tmp_path,monkeypatch,available):
    from contextlib import contextmanager
    import numpy as np
    from photo_worker.operations.optical import perspective_line_evidence
    from photo_worker.contracts.optical_perspective import LineSegments
    image,params,ctx,_=fixture()
    pixels=tmp_path/'pixels.npy'
    rgb=np.full((80,100,3),2.,dtype=np.float32)
    np.save(pixels,rgb,allow_pickle=False)
    lines=LineSegments.model_validate(dict(schema_version=1,lines=[
        dict(a=dict(x=.1,y=.1),b=dict(x=.9,y=.1),family='horizontal'),
        dict(a=dict(x=.1,y=.9),b=dict(x=.9,y=.9),family='horizontal'),
        dict(a=dict(x=.1,y=.1),b=dict(x=.1,y=.9),family='vertical'),
        dict(a=dict(x=.9,y=.1),b=dict(x=.9,y=.9),family='vertical')]))
    def detect(values,alpha,**kwargs):
        assert alpha is None and np.all(values==2.)
        assert not values.flags.writeable
        return lines if available else None
    monkeypatch.setattr(perspective_line_evidence,'detect_perspective_lines',detect)
    published=[]
    output=ArtifactRef(artifact_id=uuid4(),sha256='f'*64)
    class Artifacts:
        def declared_contract_refs(self,contract,maximum):
            return (params.base.source,) if contract=='WorkingImage' else (image.pixels,)
        def params(self,model): return params
        def contract_bytes(self,ref,model):
            assert ref==params.base.source
            return canonical_bytes(image)
        @contextmanager
        def input(self,ref,**kwargs):
            assert ref==image.pixels
            yield SimpleNamespace(path=pixels)
        def stage_contract(self,result,role):
            assert role=='document'
            published.append(result)
            return output
    result=execute(ctx,Artifacts(),working_icc_sha='c'*64)
    assert result.outputs==[output] and result.review_required is True
    assert len(published)==1 and published[0].base==params.base
    assert published[0].status==('available' if available else 'unsupported')
    assert published[0].lines==(lines if available else None)
    assert result.bytes_written==len(canonical_bytes(published[0]))
    assert np.array_equal(np.load(pixels,allow_pickle=False),rgb)


def test_factory_rejects_invalid_deployment_icc():
    from photo_worker.operations.optical.perspective_line_detection import perspective_line_detection_operations
    with pytest.raises(ValueError): perspective_line_detection_operations(None,working_icc_sha='bad')


def test_factory_rejects_wrong_command_before_artifact_creation(monkeypatch):
    from photo_worker.operations.optical import perspective_line_detection as module
    def refuse(*args,**kwargs): raise AssertionError('artifact IO must not occur')
    monkeypatch.setattr(module.TaskArtifacts,'create',refuse)
    operation=module.perspective_line_detection_operations(None,working_icc_sha='c'*64)[0]
    ctx=SimpleNamespace(lease=SimpleNamespace(command=SimpleNamespace(
        operation='optical.perspective',operation_version='1',resource_class='cpu')))
    with pytest.raises(OperationFailure): operation.execute(ctx)
