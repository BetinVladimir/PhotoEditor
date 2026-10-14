"""Submission helper privilege boundaries; real PostgreSQL, no GPU claims."""
from uuid import uuid4


def test_submission_helpers_scheduler_only(db):
    helpers=(
        'photo.bind_v26_render_submission(uuid,uuid,uuid,uuid,text)',
        'photo.v26_render_submission_state(uuid,uuid,uuid,uuid)',
        'photo.claim_v26_render_submission(uuid,uuid,uuid,uuid,uuid,text,text)',
        'photo.probe_v26_render_submission(uuid,uuid,uuid,uuid,uuid,text,text)',
        'photo.read_v26_render_submission_ticket(uuid,uuid,uuid,uuid,text,text)',
        'photo.complete_v26_render_submission(uuid,uuid,uuid,uuid,text,text)',
        'photo.bind_v26_render_qualification(uuid,uuid,uuid,uuid,jsonb)',
    )
    for role in ('photo_api','photo_scheduler','photo_worker','photo_gateway','photo_maintenance','photo_portal'):
        for helper in helpers:
            assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,helper)).fetchone()==(role=='photo_scheduler',)
        for guard in ('photo.guard_v26_submission_identity()','photo.guard_v26_render_response()','photo.guard_v26_qualification_pin()',
                      'photo.v26_render_qualification_ready_before_pin(uuid,uuid)',
                      'photo.complete_v26_render_submission_before_pin(uuid,uuid,uuid,uuid,text,text)'):
            assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,guard)).fetchone()==(False,)
    db.execute('SET LOCAL ROLE photo_scheduler')
    identity=tuple(uuid4() for _ in range(4))
    assert db.execute('SELECT * FROM photo.v26_render_submission_state(%s,%s,%s,%s)',identity).fetchall()==[]
    assert db.execute('SELECT photo.bind_v26_render_submission(%s,%s,%s,%s,%s)',(*identity,'a'*64)).fetchone()==('expired',)
    db.execute('RESET ROLE')


def test_response_guard_preserves_other_route_retention(db):
    actor=db.execute("INSERT INTO photo.users(email_normalized) VALUES('legacy-idem-render@example.test') RETURNING id").fetchone()[0]
    tenant=db.execute("INSERT INTO photo.tenants(name,owner_user_id,quotas,retention) VALUES('Legacy Idem',%s,'{}','{}') RETURNING id",(actor,)).fetchone()[0]
    record=db.execute("INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,response_status,response_body,expires_at) VALUES(%s,'user',%s,'legacy.edit','retention-check',repeat('a',64),202,'{\"state\":\"original\"}',clock_timestamp()+interval '24 hours') RETURNING id",(tenant,actor)).fetchone()[0]
    db.execute("UPDATE photo.idempotency_records SET response_body='{\"state\":\"changed\"}',expires_at=clock_timestamp() WHERE tenant_id=%s AND id=%s",(tenant,record))
    assert db.execute("SELECT response_body->>'state' FROM photo.idempotency_records WHERE id=%s",(record,)).fetchone()==('changed',)
    db.execute('DELETE FROM photo.idempotency_records WHERE tenant_id=%s AND id=%s',(tenant,record))
    assert db.execute('SELECT count(*) FROM photo.idempotency_records WHERE id=%s',(record,)).fetchone()==(0,)


def test_qualification_pin_rejects_malformed_shape_before_scope(db):
    from psycopg.types.json import Jsonb
    identity=tuple(uuid4() for _ in range(4))
    valid=dict(model_lock_sha='a'*64,runtime_lock_sha='b'*64,backend='cuda',device_profile_sha='c'*64,dataset_sha='d'*64,calibration_sha='e'*64)
    malformed=[None,[],42,{},dict(valid,model_lock_sha=int('1'*64)),dict(valid,calibration_sha=None),dict(valid,extra='unknown'),dict(valid,backend='CUDA')]
    db.execute('SET LOCAL ROLE photo_scheduler')
    for pin in malformed:
        assert db.execute('SELECT photo.bind_v26_render_qualification(%s,%s,%s,%s,%s)',(*identity,Jsonb(pin))).fetchone()==('expired',)
