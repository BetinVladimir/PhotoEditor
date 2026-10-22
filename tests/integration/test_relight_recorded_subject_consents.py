"""Authored recorded dependency gates; no inference/qualification execution claim."""
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes
from test_v28_relight_lease import relight_lease_fixture
from test_v28_relight_person import seed_person_inputs
from test_v28_relight_adoption_source import complete_relight_task


def person_fixture(db, *, linked):
    retained = {}
    def record_link(connection, tenant, scope, workflow):
        consent=connection.execute("INSERT INTO photo.consent_grants(tenant_id,project_id,purpose,status,epoch,policy_version,actor_id) VALUES(%s,%s,'learning_photographer','granted',1,'fixture-v1',%s) RETURNING id",(tenant,scope['project'],scope['actor'])).fetchone()[0]
        link=connection.execute('INSERT INTO photo.workflow_consent_links(tenant_id,project_id,workflow_id,consent_id,consent_epoch) VALUES(%s,%s,%s,%s,1) RETURNING id',(tenant,scope['project'],workflow,consent)).fetchone()[0]
        retained.update(consent=consent,link=link)
    def setup(connection, tenant, scope):
        return seed_person_inputs(connection, tenant, scope, retained, typed_workflow=linked, workflow_setup=record_link if linked else None)
    def body(pin, scope):
        return canonical_bytes(dict(base=dict(asset_version_id=str(scope['version']),
            source=dict(artifact_id=str(scope['source']), sha256=scope['source_sha']),
            canvas_hash='c'*64), calibration=pin['calibration'], subject=scope['subject_ref']))
    args, scope, _ = relight_lease_fixture(db, qualified=True, params_builder=body, extra_inputs_factory=setup)
    workflow = retained.get('workflow')
    project = scope['project']
    return args, scope, retained, workflow, project


def current(db, tenant, subject):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        result=db.execute('SELECT photo.v28_relight_recorded_subject_consents_live(%s,%s)', (tenant, Jsonb(subject))).fetchone()[0]
        db.execute('RESET ROLE')
        return result


@pytest.mark.parametrize('state', ['no_link', 'active_link', 'revoked', 'epoch'])
def test_recorded_links_gate_worker_and_completed_adoption(db, state):
    args, scope, retained, workflow, project = person_fixture(db, linked=state != 'no_link')
    assert current(db, args[0], scope['subject_ref']) is True
    grant = retained.get('consent')
    assert current(db, args[0], scope['subject_ref']) is True
    if state == 'revoked':
        db.execute("UPDATE photo.consent_grants SET status='revoked' WHERE id=%s", (grant,))
    if state == 'epoch':
        db.execute('UPDATE photo.consent_grants SET epoch=2 WHERE id=%s', (grant,))
    allowed = state in ('no_link', 'active_link')
    assert current(db, args[0], scope['subject_ref']) is allowed
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_worker')
        actual = db.execute('SELECT photo.v28_worker_relight_person_origin('+','.join(['%s']*11)+')', (*args, retained['body'].decode())).fetchone()[0]
        assert (actual is not None) is allowed
        db.execute('RESET ROLE')
    result, _ = complete_relight_task(db, args)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        actual = db.execute('SELECT photo.v28_relight_adoption_person_origin(%s,%s,1,1,%s,%s,%s)', (args[0], args[2], result, args[8], retained['body'].decode())).fetchone()[0]
        assert (actual is not None) is allowed
        db.execute('RESET ROLE')


def test_recorded_consent_helper_has_no_public_authority(db):
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'public'):
        assert db.execute("SELECT has_function_privilege(%s,'photo.v28_relight_recorded_subject_consents_live(uuid,jsonb)','EXECUTE')", (role,)).fetchone()[0] is False
