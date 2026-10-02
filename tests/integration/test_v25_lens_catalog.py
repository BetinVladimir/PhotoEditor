"""Actual migrated role policies must match the normative lens output contract."""
import json
from pathlib import Path


def test_lens_catalog_matches_normative_output_policies(db):
    root=Path(__file__).resolve().parents[2]
    operation=next(item for item in json.loads((root/'docs/implementation-v2/contracts/operations.json').read_text())['operations'] if item['id']=='optical.lens')
    actual={role:(media,size,count) for role,media,size,count in db.execute(
        "SELECT role,media_types,max_bytes,max_count FROM photo.operation_output_policies WHERE operation='optical.lens' AND version='1'").fetchall()}
    assert actual=={role:(policy['media_types'],policy['max_bytes'],policy['max_count']) for role,policy in operation['role_policies'].items()}
    row=db.execute("SELECT resource_class,timeout_seconds,backends FROM photo.operation_registry WHERE operation='optical.lens' AND version='1'").fetchone()
    assert row==('cpu',operation['timeout_seconds'],['cpu'])


def test_lens_document_reservation_requires_frozen_scope(db):
    from uuid import uuid4
    from test_database import tenant
    tid,_=tenant(db,str(uuid4())+'@example.test')
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)',(tid,))
    before=db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()
    db.execute('SET LOCAL ROLE photo_scheduler')
    for purpose in ['v25_lens_params','v25_lens_original_binding','task_params']:
        row=db.execute('SELECT outcome,artifact_id,token,ready FROM photo.reserve_v25_lens_document(%s,%s,%s,%s,%s,100,%s)',
            (tid,uuid4(),uuid4(),purpose,'a'*64,'test')).fetchone()
        assert row==('forbidden',None,None,False)
    assert not db.execute('SELECT photo.stage_v25_lens_task(%s,%s,%s,0,%s,%s,%s,%s::jsonb)',(tid,uuid4(),uuid4(),uuid4(),'a'*64,'b'*64,'{}')).fetchone()[0]
    db.execute('RESET ROLE')
    assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()==before
    assert not db.execute("SELECT has_function_privilege('photo_api','photo.reserve_v25_lens_document(uuid,uuid,uuid,text,text,bigint,text)','EXECUTE')").fetchone()[0]
