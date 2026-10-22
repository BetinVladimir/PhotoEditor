"""Declared-context loader scope boundaries; no SQL/storage qualification proof."""
from hashlib import sha256
from types import SimpleNamespace
from uuid import UUID,uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,parse_json
from photo_worker.operations.semantic.preview_retouch_context_loader import load_retouch_preview_context
from test_v27_retouch_preview_adapter_binding import fixture


def setup(*, geometry=None):
    params,candidate_raw,intent_raw,context_ref,context_raw,mask_raw=fixture(geometry=geometry)
    candidate=parse_json(candidate_raw);context=parse_json(context_raw)
    def ref(value):return ArtifactRef.model_validate_json(canonical_bytes(value))
    refs=[ref(params[key]) for key in ('plan','intent','candidate','protection')]+[ref(params['source']['source']),
        context_ref,ref(candidate['mask']),ref(candidate['transform']),ref(context['faces'][0]['landmarks']),
        ref(context['faces'][0]['manual']['artifact'])]
    command=SimpleNamespace(operation='semantic.preview_retouch_region',operation_version='1',
        tenant_id=UUID(params['tenant_id']),
        params=ArtifactRef(artifact_id=uuid4(),sha256=sha256(canonical_bytes(params)).hexdigest()),inputs=refs)
    ctx=SimpleNamespace(lease=SimpleNamespace(command=command),active=True)
    def active():
        if not ctx.active:raise InterruptedError('cancelled')
    ctx.check_active=active
    bodies={ref(params['candidate']).artifact_id:candidate_raw,ref(params['intent']).artifact_id:intent_raw,
        context_ref.artifact_id:context_raw,ref(candidate['mask']).artifact_id:mask_raw}
    return ctx,params,context_ref,bodies


@pytest.mark.parametrize('boundary',[1,2,3,4])
@pytest.mark.parametrize('loss',['cancel','lease','admission'])
def test_retouch_context_loader_stops_after_each_lost_read(boundary,loss):
    ctx,params,context_ref,bodies=setup();reads=[];denied=False
    class Artifacts:
        def declared_input_refs(self,maximum=512):return tuple(ctx.lease.command.inputs)
        def contract_bytes(self,ref,model):
            nonlocal denied
            reads.append(ref)
            if len(reads)==boundary:
                if loss=='cancel':ctx.active=False
                if loss=='lease':ctx.lease.command.operation_version='2'
                if loss=='admission':denied=True
            return bodies[ref.artifact_id]
    def qualification(*args):raise AssertionError('qualification after lost read')
    with pytest.raises((ValueError,InterruptedError)):
        load_retouch_preview_context(ctx,Artifacts(),params,context_ref,
            verify_admission=lambda *args:not denied,verify_qualification=qualification)
    assert len(reads)==boundary


@pytest.mark.parametrize('qualified',[True,False,None,1])
def test_retouch_context_loader_requires_exact_qualification_true(qualified):
    ctx,params,context_ref,bodies=setup();reads=[];qualified_calls=[]
    class Artifacts:
        def declared_input_refs(self,maximum=512):return tuple(ctx.lease.command.inputs)
        def contract_bytes(self,ref,model):reads.append(ref);return bodies[ref.artifact_id]
    def qualification(lease,owned,ref,raw):
        assert lease==ctx.lease and lease is not ctx.lease
        assert owned.kind=='retouch_region' and ref==context_ref and raw==bodies[context_ref.artifact_id]
        ref.sha256='0'*64
        qualified_calls.append(True)
        return qualified
    def run():return load_retouch_preview_context(ctx,Artifacts(),params,context_ref,
        verify_admission=lambda *args:True,verify_qualification=qualification)
    if qualified is True:
        assert run()[3].algorithm=='beautyfilter_v1'
    else:
        with pytest.raises(ValueError,match='QUALIFICATION_DENIED'):run()
    assert len(reads)==4 and qualified_calls==[True]
    assert context_ref.sha256!='0'*64


def test_retouch_context_loader_refuses_undeclared_context_before_io():
    ctx,params,context_ref,_=setup();ctx.lease.command.inputs=[ref for ref in ctx.lease.command.inputs if ref!=context_ref]
    class Artifacts:
        def declared_input_refs(self,maximum=512):return tuple(ctx.lease.command.inputs)
        def contract_bytes(self,*args):raise AssertionError('undeclared context IO')
    with pytest.raises(ValueError,match='UNDECLARED_INPUT'):
        load_retouch_preview_context(ctx,Artifacts(),params,context_ref,
            verify_admission=lambda *args:True,verify_qualification=lambda *args:True)


