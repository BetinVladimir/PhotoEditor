"""Face content/provenance boundaries; no actual landmark detector execution."""
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.operations.semantic.preview_retouch_face_geometry import bind_retouch_preview_faces
from test_v27_retouch_preview_adapter_binding import fixture
from test_v27_exposure_geometry import geometry_fixture


@pytest.mark.parametrize('failure',[None,'bytes','noncanonical','geometry','origin','late_origin','cancel_read'])
def test_retouch_face_geometry_binds_source_and_origin_before_after_read(failure):
    _,_,_,_,context_raw,_=fixture();context=parse_json(context_raw)
    edit,_,_,source_raw,*_=geometry_fixture();source=parse_json(source_raw)
    context['source']=edit['source']
    face=dict(object_id=str(uuid4()),points=[dict(x=.5,y=.5,confidence=.9) for _ in range(68)],
        canvas=dict(source['canvas']),transform_chain=source['transform_chain'])
    if failure=='geometry':face['canvas']['source_sha256']='0'*64
    raw=canonical_bytes(face)
    if failure=='noncanonical':raw+=b' '
    context['faces'][0]['landmarks']['sha256']=sha256(raw).hexdigest()
    if failure=='bytes':raw+=b' '
    active=True;reads=[];origins=[]
    def check():
        if not active:raise InterruptedError('cancelled')
    def origin(*args):
        origins.append(True)
        return failure!='origin' and not (failure=='late_origin' and len(origins)==2)
    def read(ref):
        nonlocal active
        reads.append(ref)
        if failure=='cancel_read':active=False
        return raw
    def run():return bind_retouch_preview_faces(context,source_raw,read_landmarks=read,verify_origin=origin,check_active=check)
    if failure is None:
        image,faces=run();assert len(faces)==1 and faces[0].canvas==image.canvas and len(origins)==2
    else:
        with pytest.raises((ValueError,InterruptedError)):run()
    assert len(reads)==(0 if failure=='origin' else 1)


@pytest.mark.parametrize('failure', [None, 'source', 'missing_face', 'duplicate_face',
    'canvas', 'manual_object', 'late_origin', 'noncanonical'])
def test_retouch_selects_two_distinct_faces_from_one_pinned_landmark_collection(failure):
    _, _, _, _, context_raw, _ = fixture()
    context = parse_json(context_raw)
    edit, _, _, source_raw, *_ = geometry_fixture()
    source = parse_json(source_raw)
    context['source'] = edit['source']
    first, second = str(uuid4()), str(uuid4())
    def face(identity):
        return dict(object_id=identity,
            points=[dict(x=.5, y=.5, confidence=.9) for _ in range(68)],
            canvas=dict(source['canvas']), transform_chain=source['transform_chain'])
    entries = [face(first), face(second)]
    if failure == 'duplicate_face':
        entries.append(face(first))
    if failure == 'canvas':
        entries[1]['canvas']['source_sha256'] = '0' * 64
    collection = dict(image=dict(context['source']['source']),
        faces=dict(artifact_id=str(uuid4()), sha256='a' * 64), landmarks=entries,
        model_id='fan2d', model_lock_id='b' * 64, backend='cpu',
        preprocessing_id='fan2d_crop256_v1', mean_confidence=.9, warnings=[])
    if failure == 'source':
        collection['image']['artifact_id'] = str(uuid4())
    raw = canonical_bytes(collection)
    if failure == 'noncanonical':
        raw += b' '
    shared = {**context['faces'][0]['landmarks'], 'sha256': sha256(raw).hexdigest()}
    original = context['faces'][0]
    context['faces'] = [{**original, 'landmarks': shared, 'object_id': first},
        {**original, 'landmarks': shared, 'object_id': second}]
    if failure == 'missing_face':
        context['faces'][1]['object_id'] = str(uuid4())
    if failure == 'manual_object':
        context['faces'][1]['manual'] = {**original['manual'], 'object_id': str(uuid4())}
    reads, origins = [], []
    def read(ref):
        reads.append(ref)
        return raw
    def origin(ref, binding):
        origins.append((ref, binding))
        return not (failure == 'late_origin' and len(origins) == 4)
    def run():
        return bind_retouch_preview_faces(context, source_raw, read_landmarks=read,
            verify_origin=origin, check_active=lambda: None)
    if failure is None:
        image, faces = run()
        assert [str(value.object_id) for value in faces] == [first, second]
        assert all(value.canvas == image.canvas for value in faces)
        assert len(reads) == 2 and len(origins) == 4
        assert reads[0] == reads[1]
    else:
        with pytest.raises(ValueError):
            run()
