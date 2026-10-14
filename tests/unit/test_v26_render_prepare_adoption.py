from contextlib import contextmanager
from uuid import UUID,uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.v2.adoption import AdoptionInvalid,ArtifactRef,prepare_v2_adoption
from photo_worker.v2.render_adoption_inputs import RenderAdoptionInputs
from photo_worker.v2.render_qualification_pin import RenderQualificationPin
from dataclasses import asdict
from photo_worker.v2.render_prepare_adoption import prepare_render_adoption
from test_v26_render_adoption import fixture
from test_v26_render_recomposition_adoption import expected_pixels


def case():
    reader,frozen,root,binding,parents,put,result=fixture()
    # Remove obsolete staged roots left by the reusable fixture. Every current
    # output remains present and must be reached by the actual inventory walk.
    reachable=set()
    def visit(ref):
        if ref.artifact_id in reachable:return
        body,media,source,_=reader.objects[ref.artifact_id]
        if source!='output':return
        reachable.add(ref.artifact_id)
        if media!='application/json':return
        def descend(value):
            if isinstance(value,dict):
                if set(value)=={'artifact_id','sha256'}:
                    visit(ArtifactRef(UUID(value['artifact_id']),value['sha256']))
                else:
                    for child in value.values():descend(child)
            elif isinstance(value,list):
                for child in value:descend(child)
        descend(parse_json(body))
    visit(root)
    for identifier in list(reader.objects):
        if reader.objects[identifier][2]=='output' and identifier not in reachable:
            del reader.objects[identifier]
    profile=dict(backend='cuda',renderer_version='v2.2',render_runtime_sha='d'*64,
        model_lock_sha='b'*64,icc_lock_sha='a'*64,tile_size=512,alpha_mode='require_float32')
    inputs=RenderAdoptionInputs(frozen,canonical_bytes(profile),parents,RenderQualificationPin('b'*64,'d'*64,'cuda','e'*64,'f'*64,'9'*64))
    return reader,root,inputs


def test_complete_replay_and_actual_inventory_are_prepared_within_owner(tmp_path):
    reader,root,inputs=case();state={'open':False,'closed':False}
    @contextmanager
    def replay(actual_reader,actual_inputs,frozen):
        assert actual_reader is not reader and actual_inputs is inputs
        assert actual_reader.read_json(inputs.recipe,1048576)
        assert frozen.recipe.source.source.artifact_id
        state['open']=True
        try:yield expected_pixels()
        finally:state['open']=False;state['closed']=True
    original=reader.iter_bytes
    def read(ref,limit):
        assert state['open']
        yield from original(ref,limit)
    reader.iter_bytes=read
    prepared=prepare_render_adoption(uuid4(),root,reader,inputs,replay,
        scratch_root=tmp_path,max_bytes=100000,max_tile_bytes=4096)
    assert prepared.inventory[0]['artifact_id']==str(root.artifact_id)
    assert len(prepared.digest)==64 and state['closed']
    assert reader.opened==reader.closed and not list(tmp_path.iterdir())
    staged={str(ref.artifact_id) for ref in reader.staged_outputs()}
    assert staged <= {ref['artifact_id'] for ref in prepared.inventory}


def test_generic_render_inventory_requires_independent_context():
    reader,root,_=case()
    with pytest.raises(AdoptionInvalid,match='CONTEXT_REQUIRED'):
        prepare_v2_adoption(task_id=uuid4(),operation='render.compose',version='1',root=root,reader=reader)


@pytest.mark.parametrize('failure',['absent','wrong_pixels','orphan','profile'])
def test_failed_preparation_does_not_return_inventory_and_releases_owner(tmp_path,failure):
    import numpy as np
    from photo_worker.v2.render_adjustments import AdjustmentPixels
    reader,root,inputs=case();state={'closed':False}
    @contextmanager
    def replay(*args):
        try:
            expected=expected_pixels()
            if failure=='wrong_pixels':
                wrong=np.zeros_like(expected.rgb);wrong.flags.writeable=False
                expected=AdjustmentPixels(wrong,None,expected.generated)
            yield expected
        finally:state['closed']=True
    if failure=='orphan':
        reader.objects[uuid4()]=(b'{}','application/json','output','document')
    if failure=='profile':
        profile=inputs.execution_profile();profile['model_lock_sha']='e'*64
        inputs=RenderAdoptionInputs(inputs.recipe,canonical_bytes(profile),inputs.parent_roots,inputs.qualification_pin)
    with pytest.raises(Exception):
        prepare_render_adoption(uuid4(),root,reader,inputs,None if failure=='absent' else replay,
            scratch_root=tmp_path,max_bytes=100000,max_tile_bytes=4096)
    assert state['closed'] == (failure not in ('absent','profile'))
    assert reader.opened==reader.closed and not list(tmp_path.iterdir())