@pytest.mark.parametrize('missing',['mask','transform'])
def test_retouch_context_loader_requires_transfer_closure_before_context_io(missing):
    ctx,params,context_ref,bodies=setup()
    candidate=parse_json(bodies[UUID(params['candidate']['artifact_id'])])
    missing_id=UUID(candidate[missing]['artifact_id'])
    ctx.lease.command.inputs=[ref for ref in ctx.lease.command.inputs if ref.artifact_id!=missing_id]
    reads=[]
    class Artifacts:
        def declared_input_refs(self,maximum=512):return tuple(ctx.lease.command.inputs)
        def contract_bytes(self,ref,model):reads.append(ref);return bodies[ref.artifact_id]
    def qualification(*args):raise AssertionError('qualification on incomplete closure')
    with pytest.raises(ValueError,match='UNDECLARED_INPUT'):
        load_retouch_preview_context(ctx,Artifacts(),params,context_ref,
            verify_admission=lambda *args:True,verify_qualification=qualification)
    assert len(reads)==2 and context_ref not in reads


@pytest.mark.parametrize('failure',['context_alias','params_sha'])
def test_retouch_context_loader_rejects_scope_alias_or_changed_params_before_io(failure):
    ctx,params,context_ref,_=setup()
    if failure=='context_alias':context_ref=ArtifactRef.model_validate_json(canonical_bytes(params['candidate']))
    else:ctx.lease.command.params.sha256='0'*64
    class Artifacts:
        def declared_input_refs(self,maximum=512):return tuple(ctx.lease.command.inputs)
        def contract_bytes(self,*args):raise AssertionError('scope defect before IO')
    with pytest.raises(ValueError):
        load_retouch_preview_context(ctx,Artifacts(),params,context_ref,
            verify_admission=lambda *args:True,verify_qualification=lambda *args:True)


def test_retouch_context_uses_index_dependencies_with_transport_only_command():
    ctx,params,context_ref,bodies=setup()
    dependencies=tuple(ref.model_copy(deep=True) for ref in ctx.lease.command.inputs)
    ctx.lease.command.inputs=[]
    class Artifacts:
        def declared_input_refs(self,maximum=512):return dependencies
        def contract_bytes(self,ref,model):return bodies[ref.artifact_id]
    def qualification(owner,*args):
        assert owner.command.inputs==[] and ctx.lease.command.inputs==[]
        return True
    result=load_retouch_preview_context(ctx,Artifacts(),params,context_ref,
        verify_admission=lambda *a:True,verify_qualification=qualification)
    assert result[3].algorithm=='beautyfilter_v1'
    assert ctx.lease.command.inputs==[]


@pytest.mark.parametrize('boundary', [1, 2, 3, 4])
def test_retouch_context_reader_cannot_mutate_frozen_input_references(boundary):
    ctx, params, context_ref, bodies = setup()
    original_params = canonical_bytes(params)
    original_context = canonical_bytes(context_ref)
    reads = []
    class Artifacts:
        def declared_input_refs(self, maximum=512):
            return tuple(ctx.lease.command.inputs)
        def contract_bytes(self, ref, model):
            raw = bodies[ref.artifact_id]
            reads.append(ref.model_copy(deep=True))
            if len(reads) == boundary:
                ref.artifact_id = uuid4()
                ref.sha256 = '0' * 64
            return raw
    def qualification(lease, owned, ref, raw):
        assert canonical_bytes(owned) == original_params
        assert canonical_bytes(ref) == original_context
        assert raw == bodies[context_ref.artifact_id]
        return True
    owned, candidate, intent, context = load_retouch_preview_context(
        ctx, Artifacts(), params, context_ref,
        verify_admission=lambda *args: True, verify_qualification=qualification)
    assert canonical_bytes(owned) == original_params
    assert context.candidate == owned.candidate
    assert context.transferred_mask == candidate.mask
    assert intent.kind == 'retouch_region' and len(reads) == 4
    assert canonical_bytes(params) == original_params
    assert canonical_bytes(context_ref) == original_context


@pytest.mark.parametrize('missing', ['landmarks', 'manual'])
def test_retouch_context_requires_face_closure_before_qualification(missing):
    ctx, params, context_ref, bodies = setup()
    context = parse_json(bodies[context_ref.artifact_id])
    face = context['faces'][0]
    identity = UUID(face['landmarks']['artifact_id'] if missing == 'landmarks'
        else face['manual']['artifact']['artifact_id'])
    ctx.lease.command.inputs = [ref for ref in ctx.lease.command.inputs if ref.artifact_id != identity]
    reads = []
    class Artifacts:
        def declared_input_refs(self, maximum=512): return tuple(ctx.lease.command.inputs)
        def contract_bytes(self, ref, model):
            reads.append(ref)
            return bodies[ref.artifact_id]
    def forbidden(*args): raise AssertionError('qualification with incomplete face closure')
    with pytest.raises(ValueError, match='UNDECLARED_INPUT'):
        load_retouch_preview_context(ctx, Artifacts(), params, context_ref,
            verify_admission=lambda *args: True, verify_qualification=forbidden)
    assert len(reads) == 4
