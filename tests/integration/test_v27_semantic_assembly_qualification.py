"""Controlled report import/resolution metadata, not physical GPU evidence."""
import hashlib
import json
from uuid import uuid4,uuid5
from psycopg.types.json import Jsonb
from test_v27_semantic_final_producer_gate import fixture


def test_semantic_qualification_requires_selected_import_and_exact_pins(db):
    tid, assembly, _, _, _, _, scope = fixture(db, snapshot_authority=True, qualification=False)
    run = db.execute('SELECT run_id FROM photo.tasks WHERE id=%s', (assembly,)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET operation='fixture.unused' WHERE id=%s", (assembly,))
    def call(name, *args):
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:return db.execute(f"SELECT * FROM photo.{name}({','.join(['%s']*len(args))})", args).fetchall()
        finally:db.execute('RESET ROLE')
    expansion = call('open_v27_semantic_assembly', tid, run)[0][0]
    token = call('claim_v27_semantic_assembly')[0][1]
    def read(model='a'*64, calibration='c'*64, lease=token):
        return call('v27_semantic_assembly_qualification', tid, run, expansion, lease, model, calibration)
    assert read() == []
    actor=scope['actor']
    db.execute('INSERT INTO photo.v2_qualification_operators(operator_id,enabled) VALUES(%s,true)', (actor,))
    policy=db.execute("INSERT INTO photo.v2_qualification_policies(operation,operation_version,model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha,issuer_id,required_checks,test_fixture_shas,calibration_shas,evidence_shas,license_cleared,enabled,admission_enabled) VALUES('semantic.transfer','1',%s,%s,'cuda',%s,%s,%s,ARRAY['quality'],ARRAY[%s],ARRAY[%s],ARRAY[%s],true,true,true) RETURNING id", ('a'*64,'b'*64,'c'*64,'d'*64,actor,'e'*64,'c'*64,'f'*64)).fetchone()[0]
    assert read() == []
    report=dict(schema_version=1,operation='semantic.transfer',version='1',model_lock_sha='a'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64,dataset_sha='d'*64,input_fixture_shas=['e'*64],checks=[dict(id='quality',passed=True,evidence_sha='f'*64)],metrics=dict(p50_ms=10,p95_ms=20,peak_vram_bytes=0,peak_rss_bytes=0,scratch_bytes=0),status='passed',issuer_id=str(actor),created_at='2026-09-01T00:00:00Z')
    body=json.dumps(report,sort_keys=True,separators=(',',':')).encode();sha=hashlib.sha256(body).hexdigest()
    artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,'fixture','qualification/report','pinned-v1',%s,%s,'application/json',clock_timestamp()) RETURNING id", (tid,sha,len(body))).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_maintenance')
    qualification=db.execute('SELECT photo.import_v2_qualification(%s,%s,%s,%s,%s,%s,%s)', (tid,scope['project'],artifact,sha,actor,'cuda',body.decode())).fetchone()[0]
    db.execute('RESET ROLE')
    resolved=read()
    assert len(resolved)==1 and resolved[0][0]=='d'*64 and resolved[0][8:10]==(artifact,sha) and resolved[0][12]=='pinned-v1'
    assert read(model='0'*64)==[] and read(calibration='0'*64)==[] and read(lease=uuid4())==[]

    # A separate immutable-config metadata fixture isolates frozen qualification
    # readiness from graph construction and typed object/report execution.
    calibration=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,'fixture','calibration/document','v1',%s,2,'application/json',clock_timestamp()) RETURNING id",(tid,'c'*64)).fetchone()[0]
    policy_sha=db.execute('SELECT photo.v27_semantic_policy_sha(%s)',(policy,)).fetchone()[0]
    pin=dict(policy_id=str(policy),qualification_id=str(qualification),policy_sha=policy_sha,report=dict(artifact_id=str(artifact),sha256=sha),calibration=dict(artifact_id=str(calibration),sha256='c'*64),model_lock_sha='a'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64,dataset_sha='d'*64,issuer_id=str(actor),backend='cuda')
    frozen_run=uuid4()
    cfg=dict(project_id=str(scope['project']),input_snapshot_id=str(scope['snapshot']),v27_semantic=dict(qualification=pin))
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES(%s,%s,'v2','draft',%s,%s,%s,%s,'fail_fast',false)",(frozen_run,tid,scope['snapshot'],Jsonb(cfg),'b'*64,actor))
    db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'semantic','v2.2',%s)",(uuid5(frozen_run,'workflow'),tid,scope['project'],frozen_run,scope['snapshot'],'b'*64))
    ready=lambda:call('v27_semantic_frozen_qualification_ready',tid,frozen_run)[0][0]
    assert ready()
    assert call('v27_semantic_frozen_qualification_ready',tid,run)==[(False,)]
    assert call('v27_semantic_frozen_qualification_ready',uuid4(),frozen_run)==[(False,)]
    for statement,args in [
        ("UPDATE photo.v2_qualification_policies SET required_checks=ARRAY['changed_acceptance'] WHERE id=%s",(policy,)),
        ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(policy,)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(calibration,)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(artifact,)),
        ('UPDATE photo.projects SET archived_at=clock_timestamp() WHERE id=%s',(scope['project'],)),
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(frozen_run,)),
    ]:
        with db.transaction(force_rollback=True):db.execute(statement,args);assert not ready()
    assert ready()
    # A valid imported report cannot authorize an older workflow without frozen pins.
    assert call('v27_semantic_frozen_assembly_qualification',tid,run,expansion,token,'a'*64,'c'*64,calibration)==[]
    for role in ('photo_api','photo_worker','public'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v27_semantic_frozen_assembly_qualification(uuid,uuid,uuid,uuid,text,text,uuid)','EXECUTE')",(role,)).fetchone()[0]
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v27_semantic_frozen_qualification_ready(uuid,uuid)','EXECUTE')",(role,)).fetchone()[0]

    for sql,args in [
        ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(policy,)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(artifact,)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(artifact,)),
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],)),
    ]:
        with db.transaction(force_rollback=True):db.execute(sql,args);assert read()==[]
    for role in ('photo_api','photo_worker','public'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v27_semantic_assembly_qualification(uuid,uuid,uuid,uuid,text,text)','EXECUTE')",(role,)).fetchone()[0]


def test_frozen_assembly_resolver_selects_exact_report_and_calibration(db):
    tid,assembly,_,_,_,_,scope=fixture(db,snapshot_authority=True,qualification=True)
    run=db.execute('SELECT run_id FROM photo.tasks WHERE id=%s',(assembly,)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET operation='fixture.unused' WHERE id=%s",(assembly,))
    def call(name,*args):
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:
            with db.transaction():
                return db.execute(f"SELECT * FROM photo.{name}({','.join(['%s']*len(args))})",args).fetchall()
        finally:db.execute('RESET ROLE')
    expansion=call('open_v27_semantic_assembly',tid,run)[0][0]
    token=call('claim_v27_semantic_assembly')[0][1]
    pin=scope['qualification'];cal=pin['calibration']
    def read(calibration_id=cal['artifact_id'],model=pin['model_lock_sha'],sha=cal['sha256'],lease=token):
        return call('v27_semantic_frozen_assembly_qualification',tid,run,expansion,lease,model,sha,calibration_id)
    resolved=read()
    assert len(resolved)==1
    assert str(resolved[0][8])==pin['report']['artifact_id'] and resolved[0][9]==pin['report']['sha256']
    assert resolved[0][12]=='pinned-v1'
    # JSON ->> would otherwise coerce this numeric SHA to the same 64 digits.
    malformed=json.loads(json.dumps(pin))
    malformed['calibration']['sha256']=int(cal['sha256'])
    malformed_run=uuid4()
    malformed_cfg=dict(project_id=str(scope['project']),input_snapshot_id=str(scope['snapshot']),v27_semantic=dict(qualification=malformed))
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES(%s,%s,'v2','draft',%s,%s,%s,%s,'fail_fast',false)",(malformed_run,tid,scope['snapshot'],Jsonb(malformed_cfg),'b'*64,scope['actor']))
    db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'semantic','v2.2',%s)",(uuid5(malformed_run,'workflow'),tid,scope['project'],malformed_run,scope['snapshot'],'b'*64))
    assert call('v27_semantic_frozen_qualification_ready',tid,malformed_run)==[(False,)]
    duplicate=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,'fixture','same-sha-other-calibration','pinned-v1',%s,2,'application/json',clock_timestamp()) RETURNING id",(tid,cal['sha256'])).fetchone()[0]
    assert read(calibration_id=duplicate)==[]
    assert read(model='0'*64)==[] and read(sha='0'*64)==[] and read(lease=uuid4())==[]
    for statement,args in [
        ("UPDATE photo.v2_qualification_policies SET required_checks=ARRAY['changed'] WHERE id=%s",(pin['policy_id'],)),
        ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(pin['report']['artifact_id'],)),
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],)),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(statement,args)
            assert read()==[]
    assert len(read())==1
