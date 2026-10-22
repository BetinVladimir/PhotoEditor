from photo_worker.contracts.base import canonical_bytes
"""Orchestration boundaries; component geometry/content covered separately."""
from uuid import UUID
import pytest
from photo_worker.contracts.base import parse_json
from photo_worker.operations.semantic import preview_retouch_context_loader as module
from photo_worker.operations.semantic import preview_retouch_face_geometry as geometry
from photo_worker.operations.semantic.preview_retouch_adapter_binding import bind_retouch_preview_adapter
from test_v27_retouch_preview_context_loader import setup


@pytest.mark.parametrize('loss',[None,'cancel_source','cancel_face','lease_source','lease_face',
    'admission_source','admission_face','qualification','origin'])
def test_face_context_loader_fences_source_and_landmark_io(monkeypatch,loss):
    ctx,params,context_ref,bodies=setup()
    candidate_raw=bodies[UUID(params['candidate']['artifact_id'])]
    candidate=parse_json(candidate_raw)
    result=bind_retouch_preview_adapter(params,candidate_raw,bodies[UUID(params['intent']['artifact_id'])],
        context_ref,bodies[context_ref.artifact_id],bodies[UUID(candidate['mask']['artifact_id'])])
    monkeypatch.setattr(module,'load_retouch_preview_context',lambda *args,**kwargs:result)
    reads=[];denied=False;qualifications=[]
    class Artifacts:
        def declared_input_refs(self,maximum=512):return tuple(ctx.lease.command.inputs)
        def contract_bytes(self,ref,model):
            nonlocal denied
            reads.append(ref)
            phase='source' if len(reads)==1 else 'face'
            if loss=='cancel_'+phase:ctx.active=False
            if loss=='lease_'+phase:ctx.lease.command.operation_version='2'
            if loss=='admission_'+phase:denied=True
            return b'{}'
    def faces(context,raw,*,read_landmarks,verify_origin,check_active):
        assert raw==b'{}'
        if not verify_origin(context.faces[0].landmarks,context.source):raise ValueError('origin denied')
        read_landmarks(context.faces[0].landmarks)
        check_active()
        return 'image',('face',)
    monkeypatch.setattr(geometry,'bind_retouch_preview_faces',faces)
    def qualify(*args):qualifications.append(True);return loss!='qualification'
    def run():return module.load_retouch_preview_face_context(ctx,Artifacts(),params,context_ref,
        verify_admission=lambda *args:not denied,verify_qualification=qualify,
        verify_face_origin=lambda *args:loss!='origin')
    if loss is None:
        assert run()[-2:]==('image',('face',)) and qualifications==[True]
    else:
        with pytest.raises((ValueError,InterruptedError)):run()
    expected=1 if loss in ('cancel_source','lease_source','admission_source','origin') else 2
    assert len(reads)==expected
    if loss not in (None,'qualification'):assert qualifications==[]


def full_face_loader_fixture():
    """Real typed content through both loaders; storage/SQL callbacks are doubles."""
    from hashlib import sha256
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef, canonical_bytes
    from test_v27_exposure_geometry import geometry_fixture
    values = geometry_fixture()
    ctx, params, context_ref, bodies = setup(geometry=values)
    source_raw = values[3]
    source = parse_json(source_raw)
    context = parse_json(bodies[context_ref.artifact_id])
    landmark = context['faces'][0]['landmarks']
    face = dict(object_id=str(uuid4()),
        points=[dict(x=.5, y=.5, confidence=.9) for _ in range(68)],
        canvas=source['canvas'], transform_chain=source['transform_chain'])
    face_raw = canonical_bytes(face)
    landmark['sha256'] = sha256(face_raw).hexdigest()
    context_raw = canonical_bytes(context)
    context_ref.sha256 = sha256(context_raw).hexdigest()
    landmark_ref = ArtifactRef.model_validate_json(canonical_bytes(landmark))
    ctx.lease.command.inputs = [context_ref.model_copy(deep=True)
        if ref.artifact_id == context_ref.artifact_id else landmark_ref.model_copy(deep=True)
        if ref.artifact_id == landmark_ref.artifact_id else ref.model_copy(deep=True)
        for ref in ctx.lease.command.inputs]
    bodies[context_ref.artifact_id] = context_raw
    bodies[UUID(params['source']['source']['artifact_id'])] = source_raw
    bodies[landmark_ref.artifact_id] = face_raw
    return ctx, params, context_ref, bodies, landmark_ref, face


@pytest.mark.parametrize('loss', [None, 'cancel_source', 'cancel_landmark',
    'lease_source', 'lease_landmark', 'admission_source', 'admission_landmark',
    'origin_before', 'origin_after', 'qualification_after', 'mutate_read_refs'])
def test_full_face_loader_uses_real_content_and_fences_each_authority_boundary(loss):
    from photo_worker.contracts.base import canonical_bytes
    from uuid import uuid4
    ctx, params, context_ref, bodies, landmark_ref, expected_face = full_face_loader_fixture()
    frozen_params = canonical_bytes(params)
    frozen_context = canonical_bytes(context_ref)
    reads, origins, qualifications = [], [], []
    denied = False
    source_id = UUID(params['source']['source']['artifact_id'])
    class Artifacts:
        def declared_input_refs(self, maximum=512):
            return tuple(ctx.lease.command.inputs)
        def contract_bytes(self, ref, model):
            nonlocal denied
            raw = bodies[ref.artifact_id]
            reads.append(ref.model_copy(deep=True))
            phase = 'source' if ref.artifact_id == source_id else 'landmark' if ref.artifact_id == landmark_ref.artifact_id else 'context'
            if loss == 'cancel_' + phase: ctx.active = False
            if loss == 'lease_' + phase: ctx.lease.command.operation_version = '2'
            if loss == 'admission_' + phase: denied = True
            if loss == 'mutate_read_refs':
                ref.artifact_id = uuid4()
                ref.sha256 = '0' * 64
            return raw
    def qualify(lease, owned, ref, raw):
        qualifications.append(True)
        assert canonical_bytes(owned) == frozen_params
        assert canonical_bytes(ref) == frozen_context
        assert raw == bodies[context_ref.artifact_id]
        return not (loss == 'qualification_after' and len(qualifications) == 2)
    def origin(lease, ref, source):
        origins.append(True)
        assert ref == landmark_ref and source == params_model.source
        return not (loss == 'origin_before' and len(origins) == 1
            or loss == 'origin_after' and len(origins) == 2)
    from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput
    params_model = SemanticRetouchPreviewInput.model_validate_json(canonical_bytes(params))
    def run():
        return module.load_retouch_preview_face_context(ctx, Artifacts(), params, context_ref,
            verify_admission=lambda *args: not denied, verify_qualification=qualify,
            verify_face_origin=origin)
    if loss in (None, 'mutate_read_refs'):
        result = run()
        assert result[-2].canvas == result[-1][0].canvas
        assert str(result[-1][0].object_id) == expected_face['object_id']
        assert len(reads) == 6 and len(origins) == 2 and len(qualifications) == 2
    else:
        with pytest.raises((ValueError, InterruptedError)): run()
        early = loss.endswith('_source') or loss == 'origin_before'
        assert len(reads) == (5 if early else 6)
        assert len(qualifications) == (2 if loss == 'qualification_after' else 1)
    assert canonical_bytes(params) == frozen_params
    assert canonical_bytes(context_ref) == frozen_context
