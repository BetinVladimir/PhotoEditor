from contextlib import contextmanager
from hashlib import sha256
from io import BytesIO
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.v2.render_inputs import read_render_document


class Artifacts:
    def __init__(self,body):self.body=body;self.closed=False;self.opened=0
    @contextmanager
    def stream(self,ref):
        self.opened+=1
        try:
            with BytesIO(self.body) as stream:yield stream
        finally:self.closed=True


def ref(body):return ArtifactRef.model_validate_json(canonical_bytes(dict(artifact_id=str(uuid4()),sha256=sha256(body).hexdigest())))


def test_bounded_document_read_checks_actual_digest_and_closes_stream():
    body=b'{"ok":true}';artifacts=Artifacts(body)
    assert read_render_document(artifacts,ref(body))==body and artifacts.closed
    changed=Artifacts(body+b' ')
    with pytest.raises(ValueError,match='DIGEST'):read_render_document(changed,ref(body))
    assert changed.closed
    oversized=Artifacts(b'x'*1048577)
    with pytest.raises(ValueError,match='TOO_LARGE'):read_render_document(oversized,ref(oversized.body))
    assert oversized.closed


def test_cancellation_stops_before_io_or_during_read_and_closes_stream():
    artifacts=Artifacts(b'{}')
    def abort():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError):read_render_document(artifacts,ref(b'{}'),abort)
    assert artifacts.opened==0
    count=0
    def abort_read():
        nonlocal count
        count+=1
        if count==2:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError):read_render_document(artifacts,ref(b'{}'),abort_read)
    assert artifacts.closed


def test_full_metadata_loader_preserves_layers_and_rejects_unprotected_mask():
    from photo_worker.contracts.base import canonical_hash
    from photo_worker.v2.render_inputs import load_frozen_render_inputs
    from test_v26_frozen_render_recipe import fixture
    from test_v26_render_layers import fixture as layer_fixture
    value=fixture()
    canvas=dict(width=2,height=2,orientation=1,space_id='source',source_sha256='a'*64)
    chain=dict(steps=[],hash=canonical_hash(dict(steps=[])))
    array=dict(artifact_id=str(uuid4()),sha256='a'*64)
    source=dict(pixels=array,alpha=None,dtype='float32',canvas=canvas,color=dict(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='a'*64),transform_chain=chain)
    source_body=canonical_bytes(source);binding=dict(asset_version_id=str(uuid4()),source=dict(artifact_id=str(uuid4()),sha256=canonical_hash(source)),canvas_hash=canonical_hash(canvas))
    value['recipe']['source']=binding;value['scalars']['source']=binding
    stack=layer_fixture();stack['source']=binding;stack['layers'][0]['mask']['canvas_hash']=binding['canvas_hash']
    value['recipe']['layer_stack']['sha256']=canonical_hash(stack)
    mask=dict(artifact=array,canvas=canvas,transform_chain=chain,object_id=None,selector='protected',confidence=1,model_lock_id=None,protected=True)
    value['recipe']['protected_mask']['sha256']=canonical_hash(mask)
    value['recipe_sha256']=canonical_hash(value['recipe'])
    root=canonical_bytes(value);root_ref=ref(root)
    bodies={root_ref.artifact_id:root}
    for reference,body in [(binding['source'],source_body),(value['recipe']['layer_stack'],canonical_bytes(stack)),(value['recipe']['protected_mask'],canonical_bytes(mask))]:
        bodies[ArtifactRef.model_validate_json(canonical_bytes(reference)).artifact_id]=body
    class Inputs:
        @contextmanager
        def stream(self,reference):
            with BytesIO(bodies[reference.artifact_id]) as stream:yield stream
        def contract(self,reference,model):
            body=bodies[reference.artifact_id]
            assert sha256(body).hexdigest()==reference.sha256
            return model.model_validate_json(body)
    loaded=load_frozen_render_inputs(Inputs(),root_ref)
    assert loaded.layers is not None and loaded.protection.protected
    mask['protected']=False;value['recipe']['protected_mask']['sha256']=canonical_hash(mask)
    value['recipe_sha256']=canonical_hash(value['recipe']);root=canonical_bytes(value);root_ref=ref(root)
    bodies[root_ref.artifact_id]=root;bodies[loaded.recipe.recipe.protected_mask.artifact_id]=canonical_bytes(mask)
    with pytest.raises(ValueError,match='PROTECTION'):load_frozen_render_inputs(Inputs(),root_ref)
