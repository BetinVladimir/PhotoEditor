"""Pure independent-verifier negatives; no database/server dependencies."""
from hashlib import sha256
from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4
import pytest
from v27_target_wire_verification import _read


class Response(BytesIO):
    released = False
    def release_conn(self):
        self.released = True


@pytest.mark.parametrize('corruption', ('none', 'bytes', 'version', 'role', 'attempt', 'fence', 'producer', 'missing', 'receipt'))
def test_target_verifier_requires_exact_producer_and_physical_bytes(corruption):
    body = b'actual pinned bytes'
    digest = sha256(body).hexdigest()
    task = uuid4()
    row = ['bucket', 'key', 'version', digest, len(body), 'face_boxes', task, 2, 7, 'application/json']
    if corruption in ('version', 'role', 'attempt', 'fence', 'producer'):
        index = {'version': 2, 'role': 5, 'attempt': 7, 'fence': 8, 'producer': 6}[corruption]
        row[index] = {'version': 'null', 'role': 'other', 'attempt': 1, 'fence': 6, 'producer': uuid4()}[corruption]
    seen = []
    response = Response(b'x' * len(body) if corruption == 'bytes' else body)
    def get_object(bucket, key, *, version_id):
        seen.append((bucket, key, version_id))
        return response
    db = SimpleNamespace(execute=lambda query, *args: SimpleNamespace(fetchone=lambda: ((0,) if corruption == 'receipt' else (1,)) if 'output_uploads' in query else None if corruption == 'missing' else row))
    objects = SimpleNamespace(get_object=get_object)
    kwargs = dict(producer=task, role='face_boxes', attempt=2, fence=7)
    ref = dict(artifact_id=str(uuid4()), sha256=digest)
    if corruption == 'none':
        assert _read(db, objects, uuid4(), ref, **kwargs) == body
        assert seen == [('bucket', 'key', 'version')]
        assert response.closed and response.released
    else:
        with pytest.raises(AssertionError):
            _read(db, objects, uuid4(), ref, **kwargs)
        if corruption == 'bytes':
            assert response.closed and response.released
        else:
            assert not seen

@pytest.mark.parametrize('registered,input_rows,requested,valid', (
    (False, 0, False, True),
    (False, 1, False, False),
    (False, 0, True, False),
    (True, 0, False, False),
    (True, 1, True, True),
))
def test_target_verifier_distinguishes_preregistration_from_registered_phase(monkeypatch, registered, input_rows, requested, valid):
    import v27_target_wire_verification as verifier
    task, params = uuid4(), uuid4()
    stage = dict(schema_version=1, task_id=str(task), target_ordinal=0,
                 params=dict(artifact_id=str(params), sha256='a'*64), input_digest='d'*64,
                 parent_run_id=str(uuid4()), auth_session_id=str(uuid4()),
                 target_namespace=str(uuid4()), target_stage_id=str(uuid4()),
                 manifest_sha='b'*64, request_sha='c'*64, policy_sha='e'*64,
                 source=dict(source=dict(artifact_id=str(uuid4()), sha256='f'*64)))
    profile = dict(v2_input_manifest={}) if registered else {}
    row = (uuid4(), 'draft', False, {'v27_target_person_prepare': stage}, task,
           'mask.person', 'blocked', None, 0, 0, params, 'd'*64, profile, [])
    def execute(query, *args):
        if 'FROM photo.runs r JOIN photo.tasks' in query:
            return SimpleNamespace(fetchall=lambda: [row])
        if 'v2_task_inputs' in query:
            return SimpleNamespace(fetchone=lambda: (input_rows,))
        if 'task_results' in query:
            return SimpleNamespace(fetchone=lambda: (0,))
        raise AssertionError('unexpected verification query')
    for phase in ('REGISTER', 'SEAL', 'EXECUTE', 'FINISH', 'ADVANCE'):
        monkeypatch.delenv('PHOTO_TEST_V27_TARGET_PERSON_'+phase, raising=False)
    if requested:
        monkeypatch.setenv('PHOTO_TEST_V27_TARGET_PERSON_REGISTER', '1')
    monkeypatch.setattr(verifier, '_read', lambda *args, **kwargs: b'{}')
    call = lambda: verifier._stages(SimpleNamespace(execute=execute), None, uuid4(), 'v27_target_person_prepare', ('mask.person',))
    if valid:
        assert len(call()) == 1
    else:
        with pytest.raises(AssertionError):
            call()
