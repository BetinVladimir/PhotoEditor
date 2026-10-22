"""Real operator report import and exact native installation metadata.

Uses the existing trusted report import boundary; synthetic relational evidence
exercises authority only, and is never a physical renderer qualification.
"""
import hashlib
import json
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb


def installed(db,operation,backend):
    actor=db.execute('INSERT INTO photo.users(email_normalized) VALUES(%s) RETURNING id',(str(uuid4())+'@native.example',)).fetchone()[0]
    tenant=db.execute("INSERT INTO photo.tenants(name,owner_user_id,quotas,retention) VALUES('Native',%s,'{}','{}') RETURNING id",(actor,)).fetchone()[0]
    project=db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Native') RETURNING id",(tenant,)).fetchone()[0]
    db.execute("INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES(%s,%s,'owner','active')",(tenant,actor))
    db.execute('INSERT INTO photo.v2_qualification_operators(operator_id,enabled) VALUES(%s,true)',(actor,))
    pin=dict(model_lock_sha='a'*64,runtime_lock_sha='b'*64,backend=backend,device_profile_sha='c'*64,dataset_sha='d'*64,calibration_sha='9'*64)
    db.execute("INSERT INTO photo.v2_qualification_policies(operation,operation_version,model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha,issuer_id,required_checks,test_fixture_shas,evidence_shas,calibration_shas,license_cleared,enabled,admission_enabled) VALUES(%s,'1',%s,%s,%s,%s,%s,%s,ARRAY['quality'],ARRAY[%s],ARRAY[%s],ARRAY[%s],true,true,true)",(operation,pin['model_lock_sha'],pin['runtime_lock_sha'],backend,pin['device_profile_sha'],pin['dataset_sha'],actor,'e'*64,'f'*64,'9'*64))
    report=dict(schema_version=1,operation=operation,version='1',model_lock_sha=pin['model_lock_sha'],runtime_lock_sha=pin['runtime_lock_sha'],device_profile_sha=pin['device_profile_sha'],dataset_sha=pin['dataset_sha'],input_fixture_shas=['e'*64],checks=[dict(id='quality',passed=True,evidence_sha='f'*64)],metrics=dict(p50_ms=10,p95_ms=20,peak_vram_bytes=0,peak_rss_bytes=0,scratch_bytes=0),status='passed',issuer_id=str(actor),created_at='2026-09-01T00:00:00Z')
    body=json.dumps(report,sort_keys=True,separators=(',',':')).encode();sha=hashlib.sha256(body).hexdigest()
    artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at) VALUES(%s,'test',%s,'v1',%s,%s,'application/json','ready',now()) RETURNING id",(tenant,str(uuid4()),sha,len(body))).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_maintenance')
    db.execute('SELECT photo.import_v2_qualification(%s,%s,%s,%s,%s,%s,%s)',(tenant,project,artifact,sha,actor,backend,body.decode()))
    db.execute('RESET ROLE')
    qualification=db.execute('SELECT id FROM photo.model_qualifications WHERE tenant_id=%s AND report_artifact_id=%s',(tenant,artifact)).fetchone()[0]
    return [tenant,project,operation,Jsonb(pin),'0'*64,qualification,artifact,sha]


def configure(db,args,enabled=True):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_maintenance')
        row=db.execute('SELECT photo.configure_native_render_installation(%s,%s,%s,%s,%s,%s,%s,%s,%s)',(*args,enabled)).fetchone()
        db.execute('RESET ROLE');return row[0]


@pytest.mark.parametrize('operation,backend',[('editor.layers_precompose','cpu'),('render.compose','cpu'),('render.compose','cuda'),('render.compose','mps')])
def test_exact_operator_report_pin_registration_and_revoke(db,operation,backend):
    args=installed(db,operation,backend)
    assert configure(db,args)
    assert db.execute('SELECT photo.v2_artifact_has_references(%s,%s)',(args[0],args[6])).fetchone()==(True,)
    for field in ('model_lock_sha','runtime_lock_sha','backend','device_profile_sha','dataset_sha','calibration_sha'):
        changed=list(args);pin=dict(args[3].obj);pin[field]='cpu' if field=='backend' and backend!='cpu' else ('cuda' if field=='backend' else '1'*64);changed[3]=Jsonb(pin)
        assert not configure(db,changed)
    for index in (0,1,5,6):
        changed=list(args);changed[index]=uuid4();assert not configure(db,changed)
    changed=list(args);changed[7]='1'*64;assert not configure(db,changed)
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval \'1 second\' WHERE id=%s',(args[6],))
        assert not configure(db,args)
    assert configure(db,args,False)
    assert db.execute('SELECT enabled FROM photo.native_render_installations WHERE tenant_id=%s AND project_id=%s AND operation=%s',args[:3]).fetchone()==(False,)


@pytest.mark.parametrize('role',['photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal'])
def test_operator_config_and_private_evidence_are_not_runtime_writable(db,role):
    for signature in ('photo.configure_native_render_installation(uuid,uuid,text,jsonb,text,uuid,uuid,text,boolean)','photo.native_render_installation_evidence(uuid,uuid,text,jsonb,text,uuid,uuid,text)'):
        assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()==(False,)
    assert db.execute('SELECT has_table_privilege(%s,\'photo.native_render_installations\',\'INSERT,UPDATE,DELETE\')',(role,)).fetchone()==(False,)
