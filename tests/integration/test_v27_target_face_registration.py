"""Target face registration role and malformed-envelope isolation."""
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from test_v27_target_checkpoint import ROLES
from test_v27_target_detector_registration import seed_target_registration


def test_target_face_registration_roles_and_detector_isolation(db):
    _, case, _, run, _, _, _, _, _, _, _ = seed_target_registration(db)
    signature = 'photo.semantic_target_face_registration_binding_state(uuid,uuid,text,jsonb)'
    private = 'photo.lock_v27_target_face_graph_scope(uuid,uuid,uuid)'
    for role in ROLES:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] == (role == 'photo_scheduler')
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, private)).fetchone()[0]
        if role != 'photo_scheduler':
            with pytest.raises(errors.InsufficientPrivilege):
                with db.transaction():
                    db.execute('SET LOCAL ROLE ' + role)
                    db.execute('SELECT photo.semantic_target_face_registration_binding_state(%s,%s,%s,%s)', (case['Tenant'], run, 'face.landmarks', Jsonb({})))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (case['Tenant'],))
        for operation in [None, 'face.detect', 'face.landmarks', 'face.parse']:
            for body in [None, [], {}, dict(schema_version=2), dict(schema_version=1, task_id='bad')]:
                assert db.execute('SELECT photo.semantic_target_face_registration_binding_state(%s,%s,%s,%s)', (case['Tenant'], run, operation, Jsonb(body))).fetchone()[0] == 'conflict'
        db.execute('RESET ROLE')


def test_target_face_seal_roles_and_empty_evidence(db):
    _, case, _, run, _, _, _, _, _, _, _ = seed_target_registration(db)
    public = 'photo.seal_semantic_target_face_preparation(uuid,uuid,jsonb)'
    private = 'photo.lock_semantic_target_face_sealed_evidence(uuid,uuid,jsonb)'
    for role in ROLES:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role,public)).fetchone()[0] == (role=='photo_scheduler')
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role,private)).fetchone()[0]
        if role!='photo_scheduler':
            with pytest.raises(errors.InsufficientPrivilege):
                with db.transaction():
                    db.execute('SET LOCAL ROLE '+role)
                    db.execute('SELECT photo.seal_semantic_target_face_preparation(%s,%s,%s)',(case['Tenant'],run,Jsonb({})))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
        for body in [None,[],{},dict(schema_version=2),dict(schema_version=1,face={},detector={},detector_result={})]:
            assert db.execute('SELECT photo.seal_semantic_target_face_preparation(%s,%s,%s)',(case['Tenant'],run,Jsonb(body))).fetchone()[0] is None
        db.execute('RESET ROLE')
