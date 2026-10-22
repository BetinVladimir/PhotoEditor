"""Actual capability SQL over dispatcher-compatible qualification shapes."""
import pytest
from psycopg.types.json import Jsonb
from test_task_leases import queued_task


def qualify_signer(db, item, expiry="clock_timestamp()+interval '1 hour'"):
    # Isolated operator fixture, not an API/worker self-qualification path.
    # Exact signer-only registration shape is enforced by01399.
    assert expiry in ("clock_timestamp()+interval '1 hour'", "clock_timestamp()-interval '1 hour'")
    db.execute("UPDATE photo.worker_registrations SET execution_pool='signer',"
        "resource_classes='[\"cpu\"]',backends='[\"cpu\"]',"
        "qualified_operations='{\"provenance.sign\":[\"cpu\"]}',"
        "signer_sdk_lock_sha=%s,signer_certificate_chain_sha=%s,"
        "signer_trust_bundle_sha=%s,signer_qualification_sha=%s,"
        "signer_qualified_until="+expiry+" WHERE tenant_id=%s AND worker_id=%s",
        ('a'*64,'b'*64,'c'*64,'d'*64,item['tenant'],item['worker']))


@pytest.mark.parametrize('qualification,expected',[
    (['cpu'],['cpu']),({'cpu':{}},['cpu']),(['cpu','cpu'],['cpu']),
    (['cuda'],[]),([1,True,None,{}],[]),('cpu',[]),(None,[]),([],[]),
])
def test_capabilities_handle_qualification_shapes_without_json_type_errors(db,qualification,expected):
    item=queued_task(db)
    db.execute('UPDATE photo.worker_registrations SET qualified_operations=%s WHERE tenant_id=%s AND worker_id=%s',
        (Jsonb({'asset.verify':qualification}),item['tenant'],item['worker']))
    db.execute("UPDATE photo.resource_nodes SET enabled=true,reported_at=clock_timestamp(),doctor_report_sha256=%s WHERE node_id=%s",('a'*64,item['node']))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(item['tenant']),))
        row=db.execute("SELECT available_backends FROM photo.tenant_capabilities() WHERE operation='asset.verify'").fetchone()
        assert row==(expected,)
        db.execute('RESET ROLE')


@pytest.mark.parametrize('stale',['worker','node'])
def test_capabilities_keep_freshness_gates_for_array_qualification(db,stale):
    item=queued_task(db)
    db.execute("UPDATE photo.resource_nodes SET enabled=true,reported_at=clock_timestamp(),doctor_report_sha256=%s WHERE node_id=%s",('a'*64,item['node']))
    if stale=='worker':
        db.execute("UPDATE photo.worker_registrations SET last_seen=clock_timestamp()-interval '1 hour' WHERE tenant_id=%s AND worker_id=%s",(item['tenant'],item['worker']))
    else:
        db.execute("UPDATE photo.resource_nodes SET reported_at=clock_timestamp()-interval '1 hour' WHERE node_id=%s",(item['node'],))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(item['tenant']),))
        assert db.execute("SELECT available_backends FROM photo.tenant_capabilities() WHERE operation='asset.verify'").fetchone()==([],)
        db.execute('RESET ROLE')


@pytest.mark.parametrize('operation',[
    'semantic.preview_local_exposure','semantic.adopt_shown_exposure',
    'semantic.preview_remove_object','semantic.adopt_shown_removal',
    'semantic.preview_background_cleanup','semantic.adopt_shown_background_cleanup',
    'semantic.preview_retouch_region','semantic.adopt_shown_retouch_region',
])
@pytest.mark.parametrize('enabled',[False,True])
def test_semantic_capability_requires_explicit_registry_activation(db,operation,enabled):
    item=queued_task(db)
    db.execute('UPDATE photo.operation_registry SET v2_enabled=%s WHERE operation=%s AND version=%s',(enabled,operation,'1'))
    db.execute('UPDATE photo.worker_registrations SET resource_classes=%s,backends=%s,qualified_operations=%s '
        'WHERE tenant_id=%s AND worker_id=%s',
        (Jsonb(['cpu','gpu']),Jsonb(['cpu']),Jsonb({operation:['cpu']}),item['tenant'],item['worker']))
    db.execute("UPDATE photo.resource_nodes SET enabled=true,reported_at=clock_timestamp(),doctor_report_sha256=%s WHERE node_id=%s",('a'*64,item['node']))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(item['tenant']),))
        assert db.execute('SELECT available_backends FROM photo.tenant_capabilities() WHERE operation=%s',(operation,)).fetchone()==(['cpu'] if enabled else [],)
        db.execute('RESET ROLE')


