"""Synthetic qualification + real original JSON/NPY + production Go bootstrap."""
from physical_go_child_evidence import run_evidenced_go
from pathlib import Path
from uuid import uuid4
from hashlib import sha256
from io import BytesIO
import json
import os
import subprocess
import pytest
import psycopg
from psycopg.types.json import Jsonb
from psycopg.conninfo import make_conninfo
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from test_v25_lens_objectstore import lens_objects
from test_v27_semantic_final_producer_gate import fixture
from test_v28_relight_depth import seed_depth_inputs
from test_v28_relight_person import seed_person_inputs
from test_v28_relight_manual import seed_manual_inputs
from semantic_qualification_fixture import frozen_qualification

ROOT=Path(__file__).resolve().parents[2]


@pytest.mark.parametrize('subject',['person','manual_shared','manual_external','manual_split'])
def test_actual_relight_bootstrap(database,lens_objects,tmp_path,subject,stage_only=False,transport_only=False,registered_only=False,admission=False,queued_only=False,admission_fault=None,submit_only=False,service_only=False):
    client,bucket=lens_objects
    def source_builder(artifact,original_sha):
        image=json.loads((ROOT/'backend/internal/review/testdata/working-images-python.json').read_text())[0]
        image['canvas'].update(width=5,height=5,source_sha256=original_sha,space_id='oriented')
        pixel=artifact('a'*64,role='working_rgb',media='application/octet-stream')
        image['pixels']=dict(artifact_id=str(pixel),sha256='a'*64)
        return canonical_bytes(image)
    def publish(aid,raw,role):
        media='application/octet-stream' if role in ('relative_depth','depth_invalid','mask_target','mask_protected') else 'image/png' if role.endswith('_png') else 'application/json'
        version=client.put_object(bucket,str(aid),BytesIO(raw),len(raw),content_type=media,metadata={'sha256':sha256(raw).hexdigest()}).version_id
        return bucket,str(aid),version
    with psycopg.connect(**database) as setup:
        tenant,*_,scope=fixture(setup,snapshot_authority=True,source_builder=source_builder,qualification_objects=(client,bucket))
        setup.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)',(tenant,))
        depth={};seed_depth_inputs(setup,tenant,scope,depth,objects=(client,bucket),physical=True,publish=publish)
        subject_ref=None
        if subject=='person':
            seed_person_inputs(setup,tenant,scope,{},objects=(client,bucket),publish=publish)
            subject_ref=scope['subject_ref']
        first={};seed_manual_inputs(setup,tenant,scope,first,physical=True,publish=publish,external=subject=='manual_external')
        if subject_ref is None:subject_ref=scope['subject_ref']
        if subject=='manual_split':seed_manual_inputs(setup,tenant,scope,{},physical=True,publish=publish)
        guard_ref=scope['protected_ref']
        calibration=json.loads((ROOT/'docs/implementation-v2/contracts/structural-fixtures.json').read_text())['fixtures']['Calibration']
        calibration.update(model_lock_sha='a'*64,dataset_sha='d'*64,qualified=True,loss_weights=[1/6]*6)
        calibration_raw=canonical_bytes(calibration);calibration_id=uuid4()
        object_bucket,key,version=publish(calibration_id,calibration_raw,'Calibration')
        setup.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,%s,%s,%s,%s,%s,%s,'application/json',clock_timestamp())",(calibration_id,tenant,object_bucket,key,version,sha256(calibration_raw).hexdigest(),len(calibration_raw)))
        calibration_ref=dict(artifact_id=str(calibration_id),sha256=sha256(calibration_raw).hexdigest())
        pin=frozen_qualification(setup,tenant,scope['project'],scope['actor'],objects=(client,bucket),calibration_ref=calibration_ref,operation='image.relight',backend='cpu')
        pin['policy_sha']=setup.execute('SELECT photo.v28_relight_policy_sha(%s)',(pin['policy_id'],)).fetchone()[0]
        report_locator=setup.execute('SELECT bucket,object_key,object_version FROM photo.artifacts WHERE id=%s',(pin['report']['artifact_id'],)).fetchone()
        response=client.get_object(report_locator[0],report_locator[1],version_id=report_locator[2])
        try:report_raw=response.read()
        finally:response.close();response.release_conn()
        run,workflow=uuid4(),uuid4()
        setup.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES(%s,%s,'v2','draft',%s,%s,repeat('b',64),%s,'fail_fast',false)",(run,tenant,scope['snapshot'],Jsonb({'project_id':str(scope['project'])}),scope['actor']))
        setup.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'preview','v2.2',repeat('b',64))",(workflow,tenant,scope['project'],run,scope['snapshot']))
    source=scope['source_body'].decode()
    params=dict(schema_version=1,base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash=canonical_hash(json.loads(source)['canvas'])),depth=scope['depth_ref'],subject=subject_ref,protected=guard_ref,calibration=pin['calibration'],lights=[dict(x=.5,y=.5,radius=.2,ev=1)],strength=.7)
    seed_file=tmp_path/'bootstrap.json'
    seed_file.write_text(json.dumps(dict(tenant=str(tenant),project=str(scope['project']),snapshot=str(scope['snapshot']),run=str(run),workflow=str(workflow),actor=str(scope['actor']),session=str(scope['session']),subject='person' if subject=='person' else 'manual',manual_count=2 if subject=='manual_split' else 1,source=source,params=canonical_bytes(params).decode(),pin=pin,report=report_raw.decode(),calibration=calibration_raw.decode())))
    credentials=client._provider.retrieve()
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_TEST_RELIGHT_BOOTSTRAP_SEED=str(seed_file),PHOTO_TEST_MINIO_URL='http://'+client._base_url.host,PHOTO_TEST_MINIO_BUCKET=bucket,PHOTO_TEST_MINIO_ACCESS=credentials.access_key,PHOTO_TEST_MINIO_SECRET=credentials.secret_key,GOCACHE=str(ROOT/'work/go-cache'))
    if service_only:env['PHOTO_TEST_RELIGHT_SERVICE_ONLY']='1'
    if submit_only:env['PHOTO_TEST_RELIGHT_SUBMIT_ONLY']='1'
    if admission_fault:env['PHOTO_TEST_RELIGHT_ADMISSION_FAULT']=admission_fault
    if queued_only:env['PHOTO_TEST_RELIGHT_QUEUED_ONLY']='1'
    if admission:env['PHOTO_TEST_RELIGHT_ADMISSION']='1'
    if registered_only:env['PHOTO_TEST_RELIGHT_REGISTERED_ONLY']='1'
    if stage_only:env['PHOTO_TEST_RELIGHT_STAGE_ONLY']='1'
    if transport_only:env['PHOTO_TEST_RELIGHT_TRANSPORT_ONLY']='1'
    result=run_evidenced_go(['go','test','./internal/v2portrait','-run','^TestIntegrationRelightBootstrap$','-count=1','-timeout=90s','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr
    if stage_only or transport_only or registered_only or queued_only:return json.loads(seed_file.read_text()),env


@pytest.mark.migration_head(1002)
def test_relight_params_upgrade_preserves_existing_state(database,lens_objects,tmp_path):
    with psycopg.connect(**database) as setup:
        tenant,*_,scope=fixture(setup,snapshot_authority=True)
        existing=setup.execute('SELECT count(*) FROM photo.runs').fetchone()[0]
        setup.commit()  # upgrade runs against committed pre-existing state
        for name in ['01003_v28_relight_params_documents.sql','01004_v28_relight_task_stage.sql','01005_v28_relight_transport_documents.sql','01006_v28_relight_transport_registration.sql','01007_v28_relight_registered_transport.sql']:
            migration=ROOT/'backend/db/migrations'/name
            setup.execute(migration.read_text().split('-- +goose Down')[0])
        assert setup.execute('SELECT count(*) FROM photo.runs').fetchone()[0]==existing
        assert setup.execute('SELECT id FROM photo.projects WHERE id=%s',(scope['project'],)).fetchone() is not None
        # Preserve old policy/report rows; disable their admission before the
        # second independent synthetic deployment policy is installed.
        setup.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false')
    test_actual_relight_bootstrap(database,lens_objects,tmp_path,'manual_external')


@pytest.mark.migration_head(1003)
def test_relight_stage_upgrade_preserves_saved_intent(database,lens_objects,tmp_path):
    from test_v28_relight_draft_intent import check_relight_intent
    with psycopg.connect(**database) as setup:
        saved=check_relight_intent(setup,fixture(setup,snapshot_authority=True),verify_manual=True)
        setup.commit()
        for name in ['01004_v28_relight_task_stage.sql','01005_v28_relight_transport_documents.sql','01006_v28_relight_transport_registration.sql','01007_v28_relight_registered_transport.sql']:
            migration=ROOT/'backend/db/migrations'/name
            setup.execute(migration.read_text().split('-- +goose Down')[0])
        assert setup.execute('SELECT v28_relight_intent,v28_relight_descriptor_jcs FROM photo.runs WHERE id=%s',(saved['run'],)).fetchone()==(saved['request'],None)
        assert setup.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(saved['run'],)).fetchone()[0]==0
        setup.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false')
    test_actual_relight_bootstrap(database,lens_objects,tmp_path,'person')


@pytest.mark.migration_head(1004)
def test_relight_transport_upgrade_resumes_committed_blocked_task(database,lens_objects,tmp_path):
    seed,env=test_actual_relight_bootstrap(database,lens_objects,tmp_path,'manual_shared',stage_only=True)
    with psycopg.connect(**database) as setup:
        before=setup.execute('SELECT v28_relight_intent,v28_relight_descriptor_jcs FROM photo.runs WHERE id=%s',(seed['run'],)).fetchone()
        assert before[0] is not None and before[1] is not None
        assert setup.execute("SELECT state,attempt FROM photo.tasks WHERE run_id=%s",(seed['run'],)).fetchone()==('blocked',0)
        setup.commit()
        migration=ROOT/'backend/db/migrations/01005_v28_relight_transport_documents.sql'
        setup.execute(migration.read_text().split('-- +goose Down')[0])
        assert setup.execute('SELECT v28_relight_intent,v28_relight_descriptor_jcs FROM photo.runs WHERE id=%s',(seed['run'],)).fetchone()==before
    env.pop('PHOTO_TEST_RELIGHT_STAGE_ONLY',None)
    env['PHOTO_TEST_RELIGHT_RESUME_ONLY']='1'
    result=run_evidenced_go(['go','test','./internal/v2portrait','-run','^TestIntegrationRelightBootstrap$','-count=1','-timeout=90s','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr


@pytest.mark.migration_head(1005)
def test_relight_registration_upgrade_preserves_published_transport(database,lens_objects,tmp_path):
    seed,env=test_actual_relight_bootstrap(database,lens_objects,tmp_path,'manual_external',transport_only=True)
    with psycopg.connect(**database) as setup:
        before=setup.execute("SELECT artifact_id,sha256,object_version FROM photo.backend_documents WHERE run_id=%s ORDER BY purpose",(seed['run'],)).fetchall()
        assert len(before)==3
        assert setup.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE task_id=(SELECT id FROM photo.tasks WHERE run_id=%s)',(seed['run'],)).fetchone()[0]==0
        setup.commit()
        migration=ROOT/'backend/db/migrations/01006_v28_relight_transport_registration.sql'
        setup.execute(migration.read_text().split('-- +goose Down')[0])
        assert setup.execute("SELECT artifact_id,sha256,object_version FROM photo.backend_documents WHERE run_id=%s ORDER BY purpose",(seed['run'],)).fetchall()==before
    env.pop('PHOTO_TEST_RELIGHT_TRANSPORT_ONLY',None)
    env['PHOTO_TEST_RELIGHT_RESUME_ONLY']='1'
    env['PHOTO_TEST_RELIGHT_RESUME_REGISTER_ONLY']='1'
    result=run_evidenced_go(['go','test','./internal/v2portrait','-run','^TestIntegrationRelightBootstrap$','-count=1','-timeout=90s','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr


@pytest.mark.migration_head(1006)
def test_relight_recovery_upgrade_preserves_registered_state(database,lens_objects,tmp_path):
    seed,env=test_actual_relight_bootstrap(database,lens_objects,tmp_path,'manual_split',registered_only=True)
    with psycopg.connect(**database) as setup:
        query="SELECT ordinal,artifact_id,sha256 FROM photo.v2_task_inputs WHERE task_id=(SELECT id FROM photo.tasks WHERE run_id=%s) ORDER BY ordinal"
        before=setup.execute(query,(seed['run'],)).fetchall()
        assert len(before)>11
        documents=setup.execute("SELECT artifact_id,sha256,object_version FROM photo.backend_documents WHERE run_id=%s ORDER BY purpose",(seed['run'],)).fetchall()
        setup.commit()
        migration=ROOT/'backend/db/migrations/01007_v28_relight_registered_transport.sql'
        setup.execute(migration.read_text().split('-- +goose Down')[0])
        assert setup.execute(query,(seed['run'],)).fetchall()==before
    env.pop('PHOTO_TEST_RELIGHT_REGISTERED_ONLY',None)
    env['PHOTO_TEST_RELIGHT_RESUME_ONLY']='1'
    env['PHOTO_TEST_RELIGHT_RESUME_RECOVERY_ONLY']='1'
    result=run_evidenced_go(['go','test','./internal/v2portrait','-run','^TestIntegrationRelightBootstrap$','-count=1','-timeout=90s','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr
    with psycopg.connect(**database) as setup:
        assert setup.execute(query,(seed['run'],)).fetchall()==before
        assert setup.execute("SELECT artifact_id,sha256,object_version FROM photo.backend_documents WHERE run_id=%s ORDER BY purpose",(seed['run'],)).fetchall()==documents


@pytest.mark.parametrize('subject',['person','manual_shared','manual_external','manual_split'])
def test_relight_atomic_admission(database,lens_objects,tmp_path,subject):
    test_actual_relight_bootstrap(database,lens_objects,tmp_path,subject,admission=True)


@pytest.mark.migration_head(1008)
def test_relight_queued_receipt_upgrade_and_new_process(database,lens_objects,tmp_path):
    seed,env=test_actual_relight_bootstrap(database,lens_objects,tmp_path,'manual_external',admission=True,queued_only=True)
    def snapshot(db):
        return (
            db.execute("SELECT graph_sealed,status,v28_relight_queued_receipt FROM photo.runs WHERE id=%s",(seed['run'],)).fetchone(),
            db.execute("SELECT state,attempt,input_digest,inputs,execution_profile FROM photo.tasks WHERE run_id=%s",(seed['run'],)).fetchall(),
            db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s",(seed['tenant'],)).fetchone(),
            db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s",(seed['tenant'],)).fetchone(),
            db.execute("SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s",(seed['tenant'],)).fetchone(),
        )
    with psycopg.connect(**database) as setup:
        before=snapshot(setup)
        assert before[0][0] and before[0][1]=='queued' and before[0][2]['state']=='queued'
        assert setup.execute("SELECT count(*) FROM photo.run_events WHERE run_id=%s AND type='v2.run.queued'",(seed['run'],)).fetchone()[0]==1
        setup.commit()
        migration=ROOT/'backend/db/migrations/01009_v28_relight_queued_receipt.sql'
        setup.execute(migration.read_text().split('-- +goose Down')[0])
        live_migration=ROOT/'backend/db/migrations/02422_relight_legacy_receipt_live_authority.sql'
        setup.execute(live_migration.read_text().split('-- +goose Down')[0])
        assert snapshot(setup)==before
    env.pop('PHOTO_TEST_RELIGHT_QUEUED_ONLY',None)
    env.pop('PHOTO_TEST_RELIGHT_ADMISSION',None)
    env['PHOTO_TEST_RELIGHT_RECEIPT_ONLY']='1'
    result=run_evidenced_go(['go','test','./internal/v2portrait','-run','^TestIntegrationRelightBootstrap$','-count=1','-timeout=90s','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr
    with psycopg.connect(**database) as setup:
        assert snapshot(setup)==before


@pytest.mark.parametrize('fault',['unknown_commit','concurrent','cursor_exhausted'])
def test_relight_admission_commit_faults(database,lens_objects,tmp_path,fault):
    test_actual_relight_bootstrap(database,lens_objects,tmp_path,'manual_external',admission=True,admission_fault=fault)


@pytest.mark.migration_head(1007)
def test_relight_admission_upgrade_preserves_registered_inventory(database,lens_objects,tmp_path):
    seed,env=test_actual_relight_bootstrap(database,lens_objects,tmp_path,'manual_split',registered_only=True)
    query="SELECT ordinal,artifact_id,sha256,contract FROM photo.v2_task_inputs WHERE task_id=(SELECT id FROM photo.tasks WHERE run_id=%s) ORDER BY ordinal"
    documents="SELECT artifact_id,sha256,object_version FROM photo.backend_documents WHERE run_id=%s ORDER BY purpose"
    with psycopg.connect(**database) as setup:
        before=setup.execute(query,(seed['run'],)).fetchall()
        objects=setup.execute(documents,(seed['run'],)).fetchall()
        quota=setup.execute("SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s",(seed['tenant'],)).fetchone()
        setup.commit()
        migration=ROOT/'backend/db/migrations/01008_v28_relight_graph_admission.sql'
        setup.execute(migration.read_text().split('-- +goose Down')[0])
        assert setup.execute(query,(seed['run'],)).fetchall()==before
        assert setup.execute("SELECT v2_enabled FROM photo.operation_registry WHERE operation='image.relight' AND version='1'").fetchone()==(False,)
        assert setup.execute("SELECT graph_sealed,status,v28_relight_queued_receipt FROM photo.runs WHERE id=%s",(seed['run'],)).fetchone()==(False,'draft',None)
    env.pop('PHOTO_TEST_RELIGHT_REGISTERED_ONLY',None)
    env['PHOTO_TEST_RELIGHT_RESUME_ONLY']='1'
    env['PHOTO_TEST_RELIGHT_RESUME_ADMISSION_ONLY']='1'
    result=run_evidenced_go(['go','test','./internal/v2portrait','-run','^TestIntegrationRelightBootstrap$','-count=1','-timeout=90s','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr
    with psycopg.connect(**database) as setup:
        assert setup.execute(query,(seed['run'],)).fetchall()==before
        assert setup.execute(documents,(seed['run'],)).fetchall()==objects
        assert setup.execute("SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s",(seed['tenant'],)).fetchone()==quota
        sealed,status,receipt=setup.execute("SELECT graph_sealed,status,v28_relight_queued_receipt FROM photo.runs WHERE id=%s",(seed['run'],)).fetchone()
        assert sealed and status=='queued' and receipt['state']=='queued'
        assert setup.execute("SELECT count(*) FROM photo.run_events WHERE run_id=%s AND type='v2.run.queued'",(seed['run'],)).fetchone()[0]==1


@pytest.mark.parametrize('subject',['person','manual_shared','manual_external','manual_split'])
def test_relight_complete_server_submission(database,lens_objects,tmp_path,subject):
    test_actual_relight_bootstrap(database,lens_objects,tmp_path,subject,submit_only=True)


def test_relight_server_submission_recovers_unknown_commit(database,lens_objects,tmp_path):
    test_actual_relight_bootstrap(database,lens_objects,tmp_path,'manual_external',submit_only=True,admission_fault='unknown_commit')


@pytest.mark.migration_head(1009)
def test_relight_resolver_upgrade_preserves_registered_state(database,lens_objects,tmp_path):
    seed,env=test_actual_relight_bootstrap(database,lens_objects,tmp_path,'manual_split',registered_only=True)
    def snapshot(db):
        return (
            db.execute("SELECT status,graph_sealed,v28_relight_intent,v28_relight_descriptor_jcs FROM photo.runs WHERE id=%s",(seed['run'],)).fetchone(),
            db.execute("SELECT ordinal,artifact_id,sha256,contract FROM photo.v2_task_inputs WHERE task_id=(SELECT id FROM photo.tasks WHERE run_id=%s) ORDER BY ordinal",(seed['run'],)).fetchall(),
            db.execute("SELECT artifact_id,sha256,object_version FROM photo.backend_documents WHERE run_id=%s ORDER BY purpose",(seed['run'],)).fetchall(),
            db.execute("SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s",(seed['tenant'],)).fetchone(),
        )
    with psycopg.connect(**database) as setup:
        before=snapshot(setup)
        setup.commit()
        for name in ['01010_v28_relight_qualification_sources.sql','01011_v28_relight_source_document.sql']:
            setup.execute((ROOT/'backend/db/migrations'/name).read_text().split('-- +goose Down')[0])
        assert snapshot(setup)==before
    env.pop('PHOTO_TEST_RELIGHT_REGISTERED_ONLY',None)
    env['PHOTO_TEST_RELIGHT_RESOLVERS_ONLY']='1'
    result=run_evidenced_go(['go','test','./internal/v2portrait','-run','^TestIntegrationRelightBootstrap$','-count=1','-timeout=90s','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr
    with psycopg.connect(**database) as setup:
        assert snapshot(setup)==before


@pytest.mark.parametrize('subject',['person','manual_shared','manual_external','manual_split'])
def test_relight_project_bound_submission_service(database,lens_objects,tmp_path,subject):
    test_actual_relight_bootstrap(database,lens_objects,tmp_path,subject,submit_only=True,service_only=True)
