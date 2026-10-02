"""Trusted qualification metadata selection; no renderer/GPU measurement claim."""
import hashlib
import json
from uuid import uuid4


def resolve(db,tenant,project):
    db.execute('SET LOCAL ROLE photo_api')
    try:
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        return db.execute('SELECT * FROM photo.resolve_v26_render_qualification(%s,%s)',(tenant,project)).fetchall()
    finally:
        db.execute('RESET ROLE')


def test_exact_selected_trusted_render_policy(db):
    actor=db.execute("INSERT INTO photo.users(email_normalized) VALUES('render-qualification@example.test') RETURNING id").fetchone()[0]
    tenant=db.execute("INSERT INTO photo.tenants(name,owner_user_id,quotas,retention) VALUES('Render Qualification',%s,'{}','{}') RETURNING id",(actor,)).fetchone()[0]
    db.execute("INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES(%s,%s,'owner')",(tenant,actor))
    project=db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Render Qualification') RETURNING id",(tenant,)).fetchone()[0]
    model,runtime,device,dataset=('a'*64,'b'*64,'c'*64,'d'*64)
    db.execute('INSERT INTO photo.v2_qualification_operators(operator_id,enabled) VALUES(%s,true)',(actor,))
    policy=db.execute("INSERT INTO photo.v2_qualification_policies(operation,operation_version,model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha,issuer_id,required_checks,test_fixture_shas,evidence_shas,license_cleared,enabled,admission_enabled) VALUES('render.compose','1',%s,%s,'cuda',%s,%s,%s,ARRAY['quality'],ARRAY[%s],ARRAY[%s],true,true,true) RETURNING id",(model,runtime,device,dataset,actor,'e'*64,'f'*64)).fetchone()[0]
    report=dict(schema_version=1,operation='render.compose',version='1',model_lock_sha=model,runtime_lock_sha=runtime,device_profile_sha=device,dataset_sha=dataset,input_fixture_shas=['e'*64],checks=[dict(id='quality',passed=True,evidence_sha='f'*64)],metrics=dict(p50_ms=10,p95_ms=20,peak_vram_bytes=0,peak_rss_bytes=0,scratch_bytes=0),status='passed',issuer_id=str(actor),created_at='2026-09-01T00:00:00Z')
    body=json.dumps(report,sort_keys=True,separators=(',',':')).encode()
    sha=hashlib.sha256(body).hexdigest()
    artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at) VALUES(%s,'test','render/qualification','v1',%s,%s,'application/json','ready',now()) RETURNING id",(tenant,sha,len(body))).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_maintenance')
    db.execute('SELECT photo.import_v2_qualification(%s,%s,%s,%s,%s,%s,%s)',(tenant,project,artifact,sha,actor,'cuda',body.decode()))
    db.execute('RESET ROLE')
    # Default operation remains disabled even with a controlled trusted report.
    assert resolve(db,tenant,project)==[]
    db.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='render.compose' AND version='1'")
    assert resolve(db,tenant,project)==[(model,runtime,'cuda',device,dataset)]
    assert resolve(db,uuid4(),project)==[]
    assert resolve(db,tenant,uuid4())==[]
    for statement,arg in [
        ("UPDATE photo.v2_qualification_policies SET runtime_lock_sha=repeat('0',64) WHERE id=%s",policy),
        ("UPDATE photo.v2_qualification_policies SET model_lock_sha=repeat('0',64) WHERE id=%s",policy),
        ("UPDATE photo.v2_qualification_policies SET device_profile_sha=repeat('0',64) WHERE id=%s",policy),
        ("UPDATE photo.v2_qualification_policies SET dataset_sha=repeat('0',64) WHERE id=%s",policy),
        ("UPDATE photo.v2_qualification_policies SET backend='cpu' WHERE id=%s",policy),
        ("UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s",policy),
        ("UPDATE photo.v2_qualification_policies SET admission_enabled=false,license_cleared=false WHERE id=%s",policy),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",artifact),
        ("UPDATE photo.projects SET archived_at=clock_timestamp() WHERE id=%s",project),
        ("UPDATE photo.tenants SET status='deleting' WHERE id=%s",tenant),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(statement,(arg,))
            assert resolve(db,tenant,project)==[],statement
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.operation_registry SET backends='[\"cpu\"]' WHERE operation='render.compose' AND version='1'")
        assert resolve(db,tenant,project)==[]
    assert resolve(db,tenant,project)==[(model,runtime,'cuda',device,dataset)]
    for role in ('photo_api','photo_scheduler','photo_worker','photo_gateway'):
        assert db.execute("SELECT has_function_privilege(%s,'photo.resolve_v26_render_qualification(uuid,uuid)','EXECUTE')",(role,)).fetchone()==(role=='photo_api',)
        assert db.execute("SELECT has_function_privilege(%s,'photo.v26_render_qualification_ready(uuid,uuid)','EXECUTE')",(role,)).fetchone()==(False,)
