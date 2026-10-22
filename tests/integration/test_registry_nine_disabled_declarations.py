"""Candidate declaration proof; capability registrations are synthetic SQL fixtures, not model qualification."""
from pathlib import Path
import json
import psycopg
import pytest
from psycopg.types.json import Jsonb
from conftest import database
ROOT=Path(__file__).resolve().parents[2]
PATCH=(ROOT/'backend/db/migrations/02595_registry_nine_disabled_declarations.sql').read_text().split('-- +goose Down')[0]
# Expected independently pinned catalog projection; move beside this candidate test on landing.
EXPECTED=json.loads((Path(__file__).with_name('registry_nine_expected.json')).read_text())
OPS=[x['operation'] for x in EXPECTED]


def state(db):
    return dict(registry=db.execute('SELECT to_jsonb(x) FROM photo.operation_registry x ORDER BY operation,version').fetchall(),
        policies=db.execute('SELECT to_jsonb(x) FROM photo.operation_output_policies x ORDER BY operation,version,role').fetchall(),
        authority=db.execute("SELECT oid,relowner,relacl::text,relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid IN('photo.operation_registry'::regclass,'photo.operation_output_policies'::regclass) ORDER BY oid").fetchall())


def assert_declared(db):
    for entry in EXPECTED:
        row=db.execute('SELECT resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled FROM photo.operation_registry WHERE operation=%s AND version=%s',(entry['operation'],entry['version'])).fetchone()
        assert row==(entry['resource_class'],entry['routing_key'],None,entry['timeout_seconds'],entry['backends'],False)
        policies=db.execute('SELECT role,media_types,max_bytes,max_count FROM photo.operation_output_policies WHERE operation=%s AND version=%s ORDER BY role',(entry['operation'],entry['version'])).fetchall()
        assert policies==sorted((p['role'],p['media_types'],p['max_bytes'],p['max_count']) for p in entry['policies'])
    for runtime in ('photo_api','photo_scheduler','photo_worker','photo_gateway','photo_portal','photo_maintenance'):
        for table in ('photo.operation_registry','photo.operation_output_policies'):
            assert db.execute("SELECT has_table_privilege(%s,%s,'INSERT'),has_any_column_privilege(%s,%s,'UPDATE'),has_table_privilege(%s,%s,'DELETE')",(runtime,table,runtime,table,runtime,table)).fetchone()==(False,False,False)


def test_registry_nine_fresh_head_defaults(database):
    with psycopg.connect(**database) as db: assert_declared(db)


@pytest.mark.migration_head(2594)
@pytest.mark.parametrize('collision',[None,'first','last','routing'])
def test_registry_nine_upgrade_or_collision_rollback(database,collision):
    with psycopg.connect(**database) as db:
        assert db.execute('SELECT count(*) FROM photo.operation_registry WHERE operation=ANY(%s)',(OPS,)).fetchone()==(0,)
        if collision:
            e=EXPECTED[0 if collision=='first' else -1]
            operation='fixture.registry_route_collision' if collision=='routing' else e['operation']
            db.execute("INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled) VALUES(%s,'1','cpu',%s,NULL,1,%s,true)",(operation,e['routing_key'],Jsonb(['cpu'])))
            db.commit()
        before=state(db)
        if collision:
            with pytest.raises(psycopg.errors.RaiseException,match='REGISTRY_NINE_COLLISION'):
                with db.transaction(): db.execute(PATCH)
            assert state(db)==before
        else:
            db.execute(PATCH);assert_declared(db)
            after=state(db);assert after['authority']==before['authority']
            assert [row for row in after['registry'] if row[0]['operation'] not in OPS]==before['registry']
            assert [row for row in after['policies'] if row[0]['operation'] not in OPS]==before['policies']


@pytest.mark.parametrize('operation',OPS)
@pytest.mark.parametrize('enabled',[False,True])
def test_registry_nine_explicit_activation_remains_required(db,operation,enabled):
    from test_capability_qualification_shapes import test_every_declared_v2_capability_requires_current_activation
    test_every_declared_v2_capability_requires_current_activation(db,operation,enabled)