@pytest.mark.parametrize('lock_id,ready',[('',False),('invalid',False),('a'*64,True)])
def test_model_backed_capability_requires_valid_worker_model_lock(db,lock_id,ready):
    item=queued_task(db)
    db.execute("UPDATE photo.worker_registrations SET resource_classes=%s,backends=%s,qualified_operations=%s,lock_id=%s WHERE tenant_id=%s AND worker_id=%s",
        (Jsonb(['gpu']),Jsonb(['cuda']),Jsonb({'image.generate':['cuda']}),lock_id,item['tenant'],item['worker']))
    db.execute("UPDATE photo.resource_nodes SET enabled=true,reported_at=clock_timestamp(),doctor_report_sha256=%s WHERE node_id=%s",('a'*64,item['node']))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(item['tenant']),))
        assert db.execute("SELECT model_id,available_backends FROM photo.tenant_capabilities() WHERE operation='image.generate'").fetchone()==('sd15_inpaint',['cuda'] if ready else [])
        db.execute('RESET ROLE')



def declared_v2_operations():
    import json
    from pathlib import Path
    root = Path(__file__).resolve().parents[2]
    return [entry['id'] for entry in json.loads(
        (root / 'docs/implementation-v2/contracts/operations.json').read_text())['operations']]


@pytest.mark.parametrize('operation', declared_v2_operations())
@pytest.mark.parametrize('enabled', [False, True])
def test_every_declared_v2_capability_requires_current_activation(db, operation, enabled):
    item = queued_task(db)
    row = db.execute('SELECT resource_class,backends FROM photo.operation_registry '
        'WHERE operation=%s AND version=%s', (operation,'1')).fetchone()
    assert row is not None, 'declared operation missing from migrated registry'
    resource_class, backends = row
    backend = 'cpu' if 'cpu' in backends else backends[0]
    db.execute('UPDATE photo.operation_registry SET v2_enabled=%s WHERE operation=%s AND version=%s',
        (enabled,operation,'1'))
    if operation == 'provenance.sign':
        qualify_signer(db, item)
    else:
        db.execute('UPDATE photo.worker_registrations SET resource_classes=%s,backends=%s,qualified_operations=%s '
            'WHERE tenant_id=%s AND worker_id=%s',
            (Jsonb([resource_class]),Jsonb([backend]),Jsonb({operation:[backend]}),item['tenant'],item['worker']))
    db.execute('UPDATE photo.resource_nodes SET enabled=true,reported_at=clock_timestamp(),doctor_report_sha256=%s '
        'WHERE node_id=%s', ('a'*64,item['node']))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(item['tenant']),))
        assert db.execute('SELECT available_backends FROM photo.tenant_capabilities() WHERE operation=%s',
            (operation,)).fetchone() == ([backend] if enabled else [],)
        db.execute('RESET ROLE')


@pytest.mark.parametrize('expired', [False, True])
def test_capability_signer_matches_actual_execution_pool_qualification(db, expired):
    item = queued_task(db)
    qualify_signer(db, item, "clock_timestamp()-interval '1 hour'" if expired else "clock_timestamp()+interval '1 hour'")
    db.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='provenance.sign' AND version='1'")
    db.execute("UPDATE photo.resource_nodes SET enabled=true,reported_at=clock_timestamp(),doctor_report_sha256=%s WHERE node_id=%s", ('a'*64,item['node']))
    # Private worker authority and public API advisory agree on expiry. A live
    # heartbeat must not extend operator signer qualification.
    assert db.execute("SELECT photo.worker_authorized(%s,%s,'cpu',%s,'provenance.sign','cpu',NULL)",
        (item['tenant'],item['worker'],item['node'])).fetchone() == (not expired,)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(item['tenant']),))
        assert db.execute("SELECT available_backends FROM photo.tenant_capabilities() WHERE operation='provenance.sign'").fetchone() == ([] if expired else ['cpu'],)
        assert db.execute("SELECT available_backends FROM photo.tenant_capabilities() WHERE operation='asset.verify'").fetchone() == ([],)
        db.execute('RESET ROLE')
