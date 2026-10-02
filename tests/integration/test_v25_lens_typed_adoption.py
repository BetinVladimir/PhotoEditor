"""Real typed lens output/adoption IO on synthetic, unqualified frames.

Metadata setup bypasses worker dispatch/gateway, so this is not qualification.
"""
from hashlib import sha256
from io import BytesIO
from pathlib import Path
from uuid import UUID,uuid4,uuid5
import shutil
import os
import json
import subprocess
from psycopg.conninfo import make_conninfo

import numpy as np
import psycopg
from psycopg.types.json import Jsonb
import pytest

from test_database import tenant
from test_v25_lens_objectstore import lens_objects
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.operations.optical.contracts import LensProfile,OpticalLensParams
from photo_worker.operations.optical.prepare import prepare_working_lens
from photo_worker.operations.optical.result import stage_lens_result
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.adoption_service import AdoptionCoordinator

ROOT=Path(__file__).resolve().parents[2]


@pytest.mark.parametrize('strength,revoke',[(0,False),(1,False),(1,True)])
def test_typed_lens_adoption_and_authority_change(lens_objects,database,strength,revoke):
    client,bucket=lens_objects
    run=uuid4();workflow=uuid5(run,'workflow');task=uuid5(workflow,'optical.lens:0');worker=uuid4()
    published={}
    def put(payload,media,role=None):
        ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(payload).hexdigest())
        key=('outputs/'+role if role else 'inputs')+'/'+str(ref.artifact_id)
        version=client.put_object(bucket,key,BytesIO(payload),len(payload),content_type=media,metadata={'sha256':ref.sha256}).version_id
        assert version and version!='null'
        published[ref.artifact_id]=(ref,payload,media,key,version,role)
        return ref
    def document(value,role=None):return put(canonical_bytes(value),'application/json',role)
    def array(value):
        stream=BytesIO();np.save(stream,value,allow_pickle=False)
        return put(stream.getvalue(),'application/x-npy')
    # Deliberately synthetic HDR data, not a photographic calibration fixture.
    rgb=np.full((5,5,3),[-.2,4.,.8],dtype='<f4');alpha=np.ones((5,5),dtype='<f4')
    original=put(b'synthetic original fixture','image/jpeg')
    source=WorkingImage(pixels=array(rgb),alpha=None,dtype='float32',
        canvas=Canvas(width=5,height=5,orientation=1,space_id='source',source_sha256=original.sha256),
        color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),
        transform_chain=TransformChain.create([]))
    source_ref=document(source);version_id=uuid4()
    binding=VersionBinding(asset_version_id=version_id,source=source_ref,canvas_hash=canonical_hash(source.canvas))
    qualification=document({'synthetic':True})
    lens=LensProfile(schema_version=1,camera_model='Synthetic',lens_model='Synthetic',focal_mm=50,
        aperture=2,cx=.5,cy=.5,k=[.2,0,0],p=[0,0],vignette=[0,0,0],ca_red_scale=1,
        ca_blue_scale=1,qualification=qualification)
    lens_ref=document(lens)
    params=OpticalLensParams(schema_version=1,base=binding,strength=strength,profile=lens_ref,focal_mm=50,aperture=2)
    params_ref=document(params)
    original_binding={'binding':binding.model_dump(mode='json'),'original':original.model_dump(mode='json'),'suffix':'.jpg'}
    original_binding_ref=document(original_binding)
    profile={'backend':'cpu','renderer_version':'v2.2','resource_estimate':{'ram_bytes':536872412,
        'scratch_bytes':67110364,'output_bytes':33555132,'vram_bytes':0,'pixel_count':25}}
    scratch_root=ROOT/'.runtime/v25-typed-adoption'/uuid4().hex
    scratch_root.mkdir(parents=True)
    try:
        with psycopg.connect(**database,autocommit=True) as admin:
            def record(ref):
                ref,payload,media,key,obj_version,role=published[ref.artifact_id]
                admin.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp(),%s,%s,%s,%s)",
                    (ref.artifact_id,tid,bucket,key,ref.sha256,len(payload),media,obj_version,role,task if role else None,1 if role else None,1 if role else None))
                if role:
                    admin.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES(%s,%s,1,1,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,'ready',%s)",
                        (tid,task,worker,'d'*64,uuid4(),ref.artifact_id,role,bucket,key,ref.sha256,len(payload),media,obj_version))
            with admin.transaction():
                tid,actor=tenant(admin,str(uuid4())+'@example.test')
                admin.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','100000000') WHERE id=%s",(tid,))
                admin.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING',(tid,))
                project=admin.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Typed lens') RETURNING id",(tid,)).fetchone()[0]
                session=admin.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') RETURNING id",(actor,uuid4().bytes)).fetchone()[0]
                for ref,*_ in list(published.values()):record(ref)
                # A source WorkingImage is a catalog input, not a staged output.
                admin.execute("UPDATE photo.artifacts SET output_role='working_image' WHERE id=%s",(source_ref.artifact_id,))
                asset=admin.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tid,project,original.artifact_id)).fetchone()[0]
                admin.execute("INSERT INTO photo.asset_versions(id,tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,%s,'{}','accepted')",(version_id,tid,asset,source_ref.artifact_id,source_ref.sha256))
                snapshot=admin.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES(%s,'v2','user',%s,%s,1,clock_timestamp()+interval '1 hour') RETURNING id",(tid,actor,'e'*64)).fetchone()[0]
                admin.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,0,%s,%s,%s,'{}')",(tid,snapshot,version_id,source_ref.artifact_id,source_ref.sha256))
                admin.execute('UPDATE photo.snapshots SET expires_at=NULL WHERE id=%s',(snapshot,))
                node={'task_id':str(task),'profile':profile,'params':params.model_dump(mode='json'),'params_sha':params_ref.sha256,
                    'params_jcs':canonical_bytes(params).decode(),'original':original_binding,'original_sha':original_binding_ref.sha256,
                    'original_jcs':canonical_bytes(original_binding).decode()}
                config={'project_id':str(project),'input_snapshot_id':str(snapshot),'v25_lens':{'auth_session_id':str(session),'nodes':[node]}}
                admin.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'v2','running',%s,%s,%s,%s,'fail_fast')",(run,tid,snapshot,Jsonb(config),canonical_hash(config),actor))
                admin.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'preview','v2.2',%s)",(workflow,tid,project,run,snapshot,'f'*64))
                admin.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,attempt,fencing_token,execution_profile) VALUES(%s,%s,%s,'optical.lens:0','optical.lens','1','cpu','[]',%s,%s,1,1,%s)",(task,tid,run,params_ref.artifact_id,'a'*64,Jsonb(profile)))
                # Original arrays have a distinct producer and must prove the
                # same immutable producer attempt/fence as their master.
                origin_task=uuid4()
                origin_run=uuid4()
                admin.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'v2','running',%s,'{}',%s,%s,'fail_fast')",(origin_run,tid,snapshot,'8'*64,actor))
                admin.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,attempt,fencing_token,execution_profile) VALUES(%s,%s,%s,'fixture.source:0','fixture.source','1','cpu','[]',%s,%s,1,1,%s)",(origin_task,tid,origin_run,params_ref.artifact_id,'9'*64,Jsonb(profile)))
                admin.execute("UPDATE photo.artifacts SET created_by_task_id=%s,produced_attempt=1,produced_fence=1 WHERE id IN(%s,%s)",(origin_task,source_ref.artifact_id,source.pixels.artifact_id))
                admin.execute("UPDATE photo.artifacts SET output_role='working_rgb' WHERE id=%s",(source.pixels.artifact_id,))
                admin.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),'cpu',%s)",(tid,task,worker,Jsonb(profile)))
                for ordinal,(ref,*_) in enumerate(list(published.values())):
                    admin.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,%s,%s)",(tid,project,task,ref.artifact_id,ref.sha256,'WorkingImage' if ref==source_ref else 'declared_input',ordinal))
            class Staging:
                def stage(self,path,reservation,role,media):
                    reservation.validate_file(path);ref=put(path.read_bytes(),media,role);record(ref);return ref
                def stage_contract(self,value,role):
                    ref=document(value,role);record(ref);return ref
            with ScratchSpace(scratch_root,tid,task,1,65536,min_free_bytes=0) as scratch:
                with prepare_working_lens(lens,rgb,alpha,source.color,strength,scratch) as files:
                    staged=stage_lens_result(Staging(),source_ref,source,binding,files,strength=strength)
            result=admin.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,review_required,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s,%s) RETURNING id",(tid,task,Jsonb([staged.document.model_dump(mode='json')]),staged.review_required,'b'*64)).fetchone()[0]
            admin.execute("UPDATE photo.tasks SET canonical_result_id=%s,state='succeeded' WHERE id=%s",(result,task))
            if revoke:
                admin.execute('UPDATE photo.runs SET graph_sealed=true WHERE id=%s',(run,))
            class Objects:
                revoked=False
                def get_object(self,bucket_name,key,*,version_id):
                    response=client.get_object(bucket_name,key,version_id=version_id)
                    if revoke and '/pixels/' in key and not self.revoked:
                        admin.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(session,))
                        self.revoked=True
                    return response
            with psycopg.connect(**database,autocommit=True) as read,psycopg.connect(**database) as write:
                read.execute('SET ROLE photo_scheduler');write.execute('SET ROLE photo_scheduler');write.commit()
                coordinator=AdoptionCoordinator(read,write,Objects(),bucket)
                if revoke:
                    from photo_worker.v2.service import Adopter
                    service=Adopter(read,write,coordinator.objects,bucket)
                    counts=service.run_once()
                    assert counts['rejected']==1 and counts['adopted']==0 and counts['retry']==0
                    assert service.run_once()['rejected']==0
                else:
                    assert coordinator.adopt(tid,task) is True
                    assert coordinator.adopt(tid,task) is False
                    admin.execute("SELECT set_config('app.tenant_id',%s,false)",(str(tid),))
                    admin.execute('SET ROLE photo_api')
                    probe='SELECT * FROM photo.v25_adopted_lens_binding(%s,%s,%s,%s,%s,%s)'
                    proof=admin.execute(probe,(tid,project,version_id,task,actor,session)).fetchone()
                    assert proof==(result,staged.document.artifact_id,staged.document.sha256,source_ref.artifact_id,source_ref.sha256,binding.canvas_hash)
                    assert admin.execute(probe,(tid,project,uuid4(),task,actor,session)).fetchone() is None
                    assert admin.execute(probe,(tid,uuid4(),version_id,task,actor,session)).fetchone() is None
                    child_probe='SELECT * FROM photo.v25_adopted_lens_child_object(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)'
                    doc=json.loads(published[staged.document.artifact_id][1])
                    for field,contract in [('report','ImageReport'),('changed_mask','MaskRef')]:
                        child=doc[field];entry=published[UUID(child['artifact_id'])]
                        args=(tid,project,version_id,task,actor,session,staged.document.artifact_id,staged.document.sha256,child['artifact_id'],child['sha256'],'$/'+field,contract)
                        found=admin.execute(child_probe,args).fetchone()
                        assert found==(bucket,entry[3],entry[4],len(entry[1]),entry[2])
                        assert admin.execute(child_probe,(*args[:10],'$/wrong',contract)).fetchone() is None
                        assert admin.execute(child_probe,(*args[:11],'WorkingImage')).fetchone() is None
                    mask_doc=json.loads(published[UUID(doc['changed_mask']['artifact_id'])][1])
                    mask_array=mask_doc['artifact']
                    array_args=(tid,project,version_id,task,actor,session,staged.document.artifact_id,staged.document.sha256,mask_array['artifact_id'],mask_array['sha256'],'$/changed_mask/artifact','MaskPixels')
                    assert admin.execute(child_probe,array_args).fetchone() is not None
                    if strength:
                        image_doc=json.loads(published[UUID(doc['working_image']['artifact_id'])][1])
                        pixels=image_doc['pixels']
                        pixel_args=(tid,project,version_id,task,actor,session,staged.document.artifact_id,staged.document.sha256,pixels['artifact_id'],pixels['sha256'],'$/working_image/pixels','WorkingPixels')
                        assert admin.execute(child_probe,pixel_args).fetchone() is not None
                        field=mask_doc['transform_chain']['steps'][0]['inverse_field']
                        field_args=(*pixel_args[:8],field['artifact_id'],field['sha256'],'$/changed_mask/transform_chain/steps/*/inverse_field','DenseWarpField')
                        assert admin.execute(child_probe,field_args).fetchone() is not None
                        assert admin.execute(child_probe,(*field_args[:10],'$/changed_mask/transform_chain/steps/0/inverse_field','DenseWarpField')).fetchone() is None
                    admin.execute('RESET ROLE')
                    def check_go_loader(revoked):
                        proposal_probe='SELECT photo.v25_layer_lens_proposal_scope(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)'
                        proposal_args=(tid,project,version_id,actor,session,staged.document.artifact_id,staged.document.sha256,source_ref.artifact_id,source_ref.sha256,binding.canvas_hash)
                        admin.execute("SELECT set_config('app.tenant_id','',false)")
                        admin.execute('SET ROLE photo_scheduler')
                        try:
                            assert admin.execute(proposal_probe,proposal_args).fetchone()[0] is (not revoked)
                            if not revoked:
                                for index,value in [(0,uuid4()),(1,uuid4()),(2,uuid4()),(3,uuid4()),(4,uuid4()),(5,uuid4()),(6,'0'*64),(7,uuid4()),(8,'0'*64),(9,'0'*64)]:
                                    wrong=list(proposal_args);wrong[index]=value
                                    assert admin.execute(proposal_probe,wrong).fetchone()[0] is False,index
                        finally:admin.execute('RESET ROLE')
                        case=dict(Tenant=str(tid),Project=str(project),Snapshot=str(snapshot),Version=str(version_id),Producer=str(task),Actor=str(actor),Session=str(session),ResultID=str(result),Root=staged.document.model_dump(mode='json'),Base=binding.model_dump(mode='json'),Revoked=revoked)
                        _,payload,media,key,obj_version,_=published[staged.document.artifact_id]
                        case['ExpectedImageResult']=json.loads(payload)
                        if strength:
                            case['WarpField']=mask_doc['transform_chain']['steps'][0]['inverse_field']
                        case.update(Bucket=bucket,Object=dict(Key=key,VersionID=obj_version,Bytes=len(payload),ContentType=media,SHA256=staged.document.sha256))
                        env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_ADOPTED_LENS_CASE=json.dumps(case),PHOTO_ADOPTED_ROOT_STORE=json.dumps(client._photo_test_config),GOCACHE='/private/tmp/photoeditor-go-cache')
                        checked=subprocess.run(['go','test','./internal/v2render','-run','^TestIntegrationAdoptedLensSource$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=45)
                        assert checked.returncode==0,checked.stdout+checked.stderr
                    if strength==1:
                        # A newer object version must not replace the SQL-pinned root.
                        root_key=published[staged.document.artifact_id][3]
                        newer=b'not the pinned ImageResult'
                        client.put_object(bucket,root_key,BytesIO(newer),len(newer),content_type='application/json')
                    check_go_loader(False)
                    user_uploads=admin.execute("SELECT bytes,object_version FROM photo.v2_draft_uploads WHERE tenant_id=%s AND state='ready'",(tid,)).fetchall()
                    if strength==0:
                        user_size=len(published[UUID(doc['changed_mask']['artifact_id'])][1])
                        assert len(user_uploads)==4 and any(row[0]==user_size for row in user_uploads)
                        assert all(row[1] not in ('','null',None) for row in user_uploads)
                        backend_bytes=admin.execute("SELECT coalesce(sum(a.bytes),0) FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id WHERE d.tenant_id=%s AND d.purpose LIKE 'v2_input_%%'",(tid,)).fetchone()[0]
                        output_reserved=admin.execute("SELECT coalesce(sum(bytes),0) FROM photo.quota_reservations WHERE tenant_id=%s AND state='reserved'",(tid,)).fetchone()[0]
                        assert admin.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()==(sum(row[0] for row in user_uploads)+backend_bytes,output_reserved)
                        composition=admin.execute("SELECT t.run_id,t.id,t.params_artifact_id,a.sha256,t.input_digest,t.execution_profile FROM photo.tasks t JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id WHERE t.tenant_id=%s AND t.operation='layer.compose'",(tid,)).fetchall()
                        assert len(composition)==1
                        shell=composition[0]
                        from test_v25_layer_worker_admission import assert_live_composition_admission
                        assert_live_composition_admission(admin,tid,shell,client,bucket,scratch_root,database)
                        count=int(shell[5]['v2_entry_count'])
                        registered=admin.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s',(tid,shell[1])).fetchone()[0]
                        assert registered==count+(count+511)//512+1
                        assert admin.execute('SELECT state,attempt FROM photo.tasks WHERE id=%s',(shell[1],)).fetchone()==('blocked',0)
                        assert admin.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s',(shell[0],)).fetchone()==('draft',False)
                        shell_args=(tid,shell[0],shell[1],0,shell[2],shell[3],shell[4],Jsonb({k:v for k,v in shell[5].items() if k not in ('contracts_sha','v2_input_manifest','v2_entry_count','v2_entries_sha')}))
                        shell_sql='SELECT photo.stage_v25_lens_user_masked_composition_task(%s,%s,%s,%s,%s,%s,%s,%s)'
                        with admin.transaction():
                            admin.execute('SET LOCAL ROLE photo_scheduler')
                            assert admin.execute(shell_sql,shell_args).fetchone()[0] is True
                        prior_expiry=admin.execute('SELECT expires_at FROM photo.artifacts WHERE id=%s',(staged.document.artifact_id,)).fetchone()[0]
                        admin.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 hour' WHERE id=%s",(staged.document.artifact_id,))
                        with admin.transaction():
                            admin.execute('SET LOCAL ROLE photo_scheduler')
                            assert admin.execute(shell_sql,shell_args).fetchone()[0] is False
                        admin.execute('UPDATE photo.artifacts SET expires_at=%s WHERE id=%s',(prior_expiry,staged.document.artifact_id))

                    else:
                        assert user_uploads==[]
                    # A later revoke cannot turn an already-adopted replay into rejection.
                    admin.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(session,))
                    if strength==0:
                        with admin.transaction():
                            admin.execute('SET LOCAL ROLE photo_scheduler')
                            assert admin.execute(shell_sql,shell_args).fetchone()[0] is False
                    assert coordinator.adopt(tid,task) is False
                    admin.execute('SET ROLE photo_api')
                    assert admin.execute(probe,(tid,project,version_id,task,actor,session)).fetchone() is None
                    admin.execute('RESET ROLE')
                    check_go_loader(True)
            links=admin.execute('SELECT count(*) FROM photo.v2_artifact_links WHERE tenant_id=%s AND workflow_id=%s',(tid,workflow)).fetchone()[0]
            events=admin.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.task.adopted'",(tid,run)).fetchone()[0]
            # Identity source appears at both WorkingImage and provenance paths.
            assert links==(0 if revoke else len(staged.outputs)+(2 if strength==0 else 1))
            distinct=admin.execute('SELECT count(DISTINCT artifact_id) FROM photo.v2_artifact_links WHERE tenant_id=%s AND workflow_id=%s',(tid,workflow)).fetchone()[0]
            assert distinct==(0 if revoke else len(staged.outputs)+1)
            assert events==(0 if revoke else 1)
            messages=admin.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND routing_key='v2.task.adopted' AND payload->>'task_id'=%s",(tid,str(task))).fetchone()[0]
            assert messages==(0 if revoke else 1)
            rejected=admin.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.adoption.failed' AND payload->>'code'='LENS_SCOPE_STALE'",(tid,run)).fetchone()[0]
            assert rejected==(1 if revoke else 0)
    finally:
        shutil.rmtree(scratch_root)
