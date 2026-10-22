"""Trusted qualification metadata selection; no renderer/GPU measurement claim."""
import hashlib
import json
import pytest
from uuid import uuid4


def resolve(db,tenant,project,calibration="1"*64,evidence="f"*64):
    db.execute('SET LOCAL ROLE photo_api')
    try:
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        return db.execute('SELECT * FROM photo.resolve_v26_region_qualification(%s,%s,%s,%s)',(tenant,project,calibration,evidence)).fetchall()
    finally:
        db.execute('RESET ROLE')


def qualified_region_fixture(db, backend="cuda"):
    actor=db.execute("INSERT INTO photo.users(email_normalized) VALUES('region-qualification@example.test') RETURNING id").fetchone()[0]
    tenant=db.execute("INSERT INTO photo.tenants(name,owner_user_id,quotas,retention) VALUES('Region Qualification',%s,'{}','{}') RETURNING id",(actor,)).fetchone()[0]
    db.execute("INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES(%s,%s,'owner')",(tenant,actor))
    project=db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Region Qualification') RETURNING id",(tenant,)).fetchone()[0]
    model,runtime,device,dataset=('a'*64,'b'*64,'c'*64,'d'*64)
    db.execute('INSERT INTO photo.v2_qualification_operators(operator_id,enabled) VALUES(%s,true)',(actor,))
    policy=db.execute("INSERT INTO photo.v2_qualification_policies(operation,operation_version,model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha,issuer_id,required_checks,test_fixture_shas,evidence_shas,license_cleared,enabled,admission_enabled) VALUES('region.preview','1',%s,%s,%s,%s,%s,%s,ARRAY['quality'],ARRAY[%s],ARRAY[%s],true,true,true) RETURNING id",(model,runtime,backend,device,dataset,actor,'e'*64,'f'*64)).fetchone()[0]
    db.execute("UPDATE photo.v2_qualification_policies SET region_icc_lock_sha=%s,region_execution_policy_sha=%s,calibration_shas=ARRAY[%s] WHERE id=%s",(model,'2'*64,'1'*64,policy))
    report=dict(schema_version=1,operation='region.preview',version='1',model_lock_sha=model,runtime_lock_sha=runtime,device_profile_sha=device,dataset_sha=dataset,input_fixture_shas=['e'*64],checks=[dict(id='quality',passed=True,evidence_sha='f'*64)],metrics=dict(p50_ms=10,p95_ms=20,peak_vram_bytes=0,peak_rss_bytes=0,scratch_bytes=0),status='passed',issuer_id=str(actor),created_at='2026-09-01T00:00:00Z')
    body=json.dumps(report,sort_keys=True,separators=(',',':')).encode()
    sha=hashlib.sha256(body).hexdigest()
    artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at) VALUES(%s,'test','region/qualification','v1',%s,%s,'application/json','ready',now()) RETURNING id",(tenant,sha,len(body))).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_maintenance')
    db.execute('SELECT photo.import_v2_qualification(%s,%s,%s,%s,%s,%s,%s)',(tenant,project,artifact,sha,actor,backend,body.decode()))
    db.execute('RESET ROLE')
    return tenant,project,policy,artifact,model,runtime,device,dataset


@pytest.mark.parametrize("backend", ("cuda", "mps"))
def test_exact_selected_trusted_region_policy(db, backend):
    tenant,project,policy,artifact,model,runtime,device,dataset=qualified_region_fixture(db, backend)
    # Default operation remains disabled even with a controlled trusted report.
    assert resolve(db,tenant,project)==[]
    db.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='region.preview' AND version='1'")
    assert resolve(db,tenant,project)==[(runtime,model,model,device,dataset,"2"*64,backend)]
    assert resolve(db,tenant,project,calibration='0'*64)==[]
    assert resolve(db,tenant,project,evidence='0'*64)==[]
    assert resolve(db,uuid4(),project)==[]
    assert resolve(db,tenant,uuid4())==[]
    for statement,arg in [
        ("UPDATE photo.v2_qualification_policies SET region_icc_lock_sha=NULL WHERE id=%s",policy),
        ("UPDATE photo.v2_qualification_policies SET region_execution_policy_sha=NULL WHERE id=%s",policy),
        ("UPDATE photo.v2_qualification_policies SET calibration_shas=ARRAY[]::text[] WHERE id=%s",policy),
        ("UPDATE photo.v2_qualification_policies SET evidence_shas=ARRAY[repeat('0',64)] WHERE id=%s",policy),
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
        db.execute("UPDATE photo.operation_registry SET backends='[\"cpu\"]' WHERE operation='region.preview' AND version='1'")
        assert resolve(db,tenant,project)==[]
    assert resolve(db,tenant,project)==[(runtime,model,model,device,dataset,"2"*64,backend)]
    # A report qualified for one accelerator cannot authorize the other.
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.v2_qualification_policies SET backend=%s WHERE id=%s",
                   ('mps' if backend == 'cuda' else 'cuda', policy))
        assert resolve(db,tenant,project)==[]
    for role in ('photo_api','photo_scheduler','photo_worker','photo_gateway'):
        assert db.execute("SELECT has_function_privilege(%s,'photo.resolve_v26_region_qualification(uuid,uuid,text,text)','EXECUTE')",(role,)).fetchone()==(role=='photo_api',)


def test_live_go_region_qualification_catalog_and_compiler(database):
    from pathlib import Path
    import os
    import subprocess
    import psycopg
    from psycopg.conninfo import make_conninfo
    with psycopg.connect(**database,autocommit=True) as db:
        with db.transaction():
            tenant,project,*_=qualified_region_fixture(db)
            db.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='region.preview' AND version='1'")
        env=dict(os.environ,PHOTO_REGION_QUALIFICATION_DATABASE_URL=make_conninfo(**database),
                 PHOTO_REGION_QUALIFICATION_CASE=json.dumps(dict(Tenant=str(tenant),Project=str(project))),
                 GOCACHE=str(Path(__file__).resolve().parents[2]/'.cache/go-build'))
        result=subprocess.run(['go','test','./internal/v2region','-run','^TestIntegrationRegionQualificationCompiler$','-count=1'],
            cwd=Path(__file__).resolve().parents[2]/'backend',env=env,capture_output=True,text=True,timeout=45)
        assert result.returncode==0,result.stdout+result.stderr