@pytest.mark.parametrize('accepted',[True,False])
def test_coordinator_prepares_real_bytes_before_canonical_sql(monkeypatch,tmp_path,accepted):
    from photo_worker.v2 import adoption_service as service
    reader,root,inputs=case();tenant,task,result=uuid4(),uuid4(),uuid4()
    state={'owner':False,'closed':False,'tx':False};queries=[]
    class Cursor:
        def __init__(self,row):self.row=row
        def fetchone(self):return self.row
    class Read:
        autocommit=True
        def execute(self,query,args):
            assert not state['tx'];queries.append(query)
            if 'v2_adoption_context' in query:
                return Cursor((1,2,result,'render.compose','1',root.artifact_id,root.sha256))
            if 'v26_render_adoption_qualification' in query:return Cursor((asdict(inputs.qualification_pin),))
            assert 'v26_render_adoption_inputs' in query and args==(tenant,task,1,2,result)
            return Cursor((inputs.recipe.artifact_id,inputs.recipe.sha256,inputs.execution_profile(),
                [{'artifact_id':str(ref.artifact_id),'sha256':ref.sha256} for ref in inputs.parent_roots]))
    class Write:
        @contextmanager
        def transaction(self):
            assert state['closed'] and not state['owner'];state['tx']=True
            try:yield
            finally:state['tx']=False
        def execute(self,query,args):
            assert state['tx'] and args[:5]==(tenant,task,1,2,result)
            if 'publish_v26_render_cache' in query:
                assert len(args[5])==64 and parse_json(args[6])['tenant_id']==str(tenant)
                assert args[7].obj[0]['artifact_id']==str(root.artifact_id)
                return Cursor((accepted,))
            assert args[5].obj[0]['artifact_id']==str(root.artifact_id)
            return Cursor((accepted,))
    @contextmanager
    def replay(*args):
        assert not state['tx'];state['owner']=True
        assert len(args[1].cache_identity)==64
        try:yield expected_pixels()
        finally:state['owner']=False;state['closed']=True
    monkeypatch.setattr(service,'PostgresObjectEvidenceReader',lambda *args,**kwargs:reader)
    coordinator=service.AdoptionCoordinator(Read(),Write(),object(),'private',
        layer_scratch_root=tmp_path,layer_scratch_bytes=100000,
        render_replay=replay,render_max_tile_bytes=4096)
    assert coordinator.adopt(tenant,task) is accepted
    assert reader.opened==reader.closed and not list(tmp_path.iterdir())


@pytest.mark.parametrize('kind',['noncanonical','corrupt','output_scope'])
def test_bad_frozen_recipe_never_invokes_replay(tmp_path,kind):
    from hashlib import sha256
    reader,root,inputs=case();ref=inputs.recipe
    body,media,scope,role=reader.objects[ref.artifact_id]
    if kind=='noncanonical':
        body=b' '+body
        ref=ArtifactRef(ref.artifact_id,sha256(body).hexdigest())
        inputs=RenderAdoptionInputs(ref,inputs.execution_profile_jcs,inputs.parent_roots,inputs.qualification_pin)
    elif kind=='corrupt':body=body[:-1]+b'x'
    else:scope='output'
    reader.objects[ref.artifact_id]=(body,media,scope,role)
    called=[]
    def replay(*args):called.append(True);raise AssertionError('unexpected replay')
    with pytest.raises(AdoptionInvalid,match='RECIPE_INVALID'):
        prepare_render_adoption(uuid4(),root,reader,inputs,replay,
            scratch_root=tmp_path,max_bytes=100000,max_tile_bytes=4096)
    assert not called and not list(tmp_path.iterdir())


@pytest.mark.parametrize('method',['read_json','iter_bytes','evidence','staged_outputs'])
def test_replay_cannot_read_submitted_output_as_its_oracle(tmp_path,method):
    reader,root,inputs=case();closed=[]
    @contextmanager
    def replay(view,*args):
        try:
            if method=='staged_outputs':view.staged_outputs()
            elif method=='iter_bytes':list(view.iter_bytes(root,1048576))
            elif method=='read_json':view.read_json(root,1048576)
            else:view.evidence(root)
            yield expected_pixels()
        finally:closed.append(True)
    with pytest.raises(AdoptionInvalid,match='REPLAY_INPUT_REQUIRED'):
        prepare_render_adoption(uuid4(),root,reader,inputs,replay,
            scratch_root=tmp_path,max_bytes=100000,max_tile_bytes=4096)
    assert closed and reader.opened==reader.closed and not list(tmp_path.iterdir())
