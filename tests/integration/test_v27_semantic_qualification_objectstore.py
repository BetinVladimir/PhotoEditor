"""Real leased SQL + versioned MinIO report proof; no GPU/model quality claim."""
from specialized_go_child_evidence import run_named_go
from pathlib import Path as _PhotoRepositoryPath
from contextlib import closing
from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4
import psycopg
from psycopg.conninfo import make_conninfo
from minio.error import S3Error
import pytest
from test_v25_lens_objectstore import lens_objects
from test_v27_semantic_final_producer_gate import fixture
from photo_worker.runtime.database import TaskDatabase
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.semantic_qualification_pin import SemanticQualificationPin
from photo_worker.v2.semantic_qualification_loader import semantic_qualification_loader
from photo_worker.operations.semantic.adapter import qualified_semantic_transfer_installation



def assert_live_vitest_report(path, expected, record_property):
    """Require executed child cases; an exit-zero skipped suite is insufficient."""
    import json
    report = json.loads(path.read_text())
    assert report['success'] is True, report
    assert report['numPassedTests'] == expected, report
    assert report['numFailedTests'] == report['numPendingTests'] == report.get('numTodoTests', 0) == 0, report
    cases = [case for suite in report['testResults'] for case in suite['assertionResults']]
    assert len(cases) == expected and all(case['status'] == 'passed' for case in cases), cases
    record_property(path.stem, json.dumps({'passed': expected, 'skipped': 0,
        'cases': [case['fullName'] for case in cases]}, sort_keys=True))

@pytest.fixture
def qualification_runtime_login(database):
    """Keep administrative seed/readback separate from runtime authority.

    The existing helper binds exactly this task's registration to a private
    photo_worker-only login and restores it before the database is removed.
    """
    from contextlib import ExitStack
    from wire_runtime_login import worker_login
    with ExitStack() as stack:
        yield lambda task: stack.enter_context(worker_login(make_conninfo(**database), task))


