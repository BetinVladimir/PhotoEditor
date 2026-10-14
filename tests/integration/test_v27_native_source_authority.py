"""Positive composed native SQL locks; synthetic imported report, no inference."""
from copy import deepcopy
import json
from uuid import uuid4

import pytest
import psycopg
from psycopg.types.json import Jsonb
from layer_original_fixture import seed_original_composition
from semantic_qualification_fixture import frozen_qualification


def seed_native_source_authority(db, operation):
    case = seed_original_composition(db, snapshot_canvas=True)
    tenant, project, actor, session = [case[key] for key in ['Tenant','Project','Actor','Session']]
    binding = json.loads(case['Params'])['base']
    params = case['Artifact']
    db.execute("UPDATE photo.artifacts SET output_role='document' WHERE id=%s",(params['artifact_id'],))
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)', (tenant,))
    producer_run, producer_task = uuid4(), uuid4()
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'v2',%s,%s,%s,%s,'fail_fast')",(producer_run,tenant,case['Snapshot'],Jsonb(dict(project_id=project)),'a'*64,actor))
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,'source:0','semantic.transfer','1','gpu','[]',%s,%s,%s)",(producer_task,tenant,producer_run,params['artifact_id'],'a'*64,Jsonb(dict(backend='cuda'))))
    db.execute('UPDATE photo.artifacts SET created_by_task_id=%s,produced_attempt=1,produced_fence=1 WHERE id=%s',(producer_task,binding['source']['artifact_id']))
    pixels = dict(artifact_id=str(uuid4()),sha256='9'*64)
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'test',%s,%s,128,'application/x-npy','v1',clock_timestamp(),'working_rgb',%s,1,1)",(pixels['artifact_id'],tenant,str(uuid4()),pixels['sha256'],producer_task))
    qualified = frozen_qualification(db,tenant,project,actor,operation=operation)
    document = db.execute('SELECT document_id FROM photo.v2_draft_uploads WHERE tenant_id=%s AND artifact_id=%s',(tenant,params['artifact_id'])).fetchone()[0]

    def object_pin(ref, contract, parent=None):
        bucket,key,version,size,media = db.execute('SELECT bucket,object_key,object_version,bytes,media_type FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(tenant,ref['artifact_id'])).fetchone()
        return dict(contract=contract,artifact_id=ref['artifact_id'],sha256=ref['sha256'],bucket=bucket,key=key,version=version,bytes=size,media_type=media,parent_id=parent['artifact_id'] if parent else None,parent_sha=parent['sha256'] if parent else None)
    source_pins = [object_pin(binding['source'],'WorkingImage'),object_pin(pixels,'WorkingPixels',binding['source'])]
    param_object = object_pin(params,'Params')
    params_pin = {key:param_object[key] for key in ['bucket','key','version','bytes','media_type','sha256']}
    report = object_pin(qualified['report'],'QualificationReport')
    report.pop('parent_id');report.pop('parent_sha');report.pop('media_type');report['backend']='cuda'
    files = [dict(Ref=dict(artifact_id=pin['artifact_id'],sha256=pin['sha256']),Contract=pin['contract'],Object=dict(Key=pin['key'],VersionID=pin['version'],SHA256=pin['sha256'],Bytes=pin['bytes'],ContentType=pin['media_type'])) for pin in [param_object]+source_pins]
    rows = sorted([dict(ref=file['Ref'],contract='NpyArray' if file['Contract']=='WorkingPixels' else file['Contract']) for file in files],key=lambda row:row['ref']['artifact_id'])
    expected = dict(Operation=operation,Version='1',Backend='cuda',ModelLockSHA=qualified['model_lock_sha'],RuntimeLockSHA=qualified['runtime_lock_sha'],DeviceProfileSHA=qualified['device_profile_sha'],DatasetSHA=qualified['dataset_sha'],IssuerID=actor,RequiredChecks=['quality'],CalibrationSHAs={qualified['calibration']['sha256']:{}},TestFixtureSHAs={'e'*64:{}},EvidenceSHAs={'f'*64:{}},LicenseCleared=True)
    db.execute('SET LOCAL ROLE photo_api')
    db.execute("SELECT set_config('app.tenant_id',%s,true)",(tenant,))
    policies = db.execute('SELECT photo.semantic_proposal_qualification_policy_inventory(%s,%s,%s,%s)',(tenant,project,actor,session)).fetchone()[0]
    db.execute('RESET ROLE')
    packet = dict(schema_version=1,tenant_id=tenant,project_id=project,actor_id=actor,session_id=session,operation=operation,unit_rgb=True,icc_sha='c'*64,binding=binding,source_pins=source_pins,expected=expected,qualification=report,params_document_id=str(document),params_pin=params_pin,files=files,policy_text=json.dumps(policies,sort_keys=True,separators=(',',':')),registration=dict(schema_version=1,params=params,transport=dict(rows=rows)))

    return packet,case,qualified


@pytest.mark.parametrize('operation', ['face.detect', 'mask.person'])
def test_native_source_authority_exact_then_revoked(db, operation):
    packet,case,qualified = seed_native_source_authority(db,operation)
    tenant,project,actor,session = [case[key] for key in ['Tenant','Project','Actor','Session']]
    binding,source_pins,params,params_pin,files = [packet[key] for key in ['binding','source_pins','registration','params_pin','files']]
    params = params['params']
    expected,report,document = packet['expected'],packet['qualification'],packet['params_document_id']
    policies = json.loads(packet['policy_text'])
    def lock(candidate=packet):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            result = db.execute('SELECT photo.lock_semantic_native_source_authority(%s,%s,%s,%s,%s)',(tenant,project,actor,session,Jsonb(candidate))).fetchone()[0]
            db.execute('RESET ROLE')
            return result
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.semantic_native_file_closure_matches(%s,%s,%s,%s,%s,%s)',tuple(Jsonb(value) for value in [binding,source_pins,params,params_pin,files,packet['registration']])).fetchone()[0], 'closure'
        assert db.execute('SELECT photo.lock_semantic_proposal_qualification_policy_snapshot(%s,%s,%s,%s,%s)',(tenant,project,actor,session,Jsonb(policies))).fetchone()[0], 'policy'
        function = 'face' if operation=='face.detect' else 'person'
        assert db.execute('SELECT photo.lock_semantic_proposal_'+function+'_qualification(%s,%s,%s,%s,%s,%s)',(tenant,project,actor,session,Jsonb(expected),Jsonb(report))).fetchone()[0], 'qualification'
        assert db.execute('SELECT photo.lock_semantic_proposal_source_evidence(%s,%s,%s,%s,%s,%s,%s,%s)',(tenant,project,actor,session,binding['asset_version_id'],binding['source']['artifact_id'],binding['source']['sha256'],Jsonb(source_pins))).fetchone()[0], 'source'
        assert db.execute('SELECT photo.lock_semantic_defect_version_params(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',(tenant,project,actor,session,binding['asset_version_id'],binding['source']['artifact_id'],binding['source']['sha256'],document,params['artifact_id'],params['sha256'],Jsonb(params_pin))).fetchone()[0], 'params'
        db.execute('RESET ROLE')
    assert lock()
    for mutation in ['tenant','session','icc','unit_rgb','backend','source_version','params_version','report_version','document','files','policy']:
        changed = deepcopy(packet)
        if mutation == 'tenant': changed['tenant_id']=str(uuid4())
        elif mutation == 'session': changed['session_id']=str(uuid4())
        elif mutation == 'icc': changed['icc_sha']='invalid'
        elif mutation == 'unit_rgb': changed['unit_rgb']=False
        elif mutation == 'backend': changed['expected']['Backend']='cpu'
        elif mutation == 'source_version': changed['source_pins'][1]['version']='foreign'
        elif mutation == 'params_version': changed['params_pin']['version']='foreign'
        elif mutation == 'report_version': changed['qualification']['version']='foreign'
        elif mutation == 'document': changed['params_document_id']=str(uuid4())
        elif mutation == 'files': changed['files'].pop()
        elif mutation == 'policy': changed['policy_text']='[]'
        assert not lock(changed), mutation
    db.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(qualified['policy_id'],))
    assert not lock()