@pytest.mark.parametrize('proposed,deferred,exposure,with_alpha',[(False,False,False,False),(True,False,False,False),(True,True,False,False),(False,True,False,False),(True,True,True,False),(True,True,True,True),(True,True,True,"float32"),(True,True,True,"geometry"),(True,True,True,"geometry_half"),(True,True,True,"committed_replay"),(True,True,True,"bootstrap_fresh"),(True,True,True,"bootstrap_alpha"),(True,True,True,"bootstrap_alpha_float"),(True,True,True,"bootstrap_shown")],ids=['unsupported','proposed','fresh-assembly','fresh-unsupported','fresh-exposure','fresh-exposure-alpha','fresh-exposure-alpha-float32','fresh-exposure-geometry','fresh-exposure-geometry-half','fresh-exposure-committed-replay','fresh-exposure-bootstrap','fresh-exposure-bootstrap-alpha','fresh-exposure-bootstrap-alpha-float32','fresh-exposure-bootstrap-shown'])
def test_leased_qualification_reads_old_version_and_rechecks_revocation(database,lens_objects,tmp_path,proposed,deferred,exposure,with_alpha,record_property,qualification_runtime_login):
    import numpy as np
    edge=256 if proposed else 16
    expected_rgb=np.random.default_rng(2026).uniform(.05,.95,(edge,edge,3)).astype(np.float32) if proposed else np.full((edge,edge,3),.5,dtype=np.float32)
    expected_mask=np.zeros((edge,edge),dtype=np.float32) if proposed else np.ones((edge,edge),dtype=np.float32)
    if proposed:expected_mask[96:160,96:160]=1
    client,bucket=lens_objects
    with psycopg.connect(**database) as db:
        import json
        from photo_worker.operations.session.contracts import Calibration
        from photo_worker.contracts.base import canonical_bytes
        calibration_body=canonical_bytes(Calibration(schema_version=1,model_lock_sha='a'*64,dataset_sha='d'*64,qualified=True,
            scene_cosine_min=.7,scene_log_luma_max=1.,cluster_diameter_max=.3,sharpness_scale=1.,
            clipping_penalty=1.,match_inlier_min=.8,match_error_px_max=3.,identity_similarity_min=.7,
            landmark_drift_max=.1,normalization_scales=[1.]*6,loss_weights=[1/6]*6,
            focus_warning=.5,focus_critical=.2,eye_open_min=.5,glare_warning_fraction=.2,
            clipping_warning_fraction=.1,clipping_critical_fraction=.3,min_face_edge_px=32,
            depth_sigma=.1,registration_error_px=2.,motion_threshold=.1))
        from PIL import Image
        jpeg=BytesIO();Image.new('RGB',(edge,edge),(128,128,128)).save(jpeg,format='JPEG',quality=95)
        original_body=jpeg.getvalue()
        def source_body(publish,original_sha):
            from pathlib import Path
            import numpy as np
            from hashlib import sha256
            from photo_worker.contracts.base import ArtifactRef,canonical_bytes
            from photo_worker.contracts.images import Canvas,ColorSpace,WorkingImage,TransformChain,DenseWarpTransform
            binary=BytesIO();np.save(binary,expected_rgb);raw=binary.getvalue()
            digest=sha256(raw).hexdigest();aid=publish(digest,media='application/octet-stream',body=raw)
            alpha_ref=None
            if with_alpha is True or with_alpha in ("float32","bootstrap_alpha","bootstrap_alpha_float"):
                alpha_buffer=BytesIO();np.save(alpha_buffer,np.full((edge,edge),.5 if with_alpha in ("float32","bootstrap_alpha_float") else 32768,dtype=np.float32 if with_alpha in ("float32","bootstrap_alpha_float") else np.uint16));alpha_raw=alpha_buffer.getvalue()
                alpha_sha=sha256(alpha_raw).hexdigest();alpha_id=publish(alpha_sha,media='application/octet-stream',body=alpha_raw)
                alpha_ref=ArtifactRef(artifact_id=alpha_id,sha256=alpha_sha)
            canvas=Canvas(width=edge,height=edge,orientation=1,space_id='oriented',source_sha256=original_sha)
            chain=TransformChain.create([])
            if with_alpha in ("geometry","geometry_half"):
                y,x=np.mgrid[:edge,:edge];field=np.stack((x,y),axis=2).astype(np.float32)
                if with_alpha=="geometry_half":field+=np.float32(.5)
                field_buffer=BytesIO();np.save(field_buffer,field);field_raw=field_buffer.getvalue()
                field_sha=sha256(field_raw).hexdigest();field_id=publish(field_sha,media='application/octet-stream',body=field_raw)
                output_canvas=canvas.model_copy(update={'space_id':'warped'})
                chain=TransformChain.create([DenseWarpTransform(kind='dense_warp',from_canvas=canvas,to_canvas=output_canvas,invertibility='invertible',valid_region={'x':0,'y':0,'width':edge,'height':edge},inverse_field=ArtifactRef(artifact_id=field_id,sha256=field_sha),**({'pixel_centers':'half_pixel'} if with_alpha=="geometry_half" else {}))])
                canvas=output_canvas
            image=WorkingImage(pixels=ArtifactRef(artifact_id=aid,sha256=digest),canvas=canvas,color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256=sha256((Path(__file__).resolve().parents[2]/'resources/icc/work_rec2020_gamma24_v1.icc').read_bytes()).hexdigest(),encoding='gamma24'),dtype='float32',alpha=alpha_ref,transform_chain=chain)
            return canonical_bytes(image)
        def protections(scope,publish,tenant):
            import numpy as np
            from hashlib import sha256
            from psycopg.types.json import Jsonb
            from photo_worker.contracts.base import ArtifactRef,canonical_bytes
            from photo_worker.contracts.images import MaskRef,WorkingImage
            image=WorkingImage.model_validate_json(scope['source_body'])
            producer=uuid4()
            mask_run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'preview','running',%s,%s,%s,%s,true) RETURNING id",(tenant,scope['snapshot'],Jsonb({'project_id':str(scope['project'])}),'b'*64,scope['actor'])).fetchone()[0]
            db.execute("INSERT INTO photo.interactive_tasks(tenant_id,run_id,task_id,kind,ordinal,operation,source_artifact_id,declared_inputs,params,params_sha256,backend,runtime_lock_id,state) VALUES(%s,%s,%s,'mask_manual',0,'mask.rasterize',%s,'[]','{}',%s,'cpu',%s,'complete')",(tenant,mask_run,producer,scope['source'],'c'*64,'d'*64))
            db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'mask/00','mask.rasterize','1','cpu','ready','[]',%s,%s,'{}',1,1)",(producer,tenant,mask_run,scope['source'],'e'*64))
            db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')",(tenant,producer,uuid4()))
            roots={};outputs=[];scope['protection_pixel_inputs']=[]
            for name in ('source_protection','target_protection'):
                values=np.zeros((edge,edge),dtype=np.float32);values[0,0]=1
                binary=BytesIO();np.save(binary,values);raw=binary.getvalue();digest=sha256(raw).hexdigest()
                child=publish(digest,task=producer,media='application/octet-stream',body=raw)
                mask=MaskRef(artifact=ArtifactRef(artifact_id=child,sha256=digest),canvas=image.canvas,transform_chain=image.transform_chain,selector='manual',confidence=1.,model_lock_id=None,protected=True)
                body=canonical_bytes(mask);root_sha=sha256(body).hexdigest();root=publish(root_sha,task=producer,body=body)
                db.execute("UPDATE photo.artifacts SET output_role='mask_protected' WHERE id=%s",(child,))
                db.execute("UPDATE photo.artifacts SET output_role='mask_protected_ref' WHERE id=%s",(root,))
                roots[name]=dict(artifact_id=str(root),sha256=root_sha)
                outputs.extend([roots[name],dict(artifact_id=str(child),sha256=digest)])
                scope['protection_pixel_inputs'].append((child,digest,'NpyArray'))
            result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,producer,Jsonb(outputs),'e'*64)).fetchone()[0]
            db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,producer))
            scope['protection_producer']=producer
            return roots
        def params_body(scope,publish):
            binding=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='7'*64)
            from hashlib import sha256
            from photo_worker.contracts.semantic import EditIntent
            import numpy as np
            from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
            from photo_worker.contracts.images import Canvas,TransformChain,MaskRef
            from photo_worker.contracts.images import WorkingImage
            source_image=WorkingImage.model_validate_json(scope['source_body'])
            canvas=source_image.canvas
            binding['canvas_hash']=canonical_hash(canvas)
            binary=BytesIO();np.save(binary,expected_mask);binary_body=binary.getvalue()
            binary_sha=sha256(binary_body).hexdigest()
            binary_id=publish(binary_sha,media='application/octet-stream',body=binary_body)
            mask=MaskRef(artifact=ArtifactRef(artifact_id=binary_id,sha256=binary_sha),canvas=canvas,transform_chain=source_image.transform_chain,selector='manual',confidence=1.,model_lock_id=None,protected=False)
            mask_body=canonical_bytes(mask);mask_sha=sha256(mask_body).hexdigest()
            mask_id=publish(mask_sha,body=mask_body)
            scope['selected_ref']=dict(artifact_id=str(mask_id),sha256=mask_sha)
            scope['selected_pixels']=dict(artifact_id=str(binary_id),sha256=binary_sha)
            intent_body=json.dumps(dict(schema_version=1,kind='local_exposure' if exposure else 'remove_object',reference=binding,reference_mask=scope['selected_ref'],target_snapshot_id=str(scope['snapshot']),strength=1,ev=.25 if exposure else None,preserve_persons=True,prompt=None),sort_keys=True,separators=(',',':')).encode()
            EditIntent.model_validate_json(intent_body)
            intent_sha=sha256(intent_body).hexdigest()
            intent_id=publish(intent_sha,body=intent_body)
            scope['intent_ref']=dict(artifact_id=str(intent_id),sha256=intent_sha)
            scope['intent_body']=intent_body
            scope['transfer_inputs']=[(intent_id,intent_sha,'EditIntent'),(mask_id,mask_sha,'MaskRef'),(binary_id,binary_sha,'NpyArray')]
            return json.dumps(dict(schema_version=1,intent=scope['intent_ref'],reference=binding,target=binding,model_lock_sha=scope['qualification']['model_lock_sha'],calibration=scope['qualification']['calibration']),sort_keys=True,separators=(',',':')).encode()
        def frozen_snapshot(scope,publish):
            from photo_worker.contracts.semantic import SemanticTransferParams
            from photo_worker.operations.session.contracts import InputSnapshot,SnapshotItem
            from photo_worker.contracts.base import canonical_hash
            transfer=SemanticTransferParams.model_validate_json(scope['transfer_params_body'])
            snapshot=InputSnapshot(schema_version=1,snapshot_id=scope['snapshot'],snapshot_digest='0'*64,
                items=[SnapshotItem(ordinal=0,binding=transfer.target,working_image=transfer.target.source,
                    captured_at=None,camera_hash=None,recipe=transfer.intent)])
            snapshot.snapshot_digest=canonical_hash(snapshot.model_dump(mode='json',exclude={'snapshot_digest'}))
            return canonical_bytes(snapshot)
        tid,_,_,task,_,_,scope=fixture(db,snapshot_authority=True,qualification=True,qualification_objects=lens_objects,transfer_params_builder=params_body,calibration_body=calibration_body,source_builder=source_body,original_body=original_body,protection_builder=protections,transfer_resource_estimate=dict(ram_bytes=2*1024**3,vram_bytes=12*1024**3,scratch_bytes=1024**3,output_bytes=1024**3),physical_closure=True,snapshot_builder=frozen_snapshot,physical_assembly=True,defer_assembly=deferred)
        from v27_cpu_inputs_probe import verify_cpu_inputs
        if not deferred:verify_cpu_inputs(db,database,client,bucket,tid,scope,tmp_path)
        else:
            db.execute('INSERT INTO photo.tenant_usage(tenant_id,reserved_bytes) VALUES(%s,1048576)',(tid,))
            owner_run=db.execute('SELECT run_id FROM photo.tasks WHERE id=%s',(task,)).fetchone()[0]
            from psycopg.types.json import Jsonb
            db.execute("INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at) VALUES(%s,'user',%s,'run',%s,1048576,1,clock_timestamp()+interval '1 hour')",(tid,scope['actor'],owner_run))
            db.commit()
        run,params,digest=db.execute('SELECT run_id,params_artifact_id,input_digest FROM photo.tasks WHERE id=%s',(task,)).fetchone()
        protection_locations={name:db.execute('SELECT bucket,object_key,object_version FROM photo.artifacts WHERE id=%s',(ref['artifact_id'],)).fetchone() for name,ref in scope['protection'].items()}
        protection_pixel_locations=[db.execute('SELECT bucket,object_key,object_version FROM photo.artifacts WHERE id=%s',(entry[0],)).fetchone() for entry in scope['protection_pixel_inputs']]
        original_location=db.execute('SELECT bucket,object_key,object_version FROM photo.artifacts WHERE id=%s',(scope['original'],)).fetchone()
        rgb_location=db.execute('SELECT bucket,object_key,object_version FROM photo.artifacts WHERE id=%s',(scope['source_inputs'][0][0],)).fetchone()
        source_location=db.execute('SELECT bucket,object_key,object_version FROM photo.artifacts WHERE id=%s',(scope['source'],)).fetchone()
        calibration_location=db.execute('SELECT bucket,object_key,object_version FROM photo.artifacts WHERE id=%s',(scope['qualification']['calibration']['artifact_id'],)).fetchone()
        intent_location=db.execute('SELECT bucket,object_key,object_version FROM photo.artifacts WHERE id=%s',(scope['intent_ref']['artifact_id'],)).fetchone()
        assert db.execute("SELECT count(*) FROM photo.v2_task_inputs WHERE task_id=%s AND artifact_id=%s AND sha256=%s AND contract='EditIntent'",(task,scope['intent_ref']['artifact_id'],scope['intent_ref']['sha256'])).fetchone()[0]==1
        selected_location=db.execute('SELECT bucket,object_key,object_version FROM photo.artifacts WHERE id=%s',(scope['selected_ref']['artifact_id'],)).fetchone()
        pixels_location=db.execute('SELECT bucket,object_key,object_version FROM photo.artifacts WHERE id=%s',(scope['selected_pixels']['artifact_id'],)).fetchone()
        profile=db.execute('SELECT execution_profile FROM photo.tasks WHERE id=%s',(task,)).fetchone()[0]
        declared=db.execute('SELECT a.id,a.sha256,a.bucket,a.object_key,a.object_version FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256 WHERE i.task_id=%s',(task,)).fetchall()
        params_location=db.execute('SELECT bucket,object_key,object_version FROM photo.artifacts WHERE id=%s',(params,)).fetchone()
        worker,node=uuid4(),'semantic-report-'+str(uuid4())
        db.execute("INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes) VALUES(%s,4000000000,12000000000,2000000000)",(node,))
        db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions) VALUES(%s,%s,'[\"gpu\"]','[\"cuda\"]',%s,now(),'{}',session_user,%s,true,'{\"semantic.transfer\":[\"cuda\"]}','[\"v2.2\"]')",(tid,worker,'a'*64,node))
        db.execute("UPDATE photo.tasks SET state='running',attempt=2,fencing_token=2,canonical_result_id=NULL,worker_id=%s,lease_until=clock_timestamp()+interval '60 seconds',deadline=clock_timestamp()+interval '120 seconds' WHERE id=%s",(worker,task))
        db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity,tenant_id,task_id,fencing_token,lease_until) VALUES(%s,'gpu1','gpu','{}',%s,%s,2,clock_timestamp()+interval '60 seconds')",(node,tid,task))
        from psycopg.types.json import Jsonb
        db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,backend,profile) VALUES(%s,%s,2,2,%s,clock_timestamp(),'cuda',%s)",(tid,task,worker,Jsonb(profile)))
        db.commit()
    transport=TaskDatabase(qualification_runtime_login(task))
    lease=SimpleNamespace(command=SimpleNamespace(tenant_id=tid,run_id=run,task_id=task,attempt=2,params=SimpleNamespace(artifact_id=params,sha256=scope['transfer_params_sha'])),fence=2,worker_id=worker,input_digest=digest)
    assert transport.semantic_assembly_qualification(lease) is None  # GPU transfer cannot acquire CPU assembly authority.
    assert transport.semantic_assembly_snapshot(lease)==[]
    assert transport.semantic_assembly_report_evidence(lease) is None
    assert transport.semantic_assembly_producer(lease,lease.command.params) is None
    pin=SemanticQualificationPin.from_value(scope['qualification'])
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.contracts.semantic import SemanticTransferParams
    from photo_worker.runtime.parameters import read_contract_bytes
    params_ref=ArtifactRef(artifact_id=params,sha256=scope['transfer_params_sha'])
    def read_params():
        return read_contract_bytes(params_ref,SemanticTransferParams,
            lambda ref:client.get_object(params_location[0],params_location[1],version_id=params_location[2]),lambda:None)
    assert read_params()==scope['transfer_params_body']
    from photo_worker.contracts.semantic import EditIntent
    from photo_worker.runtime.parameters import read_contract
    intent_ref=ArtifactRef.model_validate_json(json.dumps(scope['intent_ref']))
    intent=read_contract(intent_ref,EditIntent,lambda ref:client.get_object(intent_location[0],intent_location[1],version_id=intent_location[2]),lambda:None)
    typed_params=SemanticTransferParams.model_validate_json(read_params())
    assert typed_params.intent==intent_ref and typed_params.reference==intent.reference
    assert intent.target_snapshot_id==scope['snapshot']
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.contracts.base import canonical_hash
    image=read_contract(typed_params.reference.source,WorkingImage,lambda ref:client.get_object(source_location[0],source_location[1],version_id=source_location[2]),lambda:None)
    assert canonical_hash(image.canvas)==typed_params.reference.canvas_hash and image.dtype=='float32'
    from hashlib import sha256
    original_response=client.get_object(original_location[0],original_location[1],version_id=original_location[2])
    try:
        original_bytes=original_response.read()
    finally:
        original_response.close();original_response.release_conn()
    assert original_bytes==original_body and sha256(original_bytes).hexdigest()==image.canvas.source_sha256
    with Image.open(BytesIO(original_bytes)) as decoded:
        assert decoded.size==(edge,edge) and decoded.mode=='RGB'
        decoded.load()
    assert transport._query("SELECT nullif(current_setting('app.tenant_id',true),'') IS NULL",())[0] is True
    assert transport.semantic_source_ready(lease,typed_params.reference,target=False)
    for field,value in (('tenant_id',uuid4()),('task_id',uuid4())):
        altered=SimpleNamespace(**vars(lease.command));setattr(altered,field,value)
        invalid=SimpleNamespace(**{**vars(lease),'command':altered})
        assert not transport.semantic_source_ready(invalid,typed_params.reference,target=False)
    assert not transport.semantic_source_ready(SimpleNamespace(**{**vars(lease),'fence':lease.fence+1}),typed_params.reference,target=False)
    assert transport.semantic_source_ready(lease,typed_params.target,target=True)
    from photo_worker.contracts.images import MaskRef
    assert transport.semantic_protection_pins(lease)==scope['protection']
    for (name,ref),protection_pixel_location in zip(scope['protection'].items(),protection_pixel_locations):
        location=protection_locations[name]
        root=ArtifactRef.model_validate_json(json.dumps(ref))
        protected=read_contract(root,MaskRef,lambda value:client.get_object(location[0],location[1],version_id=location[2]),lambda:None)
        assert protected.protected and protected.canvas==image.canvas and protected.transform_chain==image.transform_chain
        assert transport.semantic_protection_origin(lease,typed_params.target if name=='target_protection' else typed_params.reference,root,protected.artifact,target=name=='target_protection')
        response=client.get_object(protection_pixel_location[0],protection_pixel_location[1],version_id=protection_pixel_location[2])
        try:body=response.read()
        finally:response.close();response.release_conn()
        from hashlib import sha256
        import numpy as np
        assert sha256(body).hexdigest()==protected.artifact.sha256
        values=np.load(BytesIO(body),allow_pickle=False)
        assert values.dtype==np.float32 and values.shape==(edge,edge) and np.count_nonzero(values)==1 and values[0,0]==1
    import numpy as np
    from hashlib import sha256
    rgb_response=client.get_object(rgb_location[0],rgb_location[1],version_id=rgb_location[2])
    try:
        rgb_body=rgb_response.read()
    finally:
        rgb_response.close();rgb_response.release_conn()
    assert sha256(rgb_body).hexdigest()==image.pixels.sha256
    rgb=np.load(BytesIO(rgb_body),allow_pickle=False)
    assert rgb.dtype==np.float32 and rgb.shape==(edge,edge,3) and np.array_equal(rgb,expected_rgb)
    from photo_worker.runtime.artifacts import TaskArtifacts
    from photo_worker.runtime.scratch import ScratchSpace
    from photo_worker.operations.semantic.adapter import sql_semantic_artifact_factory
    lease.command.params=params_ref
    lease.command.inputs=[ArtifactRef.model_validate_json(json.dumps(profile['v2_input_manifest']))]
    lease.execution_profile=profile
    locations={(row[0],row[1]):row[2:] for row in declared}
    opened=[]
    class InputStream:
        def __init__(self,response):self.response=response;self.size=int(response.headers['Content-Length']);self.closed=False
        def read(self,size):return self.response.read(size)
        def close(self):
            if not self.closed:self.response.close();self.response.release_conn();self.closed=True
    def open_input(ref):
        location=locations[(ref.artifact_id,ref.sha256)]
        opened.append(ref)
        return InputStream(client.get_object(location[0],location[1],version_id=location[2]))
    context=SimpleNamespace(lease=lease,check_active=lambda:None)
    colors=[]
    factory=sql_semantic_artifact_factory(database=transport,objects=client,bucket=bucket,
        model_lock_sha=pin.model_lock_sha,runtime_lock_sha=pin.runtime_lock_sha,
        device_profile_sha=pin.device_profile_sha,dataset_sha=pin.dataset_sha,
        verify_color=lambda value:colors.append(value),verify_scene=lambda *args:None)
    with ScratchSpace(tmp_path,tid,task,1,1024**3,min_free_bytes=0) as scratch:
        from photo_worker.v2.input_closure import V2InputIndex
        index=V2InputIndex.build(task_id=task,inputs=lease.command.inputs,params=params_ref,execution_profile=profile,scratch=scratch,open_stream=open_input,check_active=context.check_active)
        baseline=set(scratch.directory.iterdir())
        assert baseline
        with TaskArtifacts(context,SimpleNamespace(open_stream=open_input),scratch,index) as artifacts:
            assert artifacts.declared_contract_refs('Calibration')==(typed_params.calibration,)
            assert artifacts.declared_contract_refs('WorkingImage')==(typed_params.reference.source,)
            before=len(opened)
            with pytest.raises(ValueError,match='not a declared task input'):
                artifacts.contract_bytes(ArtifactRef(artifact_id=uuid4(),sha256='0'*64),SemanticTransferParams)
            assert len(opened)==before
            physical=factory(context,artifacts)
            assert physical.verify_runtime()==pin
            with physical.load_arrays(artifacts,typed_params,image,image,profile,context.check_active) as arrays:
                assert arrays.reference.shape==(edge,edge,3) and arrays.target.shape==(edge,edge,3)
                assert np.allclose(arrays.reference,expected_rgb**2.4) and np.allclose(arrays.target,expected_rgb**2.4)
                assert np.array_equal(arrays.mask,expected_mask) and arrays.reference_protected[0,0]==1 and arrays.target_protected[0,0]==1
            assert set(scratch.directory.iterdir())==baseline
            try:
                with pytest.raises(AdoptionInvalid,match='ORIGIN_STALE'):
                    with physical.load_arrays(artifacts,typed_params,image,image,profile,context.check_active):
                        with psycopg.connect(**database) as revoke:
                            revoke.execute('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(scope['protection_producer'],))
                            revoke.commit()
                assert set(scratch.directory.iterdir())==baseline
            finally:
                with psycopg.connect(**database) as restore:
                    restore.execute('UPDATE photo.tasks SET fencing_token=1 WHERE id=%s',(scope['protection_producer'],))
                    restore.commit()
    assert not scratch.directory.exists()
    assert opened[0]==lease.command.inputs[0] and colors
    import os
    import subprocess
    from pathlib import Path
    repo=Path(__file__).resolve().parents[2]
    credentials=client._provider.retrieve()
    environment=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_TEST_V27_GATEWAY_TASK=str(task),PHOTO_TEST_V27_REPO_ROOT=str(repo),
        PHOTO_TEST_MINIO_URL='http://'+client._base_url.host,PHOTO_TEST_MINIO_BUCKET=bucket,
        PHOTO_TEST_MINIO_ACCESS=credentials.access_key,PHOTO_TEST_MINIO_SECRET=credentials.secret_key,
        GOCACHE=str(_PhotoRepositoryPath(__file__).resolve().parents[2]/'work/go-cache'),PHOTO_TEST_V27_EXPECTED_STATE='proposed' if proposed else 'unsupported')
    wire=run_named_go(['go','test','./internal/gateway','-run','^TestIntegrationV27IndexedDownloads$','-count=1','-timeout=90s','-v'],cwd=repo/'backend',env=environment,capture_output=True,text=True,timeout=100)
    assert wire.returncode==0,wire.stdout+wire.stderr
    assert '--- PASS: TestIntegrationV27IndexedDownloads' in wire.stdout
    with psycopg.connect(**database) as registry:
        assert registry.execute("SELECT resource_class,routing_key,timeout_seconds,backends,v2_enabled FROM photo.operation_registry WHERE operation='semantic.transfer' AND version='1'").fetchone()==('gpu','gpu.semantic_transfer',1800,['cuda'],False)
        roles=registry.execute("SELECT role,max_bytes,max_count,media_types FROM photo.operation_output_policies WHERE operation='semantic.transfer' AND version='1' ORDER BY role").fetchall()
        assert roles==[('document',8388608,1,['application/json']),('mask',8388608,1,['application/json']),('mask_pixels',800004096,1,['application/x-npy']),('transform',8388608,1,['application/json'])]
    from photo_worker.operations.session.contracts import Calibration
    calibration=read_contract(typed_params.calibration,Calibration,lambda ref:client.get_object(calibration_location[0],calibration_location[1],version_id=calibration_location[2]),lambda:None)
    assert calibration.model_lock_sha==pin.model_lock_sha and calibration.dataset_sha==pin.dataset_sha
    assert typed_params.calibration.artifact_id==pin.calibration.artifact_id
    assert typed_params.calibration.sha256==pin.calibration.sha256
    from photo_worker.contracts.semantic import bind_semantic_transfer_documents
    assert bind_semantic_transfer_documents(typed_params,intent,calibration)==(typed_params,intent,calibration)
    from photo_worker.contracts.images import MaskRef
    from photo_worker.contracts.base import canonical_hash
    from hashlib import sha256
    import numpy as np
    selected=read_contract(intent.reference_mask,MaskRef,lambda ref:client.get_object(selected_location[0],selected_location[1],version_id=selected_location[2]),lambda:None)
    assert not selected.protected and canonical_hash(selected.canvas)==typed_params.reference.canvas_hash
    assert selected.artifact.sha256==scope['selected_pixels']['sha256']
    response=client.get_object(pixels_location[0],pixels_location[1],version_id=pixels_location[2])
    try:
        pixels_body=response.read()
    finally:
        response.close();response.release_conn()
    assert sha256(pixels_body).hexdigest()==selected.artifact.sha256
    pixels=np.load(BytesIO(pixels_body),allow_pickle=False)
    assert pixels.dtype==np.float32 and pixels.shape==(edge,edge) and np.array_equal(pixels,expected_mask)
    newer=client.put_object(params_location[0],params_location[1],BytesIO(b'{}'),2,content_type='application/json')
    assert newer.version_id!=params_location[2] and read_params()==scope['transfer_params_body']
    load=semantic_qualification_loader(transport,lease,client,bucket,frozen_pin=pin)
    evidence=transport.semantic_report_evidence(lease)
    version=evidence['report']['object_version'];key=evidence['report']['object_key']
    assert version not in ('','null','pinned-v1')
    assert load()==scope['qualification']
    ctx=SimpleNamespace(lease=lease,check_active=lambda:None)
    def forbidden_source(*args):
        raise AssertionError('Qualification verification must not read source images')
    def install(objects=client,device=pin.device_profile_sha):
        return qualified_semantic_transfer_installation(ctx,database=transport,objects=objects,bucket=bucket,
            frozen_pin=pin,model_lock_sha=pin.model_lock_sha,runtime_lock_sha=pin.runtime_lock_sha,
            device_profile_sha=device,dataset_sha=pin.dataset_sha,load_arrays=forbidden_source,verify_scene=forbidden_source)
    installation=install()
    assert installation.verify_runtime()==pin
    lease.command.params.sha256='0'*64
    try:
        assert load()==scope['qualification']
        assert installation.verify_runtime()==pin
        assert transport.semantic_report_evidence(lease) is None
    finally:
        lease.command.params.sha256=scope['transfer_params_sha']
    with pytest.raises(AdoptionInvalid,match='QUALIFICATION_CHANGED'):
        install(device='0'*64).verify_runtime()
    replacement=client.put_object(bucket,key,BytesIO(b'{}'),2,content_type='application/json')
    assert replacement.version_id!=version
    with closing(client.get_object(bucket,key)) as response:assert response.read()==b'{}'
    assert load()==scope['qualification']  # Newest malformed bytes cannot replace the frozen version.
    assert installation.verify_runtime()==pin
    class RevokingObjects:
        def get_object(self,*args,**kwargs):
            response=client.get_object(*args,**kwargs)
            with psycopg.connect(**database) as revocation:
                revocation.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin.policy_id,))
                revocation.commit()
            return response
    revoked=install(objects=RevokingObjects())
    with pytest.raises(AdoptionInvalid,match='SCOPE_STALE'):revoked.verify_runtime()
    with psycopg.connect(**database) as restore:
        restore.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=%s',(pin.policy_id,))
        restore.commit()
    assert load()==scope['qualification']
    assert installation.verify_runtime()==pin

    # Completion is distinct from final adoption: a later revocation must still
    # prevent adoption even when the immutable task result was committed.
    from photo_worker.contracts.base import canonical_bytes
    with psycopg.connect(**database) as completed:
        output=completed.execute("SELECT artifact_id,sha256,bytes FROM photo.output_uploads WHERE task_id=%s AND attempt=2 AND fence=2 AND role='document' AND state='ready'",(task,)).fetchone()
    assert output is not None
    payload=canonical_bytes(dict(outputs=[dict(artifact_id=str(output[0]),sha256=output[1])],metrics=dict(duration_ms=0,peak_memory_bytes=0,bytes_written=output[2]),review_required=proposed))
    # This fixture imports qualification reports; activation here is an explicit
    # isolated operator action, not measured model/photo qualification.
    assert transport.commit_success(lease,payload)==('stale',None,None)
    with psycopg.connect(**database) as activate:
        assert activate.execute('SELECT canonical_result_id FROM photo.tasks WHERE id=%s',(task,)).fetchone()==(None,)
        assert activate.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='semantic.transfer' AND version='1' AND NOT v2_enabled").rowcount==1
        assert activate.execute('SELECT photo.execution_source_live(%s)',(task,)).fetchone()==(True,)
        # The same real, already sealed live graph distinguishes caller context
        # from a missing/nonsealable graph. Roll back every probe transaction.
        for value,expected in (('',False),(str(uuid4()),False),('invalid',False),(str(tid),True)):
            with activate.transaction(force_rollback=True):
                activate.execute('SET LOCAL ROLE photo_scheduler')
                activate.execute("SELECT set_config('app.tenant_id',%s,true)",(value,))
                assert activate.execute('SELECT photo.seal_v2_graph(%s,%s)',(tid,run)).fetchone()==(expected,)
    outcome,result_id,result_sha=transport.commit_success(lease,payload)
    assert outcome=='committed' and result_id is not None and result_sha
    assert transport.commit_success(lease,payload)==('replayed',result_id,result_sha)
    with psycopg.connect(**database) as completed:
        state=completed.execute('SELECT state,canonical_result_id FROM photo.tasks WHERE id=%s',(task,)).fetchone()
        assert state==('succeeded',result_id)
        assert completed.execute('SELECT count(*) FROM photo.task_results WHERE task_id=%s AND attempt=1',(task,)).fetchone()[0]==1
        assert completed.execute('SELECT count(*) FROM photo.task_results WHERE task_id=%s AND attempt=2',(task,)).fetchone()[0]==1
    assert transport.semantic_report_evidence(lease) is None

    # Completed-result source proof must not reuse a running worker lease.
    with psycopg.connect(**database,autocommit=True) as authority:
        authority.execute('SET ROLE photo_scheduler')
        def source_ready(binding,target,attempt=2,fence=2,result=result_id):
            return authority.execute('SELECT photo.v27_semantic_adoption_source_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                (tid,task,attempt,fence,result,binding.asset_version_id,binding.source.artifact_id,binding.source.sha256,target)).fetchone()[0]
        assert source_ready(typed_params.reference,False)
        assert source_ready(typed_params.target,True)
        assert not source_ready(typed_params.target,True,attempt=1)
        assert not source_ready(typed_params.target,True,fence=1)
        assert not source_ready(typed_params.target,True,result=uuid4())

        for name,ref in scope['protection'].items():
            location=protection_locations[name]
            root=ArtifactRef.model_validate_json(json.dumps(ref))
            protected=read_contract(root,MaskRef,lambda value:client.get_object(location[0],location[1],version_id=location[2]),lambda:None)
            target=name=='target_protection'
            binding=typed_params.target if target else typed_params.reference
            args=(tid,task,2,2,result_id,binding.asset_version_id,binding.source.artifact_id,binding.source.sha256,
                target,root.artifact_id,root.sha256,protected.artifact.artifact_id,protected.artifact.sha256)
            query='SELECT photo.v27_semantic_adoption_protection_origin('+','.join(['%s']*13)+')'
            assert authority.execute(query,args).fetchone()[0]
            changed=list(args);changed[-1]='0'*64
            assert not authority.execute(query,changed).fetchone()[0]
            changed=list(args);changed[9]=uuid4()
            assert not authority.execute(query,changed).fetchone()[0]

        report_query='SELECT photo.v27_semantic_adoption_report_evidence(%s,%s,%s,%s,%s)'
        report_args=(tid,task,2,2,result_id)
        record=authority.execute(report_query,report_args).fetchone()[0]
        assert record['pin']==scope['qualification'] and record['report']['object_version']==version
        assert record['policy']['policy_id']==str(pin.policy_id)
        assert authority.execute(report_query,(tid,task,1,2,result_id)).fetchone()[0] is None
        with psycopg.connect(**database) as revoke:
            revoke.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin.policy_id,))
        try:
            assert authority.execute(report_query,report_args).fetchone()[0] is None
        finally:
            with psycopg.connect(**database) as restore:
                restore.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=%s',(pin.policy_id,))

        from photo_worker.v2.semantic_transfer_authority import sql_semantic_adoption_authority
        from photo_worker.v2.adoption_store import PostgresObjectEvidenceReader
        from photo_worker.v2.adoption import ArtifactRef as AdoptionRef
        from photo_worker.v2.adoption_service import AdoptionContext
        from photo_worker.operations.session.contracts import Calibration
        evidence_reader=PostgresObjectEvidenceReader(authority,client,tenant_id=tid,task_id=task,attempt=2,fence=2,allowed_bucket=bucket)
        calibration_ref=AdoptionRef(typed_params.calibration.artifact_id,typed_params.calibration.sha256)
        calibration=Calibration.model_validate_json(evidence_reader.read_json(calibration_ref,1048576))
        result_context=AdoptionContext(2,2,result_id,'semantic.transfer','1',AdoptionRef(output[0],output[1]))
        scene_calls=[]
        def controlled_scene(p,i,c,images,selected,guards):
            # Fixture authority only: no scientific planar/occlusion claim.
            assert p==typed_params and i==intent and c==calibration
            assert images==(image,image) and len(guards)==2 and all(mask.protected for mask in guards)
            scene_calls.append(1)
        composed=sql_semantic_adoption_authority(authority,client,bucket,working_icc_sha=image.color.icc_sha256,verify_scene=controlled_scene)
        composed(tid,task,result_context,typed_params,intent,calibration,None,[])
        assert scene_calls==[1]
        from photo_worker.v2.service import Adopter
        with psycopg.connect(**database) as adoption_write:
            adoption_write.execute('SET ROLE photo_scheduler');adoption_write.commit()
            adopter=Adopter(authority,adoption_write,client,bucket,layer_scratch_root=tmp_path,
                layer_scratch_bytes=32*1024*1024,semantic_ram_bytes=256*1024*1024,
                semantic_scene_verifier=controlled_scene,semantic_working_icc_sha=image.color.icc_sha256)
            assert adopter.coordinator.adopt(tid,task) is True
            assert adopter.coordinator.adopt(tid,task) is False
            with psycopg.connect(**database) as audit:
                assert audit.execute("SELECT count(*) FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(%s,%s)",(tid,task,result_id)).fetchone()[0]==1
        assert len(scene_calls)>=2
        if deferred:
            expansion=authority.execute('SELECT photo.open_v27_semantic_assembly(%s,%s)',(tid,run)).fetchone()[0]
            assert expansion is not None
            claim=authority.execute('SELECT * FROM photo.claim_v27_semantic_assembly()').fetchone()
            assert claim is not None and claim[0]==expansion
            inputs=authority.execute('SELECT * FROM photo.v27_semantic_assembly_inputs(%s,%s)',(tid,run)).fetchall()
            assert len(inputs)==1 and output[0] in inputs[0]
            inventory=authority.execute('SELECT * FROM photo.v27_semantic_assembly_source_inventory(%s,%s,%s,%s)',(tid,run,expansion,claim[1])).fetchall()
            assert any(row[1]==output[0] and row[3]=='TransferCandidate' for row in inventory)
            assert authority.execute('SELECT photo.release_v27_semantic_assembly(%s,%s)',(expansion,claim[1])).fetchone()[0]
            with psycopg.connect(**database) as due:
                due.execute('UPDATE photo.graph_expansions SET next_expansion_at=clock_timestamp() WHERE id=%s',(expansion,))
            credentials=client._provider.retrieve()
            environment=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_TEST_V27_ASSEMBLY_RUN=str(run),
                PHOTO_TEST_MINIO_URL='http://'+client._base_url.host,PHOTO_TEST_MINIO_BUCKET=bucket,
                PHOTO_TEST_MINIO_ACCESS=credentials.access_key,PHOTO_TEST_MINIO_SECRET=credentials.secret_key,GOCACHE=str(_PhotoRepositoryPath(__file__).resolve().parents[2]/'work/go-cache'))
            orchestration=run_named_go(['go','test','./internal/v2input','-run','^TestIntegrationV27AssemblyRuntime$','-count=1','-timeout=90s','-v'],cwd=repo/'backend',env=environment,capture_output=True,text=True,timeout=100)
            assert orchestration.returncode==0,orchestration.stdout+orchestration.stderr
            assert 'V27_ASSEMBLY_RUNTIME_OK' in orchestration.stdout
            with psycopg.connect(**database) as published:
                rows=published.execute("SELECT id,state,input_digest FROM photo.tasks WHERE tenant_id=%s AND run_id=%s AND operation='v2.semantic.assemble'",(tid,run)).fetchall()
                assert len(rows)==1 and rows[0][1]=='blocked' and rows[0][2]
                assert published.execute('SELECT photo.v2_input_closure_ready(%s)',(rows[0][0],)).fetchone()[0]
                scope['assembly']=rows[0][0]
                expected_review_state='proposed' if proposed else 'unsupported'
                verify_cpu_inputs(published,database,client,bucket,tid,scope,tmp_path,expected_state=expected_review_state,reserve_quota=False)
                assert published.execute('SELECT photo.finalize_v2_run(%s,%s)',(tid,run)).fetchone()[0]=='waiting_review'
            if exposure:
                from semantic_qualification_fixture import frozen_qualification
                with psycopg.connect(**database) as qualification_db:
                    cpu_pin=frozen_qualification(qualification_db,tid,scope['project'],scope['actor'],objects=(client,bucket),operation='semantic.local_exposure',backend='cpu')
                environment['PHOTO_TEST_V27_EXPOSURE_PIN']=json.dumps(cpu_pin)
                environment['PHOTO_TEST_V27_EXPOSURE_PROTECTION']=json.dumps(scope['protection']['target_protection'])
                if with_alpha in ('committed_replay','bootstrap_fresh','bootstrap_alpha','bootstrap_alpha_float','bootstrap_shown'):environment['PHOTO_TEST_V27_COMMITTED_REPLAY']='true'
            environment['PHOTO_TEST_V27_REVIEW_STATE']=expected_review_state
            if with_alpha=='committed_replay':
                bootstrap=run_named_go(['go','test','./cmd/photo','-run','^TestIntegrationSemanticExposureBootstrap$','-count=1','-timeout=90s','-v'],cwd=repo/'backend',env=environment,capture_output=True,text=True,timeout=100)
                assert bootstrap.returncode==0,bootstrap.stdout+bootstrap.stderr
                assert 'V27_EXPOSURE_BOOTSTRAP_OK' in bootstrap.stdout
            if with_alpha in ('bootstrap_fresh','bootstrap_alpha','bootstrap_alpha_float','bootstrap_shown'):environment['PHOTO_TEST_V27_PREPARED_APPLY_FILE']=str(tmp_path/'prepared-apply.json')
            review=run_named_go(['go','test','./internal/semanticreview','-run','^TestIntegrationV27ReviewReadsAdoptedPlan$','-count=1','-timeout=90s','-v'],cwd=repo/'backend',env=environment,capture_output=True,text=True,timeout=100)
            assert review.returncode==0,review.stdout+review.stderr
            assert 'V27_REVIEW_READ_OK' in review.stdout
            assert 'V27_INITIALIZED_RUN_INTENT_AUTHORITY_OK' in review.stdout
            assert 'V27_RUN_INTENT_RESTRICTED_ACL_ARCHIVE_OK' in review.stdout
            if with_alpha=='bootstrap_shown':
                from v27_shown_capture_lifecycle import run_shown_capture_lifecycle
                run_shown_capture_lifecycle(database,client,bucket,tid,scope,tmp_path,environment,record_property)
            if with_alpha in ('bootstrap_fresh','bootstrap_alpha','bootstrap_alpha_float'):
                assert 'V27_REVIEW_PREPARED_OK' in review.stdout
                environment['PHOTO_TEST_V27_APPLY_RESPONSE_FILE']=str(tmp_path/'apply-response.json')
                fresh_bootstrap=run_named_go(['go','test','./cmd/photo','-run','^TestIntegrationSemanticExposureBootstrap$','-count=1','-timeout=90s','-v'],cwd=repo/'backend',env=environment,capture_output=True,text=True,timeout=100)
                assert fresh_bootstrap.returncode==0,fresh_bootstrap.stdout+fresh_bootstrap.stderr
                assert 'V27_EXPOSURE_FRESH_OK' in fresh_bootstrap.stdout
                assert 'V27_CLOSE_QUEUED_CHILD_OK' in fresh_bootstrap.stdout
                apply_sdk_environment=dict(os.environ,PHOTO_TEST_V27_APPLY_RESPONSE_FILE=str(tmp_path/'apply-response.json'))
                apply_sdk=subprocess.run(['npm','test','--','--run','src/semantic-apply.integration.test.ts','--reporter=json','--outputFile='+str(tmp_path/'apply_sdk-vitest.json')],cwd=repo/'packages'/'api-client',env=apply_sdk_environment,capture_output=True,text=True,timeout=40)
                assert apply_sdk.returncode==0,apply_sdk.stdout+apply_sdk.stderr
                assert_live_vitest_report(tmp_path/'apply_sdk-vitest.json', 1, record_property)
                apply_ui=subprocess.run(['npm','test','--','--run','src/semantic-review-decisions.integration.test.tsx','--reporter=json','--outputFile='+str(tmp_path/'ordinary-decision-guard-ui-vitest.json')],cwd=repo/'apps'/'photographer',env=apply_sdk_environment,capture_output=True,text=True,timeout=40)
                assert apply_ui.returncode==0,apply_ui.stdout+apply_ui.stderr
                assert_live_vitest_report(tmp_path/'ordinary-decision-guard-ui-vitest.json', 1, record_property)
                from v27_exposure_inputs_probe import verify_http_exposure_worker_inputs
                with psycopg.connect(**database) as leased_exposure:
                    exposure_task=verify_http_exposure_worker_inputs(leased_exposure,database,client,bucket,tid,tmp_path/'exposure-inputs')
                exposure_environment=dict(environment,PHOTO_TEST_V27_GATEWAY_TASK=str(exposure_task),PHOTO_TEST_V27_REPO_ROOT=str(repo))
                exposure_wire=run_named_go(['go','test','./internal/gateway','-run','^TestIntegrationV27IndexedDownloads$','-count=1','-timeout=90s','-v'],cwd=repo/'backend',env=exposure_environment,capture_output=True,text=True,timeout=100)
                assert exposure_wire.returncode==0,exposure_wire.stdout+exposure_wire.stderr
                if os.environ.get('PHOTO_TEST_V27_EARLY_CLOSE')=='true':
                    assert 'V27_CLOSE_EARLY_DURABLE_OK' in fresh_bootstrap.stdout
                    assert 'V27_WORKER_WIRE_OK' in exposure_wire.stdout
                    with psycopg.connect(**database) as early_verify:
                        state=early_verify.execute("SELECT parent.status,child.status,target.state FROM photo.semantic_targets target JOIN photo.semantic_intents intent ON intent.tenant_id=target.tenant_id AND intent.id=target.intent_id JOIN photo.v2_workflows workflow ON workflow.tenant_id=intent.tenant_id AND workflow.id=intent.workflow_id JOIN photo.runs parent ON parent.tenant_id=workflow.tenant_id AND parent.id=workflow.run_id JOIN photo.runs child ON child.tenant_id=target.tenant_id AND child.id=target.child_run_id JOIN photo.tasks task ON task.tenant_id=child.tenant_id AND task.run_id=child.id WHERE task.id=%s",(exposure_task,)).fetchone()
                        assert state==('completed','completed','applied')
                    return
                exposure_environment['PHOTO_TEST_V27_TARGETS_RESPONSE_FILE']=str(tmp_path/'targets-response.json')
                exposure_environment['PHOTO_TEST_V27_OUTPUTS_RESPONSE_FILE']=str(tmp_path/'outputs-response.json')
                targets_http=run_named_go(['go','test','./internal/httpapi','-run','^TestIntegrationCompletedSemanticTargetsHTTP$','-count=1','-timeout=30s','-v'],cwd=repo/'backend',env=exposure_environment,capture_output=True,text=True,timeout=40)
                assert targets_http.returncode==0,targets_http.stdout+targets_http.stderr
                assert 'V27_TARGETS_HTTP_OK' in targets_http.stdout
                assert 'V27_CLOSE_HTTP_OK' in targets_http.stdout
                if os.environ.get('PHOTO_TEST_V27_CLOSE_RACE')=='same-key':
                    assert 'V27_CLOSE_SAME_KEY_OK' in targets_http.stdout
                sdk_environment=dict(os.environ,PHOTO_TEST_V27_TARGETS_RESPONSE_FILE=str(tmp_path/'targets-response.json'))
                sdk=subprocess.run(['npm','test','--','--run','src/semantic.integration.test.ts','--reporter=json','--outputFile='+str(tmp_path/'sdk-vitest.json')],cwd=repo/'packages'/'api-client',env=sdk_environment,capture_output=True,text=True,timeout=40)
                assert sdk.returncode==0,sdk.stdout+sdk.stderr
                assert_live_vitest_report(tmp_path/'sdk-vitest.json', 1, record_property)
                ui=subprocess.run(['npm','test','--','--run','src/semantic-target-screen.integration.test.tsx','--reporter=json','--outputFile='+str(tmp_path/'ui-vitest.json')],cwd=repo/'apps'/'photographer',env=sdk_environment,capture_output=True,text=True,timeout=40)
                assert ui.returncode==0,ui.stdout+ui.stderr
                assert_live_vitest_report(tmp_path/'ui-vitest.json', 1, record_property)
                output_capture=tmp_path/'outputs-response.json'
                output_environment=dict(os.environ,PHOTO_TEST_V27_OUTPUTS_RESPONSE_FILE=str(output_capture))
                try:
                    output_sdk=subprocess.run(['npm','test','--','--run','src/run-outputs.integration.test.ts','--reporter=json','--outputFile='+str(tmp_path/'output_sdk-vitest.json')],cwd=repo/'packages'/'api-client',env=output_environment,capture_output=True,text=True,timeout=40)
                    assert output_sdk.returncode==0,output_sdk.stdout+output_sdk.stderr
                    assert_live_vitest_report(tmp_path/'output_sdk-vitest.json', 1, record_property)
                    # ShownAppliedPreview UI is exercised by bootstrap_shown's
                    # real preview/selection/copy/adoption endpoint, separately.

                finally:
                    output_capture.unlink(missing_ok=True)
            if with_alpha=='committed_replay':
                environment['PHOTO_TEST_V27_BOOTSTRAP_REPLAY']='true'
                replay_bootstrap=run_named_go(['go','test','./cmd/photo','-run','^TestIntegrationSemanticExposureBootstrap$','-count=1','-timeout=90s','-v'],cwd=repo/'backend',env=environment,capture_output=True,text=True,timeout=100)
                assert replay_bootstrap.returncode==0,replay_bootstrap.stdout+replay_bootstrap.stderr
                assert 'V27_EXPOSURE_REPLAY_OK' in replay_bootstrap.stdout
        calls_before_missing=len(scene_calls)
        if deferred:
            # Explicit review finalization ends the adopter's running-run scope.
            # Prove this denial before deleting any report bytes.
            with pytest.raises(AdoptionInvalid,match='SEMANTIC_QUALIFICATION_SCOPE_STALE'):
                composed(tid,task,result_context,typed_params,intent,calibration,None,[])
            assert len(scene_calls)==calls_before_missing
        client.remove_object(bucket,key,version_id=version)
        if deferred:
            with pytest.raises(AdoptionInvalid,match='SEMANTIC_QUALIFICATION_SCOPE_STALE'):
                composed(tid,task,result_context,typed_params,intent,calibration,None,[])
            assert len(scene_calls)==calls_before_missing
        else:
            with pytest.raises(S3Error) as missing:
                composed(tid,task,result_context,typed_params,intent,calibration,None,[])
            assert missing.value.code in ('NoSuchKey','NoSuchVersion') and len(scene_calls)==calls_before_missing