@pytest.mark.parametrize('operation', ['face.detect', 'mask.person'])
def test_native_source_authority_holds_revocation_locks(database, operation):
    with psycopg.connect(**database) as setup:
        packet,case,qualified = seed_native_source_authority(setup,operation)
        setup.commit()
    tenant,project,actor,session = [case[key] for key in ['Tenant','Project','Actor','Session']]
    args = (tenant,project,actor,session,Jsonb(packet))
    query = 'SELECT photo.lock_semantic_native_source_authority(%s,%s,%s,%s,%s)'
    changes = [
        ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',qualified['policy_id']),
        ("UPDATE photo.model_qualifications SET status='failed' WHERE id=%s",qualified['qualification_id']),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',qualified['report']['artifact_id']),
        ("UPDATE photo.artifacts SET object_version='changed' WHERE id=%s",packet['source_pins'][1]['artifact_id']),
        ("UPDATE photo.v2_draft_uploads SET state='retired' WHERE document_id=%s",packet['params_document_id']),
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',session),
    ]
    with psycopg.connect(**database) as locked, psycopg.connect(**database) as writer:
        locked.execute('SET LOCAL ROLE photo_scheduler')
        locked.execute("SELECT set_config('app.tenant_id',%s,true)",(tenant,))
        assert locked.execute(query,args).fetchone()[0]
        for statement,value in changes:
            with pytest.raises(psycopg.errors.LockNotAvailable):
                with writer.transaction():
                    writer.execute("SET LOCAL lock_timeout='250ms'")
                    writer.execute(statement,(value,))
            assert locked.execute(query,args).fetchone()[0]
        locked.rollback()
        writer.execute(changes[0][0],(changes[0][1],))
        writer.commit()
        locked.execute('SET LOCAL ROLE photo_scheduler')
        locked.execute("SELECT set_config('app.tenant_id',%s,true)",(tenant,))
        assert not locked.execute(query,args).fetchone()[0]
        locked.rollback()
        assert writer.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND (graph_sealed OR status<>'draft')",(tenant,)).fetchone()[0] == 0
