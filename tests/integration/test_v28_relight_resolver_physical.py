"""Composed live authorities; synthetic qualification and seeded producer state."""
from io import BytesIO
from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
from hashlib import sha256
from pathlib import Path
import json
import psycopg
from psycopg.conninfo import make_conninfo
import pytest
import numpy as np
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,MaskRef,RelativeDepth
from photo_worker.contracts.relight import ImageRelightParams
from photo_worker.operations.session.contracts import Calibration
from photo_worker.runtime.database import TaskDatabase
from wire_runtime_login import worker_login
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.operations.portrait.relight_resolver import RelightProducerResolver
from photo_worker.operations.portrait.relight_admission import QualifiedRelightAdmission,LeaseGatedRelightResolver
from photo_worker.operations.portrait.relight_depth_authority import SQLRelightDepthAuthority
from photo_worker.operations.portrait.relight_manual_authority import SQLRelightManualOrigin,RelightManualMaskAuthority
from test_v28_relight_manual import seed_manual_inputs
from test_v28_relight_depth import seed_depth_inputs
from test_v28_relight_lease import relight_lease_fixture
from test_v25_lens_objectstore import lens_objects


@pytest.mark.parametrize('installed',[False,True])
def test_composed_live_relight_authority_and_revoked_qualification(database,lens_objects,tmp_path,installed,record_property):
    client,bucket=lens_objects;manual_data={};depth_data={};locators={};source_data={}
    runtime_sha='b'*64;runtime_lock=None
    if installed:
        import sys,platform
        from importlib import metadata
        from photo_worker.operations.portrait.relight_runtime_lock import RelightRuntimeLock,installed_component_hashes
        runtime_lock=RelightRuntimeLock(schema_version=1,python_version='.'.join(map(str,sys.version_info[:3])),
            implementation=platform.python_implementation(),machine=platform.machine(),
            packages={name:metadata.version(name) for name in ('numpy','pydantic','rfc8785','opencv-python-headless')},
            components=installed_component_hashes())
        runtime_sha=canonical_hash(runtime_lock)
    record_property('relight_runtime_sha',runtime_sha)
    record_property('actual_installed_runtime',str(installed).lower())
    record_property('qualification_evidence','synthetic isolated test catalog; not image quality acceptance')
    migrations=sorted((Path(__file__).resolve().parents[2]/'backend/db/migrations').glob('*.sql'))
    record_property('migration_head',migrations[-1].name)
    record_property('migration_files_sha',canonical_hash({p.name:sha256(p.read_bytes()).hexdigest() for p in migrations}))
    calibration=Calibration(**dict(schema_version=1,model_lock_sha='a'*64,dataset_sha='d'*64,qualified=True,
        scene_cosine_min=.3,scene_log_luma_max=1.,cluster_diameter_max=1.,sharpness_scale=1.,clipping_penalty=1.,match_inlier_min=.3,match_error_px_max=5.,identity_similarity_min=.4,landmark_drift_max=.2,normalization_scales=[1.]*6,loss_weights=[.4,.1,.1,.1,.2,.1],focus_warning=.5,focus_critical=.2,eye_open_min=.5,glare_warning_fraction=.1,clipping_warning_fraction=.1,clipping_critical_fraction=.3,min_face_edge_px=64,depth_sigma=.1,registration_error_px=2.,motion_threshold=.1))
    def source_builder(artifact,original_sha):
        raw=json.loads((Path(__file__).resolve().parents[2]/'backend/internal/review/testdata/working-images-python.json').read_text())[0]
        raw['canvas'].update(width=5,height=5,source_sha256=original_sha,space_id='oriented')
        stream=BytesIO();np.save(stream,np.full((5,5,3),.4,np.float32),allow_pickle=False);payload=stream.getvalue()
        aid=artifact(sha256(payload).hexdigest(),media='application/octet-stream',role='working_rgb',body=payload)
        raw['pixels']=dict(artifact_id=str(aid),sha256=sha256(payload).hexdigest());raw['alpha']=None
        source_data['value']=WorkingImage.model_validate_json(canonical_bytes(raw));return canonical_bytes(raw)
    def publish(aid,raw,role):
        key='relight-composed/'+str(aid);version=client.put_object(bucket,key,BytesIO(raw),len(raw)).version_id
        locators[aid]=(key,version,len(raw));return bucket,key,version
    def setup(db,tenant,scope):
        return scope['source_inputs']+seed_manual_inputs(db,tenant,scope,manual_data,physical=True,publish=publish)+seed_depth_inputs(db,tenant,scope,depth_data,objects=(client,bucket),physical=True,publish=publish)
    def build(pin,scope):return canonical_bytes(dict(schema_version=1,base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash=canonical_hash(source_data['value'].canvas)),depth=scope['depth_ref'],subject=scope['subject_ref'],protected=scope['protected_ref'],calibration=pin['calibration'],strength=.7,lights=[dict(x=.5,y=.5,radius=.2,ev=1.)]))
    from photo_worker.operations.portrait.local_resources import local_portrait_resources
    with psycopg.connect(**database) as db:
        args,scope,pin=relight_lease_fixture(db,qualified=True,objects=(client,bucket),calibration_raw=canonical_bytes(calibration),params_builder=build,extra_inputs_factory=setup,source_builder=source_builder,execution_resource_estimate=local_portrait_resources(5,5),runtime_sha=runtime_sha)
        input_evidence={}
        profile=db.execute('SELECT execution_profile FROM photo.tasks WHERE id=%s',(args[2],)).fetchone()[0]
        refs=[ArtifactRef(artifact_id=r[0],sha256=r[1]) for r in db.execute('SELECT artifact_id,sha256 FROM photo.v2_task_inputs WHERE task_id=%s',(args[2],))]
        for ref in refs:
            input_evidence[ref.artifact_id]=db.execute('SELECT media_type,bytes,output_role FROM photo.artifacts WHERE id=%s',(ref.artifact_id,)).fetchone()
            if ref.artifact_id not in locators:
                row=db.execute('SELECT object_key,object_version,bytes,bucket FROM photo.artifacts WHERE id=%s',(ref.artifact_id,)).fetchone()
                if row[3]==bucket:locators[ref.artifact_id]=row[:3]
    params=ImageRelightParams.model_validate_json(args[8])
    command=SimpleNamespace(tenant_id=args[0],run_id=args[1],task_id=args[2],attempt=1,operation='image.relight',operation_version='1',resource_class='cpu',params=ArtifactRef(artifact_id=args[6],sha256=args[7]),inputs=refs)
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(command=command,fence=1,worker_id=args[5],input_digest=args[9],execution_profile=profile))
    staged={};stage_bytes={}
    class Artifacts:
        def params(self,model):return params
        def contract_bytes(self,ref,model):
            assert ref in refs
            key,version,size=locators[ref.artifact_id];response=client.get_object(bucket,key,version_id=version)
            try:raw=response.read(size+1)
            finally:response.close();response.release_conn()
            assert len(raw)==size and sha256(raw).hexdigest()==ref.sha256
            return raw
        def contract(self,ref,model):return model.model_validate_json(self.contract_bytes(ref,model))
        def stage(self,path,reservation,role,mime):
            reservation.validate();raw=path.read_bytes();ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
            key='relight-output/'+str(ref.artifact_id);version=client.put_object(bucket,key,BytesIO(raw),len(raw),content_type=mime).version_id
            staged[role]=ref;stage_bytes[role]=(key,version,raw);return ref
        def stage_contract(self,value,role):
            raw=canonical_bytes(value);ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
            key='relight-output/'+str(ref.artifact_id);version=client.put_object(bucket,key,BytesIO(raw),len(raw),content_type='application/json').version_id
            staged[role]=ref;stage_bytes[role]=(key,version,raw);return ref

        @contextmanager
        def input(self,ref,suffix):
            path=tmp_path/(str(ref.artifact_id)+suffix);path.write_bytes(self.contract_bytes(ref,None));yield SimpleNamespace(path=path)
    # Attest the isolated runtime identity before any expected negative outcome.
    with worker_login(make_conninfo(**database),args[2]) as runtime_dsn:
        artifacts=Artifacts();dbport=TaskDatabase(runtime_dsn)
        dbport.ping()
        manual=RelightManualMaskAuthority(artifacts,SQLRelightManualOrigin(dbport,artifacts))
        masks={('manual',False):lambda *args:manual.verify(*args,protected=False),('manual',True):lambda *args:manual.verify(*args,protected=True)}
        resolver=RelightProducerResolver(dbport,client,bucket,runtime_sha,SQLRelightDepthAuthority(dbport,artifacts,objects=client,bucket=bucket),masks)
        admission=QualifiedRelightAdmission(LeaseGatedRelightResolver(dbport,resolver),runtime_sha)
        documents=(params,source_data['value'],RelativeDepth.model_validate_json(depth_data['body']),MaskRef.model_validate_json(manual_data['target_raw']),MaskRef.model_validate_json(manual_data['guard_raw']),calibration)
        admission.admit(ctx,*documents)
        admission.admit(ctx,*documents)
        from photo_worker.runtime.scratch import ScratchSpace
        from photo_worker.operations.portrait.relight_adapter import execute
        with ScratchSpace(tmp_path,uuid4(),uuid4(),1,profile['resource_estimate']['scratch_bytes'],min_free_bytes=0) as scratch:
            artifacts.scratch=scratch
            result=execute(ctx,artifacts,admission)
            assert result.review_required and result.outputs==[staged['document']]
            assert result.bytes_written==sum(len(value[2]) for value in stage_bytes.values())
            assert not list(scratch.directory.iterdir())
        rgb=np.load(BytesIO(stage_bytes['pixels'][2]),allow_pickle=False)
        changed=np.load(BytesIO(stage_bytes['mask_pixels'][2]),allow_pickle=False)
        guard=np.load(BytesIO(manual_data['raw_objects'][manual_data['pins']['mask_protected'].artifact_id]),allow_pickle=False)
        assert np.any(rgb>.4)
        np.testing.assert_array_equal(rgb[guard>0],np.full_like(rgb[guard>0],.4))
        np.testing.assert_array_equal(changed,np.any(rgb!=np.float32(.4),axis=-1).astype(np.float32))
        for key,version,raw in stage_bytes.values():
            response=client.get_object(bucket,key,version_id=version)
            try:assert response.read()==raw
            finally:response.close();response.release_conn()
        from photo_worker.v2.adoption import ArtifactEvidence
        from photo_worker.v2.lens_bypass import _ref
        from photo_worker.v2.relight_adoption import verify_relight_output_arrays
        by_output={ref.artifact_id:role for role,ref in staged.items()}
        class AdoptionReader:
            def staged_outputs(self):return {_ref(ref) for ref in staged.values()}
            def evidence(self,ref):
                if ref.artifact_id in by_output:
                    role=by_output[ref.artifact_id];raw=stage_bytes[role][2]
                    return ArtifactEvidence(ref,'application/x-npy' if role in ('pixels','mask_pixels') else 'application/json',len(raw),role,'output')
                if ref==_ref(command.params):return ArtifactEvidence(ref,'application/json',len(args[8].encode()),None,'input')
                media,size,role=input_evidence[ref.artifact_id]
                return ArtifactEvidence(ref,media,size,role,'input')
            def raw(self,ref):
                if ref==_ref(command.params):return args[8].encode()
                if ref.artifact_id in by_output:
                    key,version,expected=stage_bytes[by_output[ref.artifact_id]]
                    response=client.get_object(bucket,key,version_id=version)
                    try:return response.read(len(expected)+1)
                    finally:response.close();response.release_conn()
                return artifacts.contract_bytes(ArtifactRef(artifact_id=ref.artifact_id,sha256=ref.sha256),None)
            def read_json(self,ref,maximum):
                raw=self.raw(ref);assert len(raw)<=maximum;return raw
            def iter_bytes(self,ref,maximum):
                raw=self.raw(ref)
                for offset in range(0,len(raw),65536):yield raw[offset:offset+65536]
        adopted_params,adopted_result=verify_relight_output_arrays(AdoptionReader(),_ref(command.params),_ref(staged['document']),scratch_root=tmp_path,max_bytes=1048576,check_active=ctx.check_active)
        assert adopted_params==params and adopted_result.working_image==staged['working_image']
        assert not list(tmp_path.glob('adoption-*'))
        from photo_worker.v2.relight_replay import verify_relight_replay
        replay_calls=[]
        def replay_authority(*documents):
            replay_calls.append(1);admission.admit(ctx,*documents);return True
        replay_params,replay_result=verify_relight_replay(AdoptionReader(),_ref(command.params),_ref(staged['document']),scratch_root=tmp_path,max_bytes=1048576,max_scratch_bytes=profile['resource_estimate']['scratch_bytes'],execution_profile=profile,verify_authority=replay_authority,check_active=ctx.check_active)
        assert replay_params==params and replay_result==adopted_result and len(replay_calls)==2
        from photo_worker.v2.relight_replay import RelightReplayContext
        from photo_worker.v2.adoption import prepare_v2_adoption
        replay_context=RelightReplayContext(tmp_path,1048576,profile['resource_estimate']['scratch_bytes'],profile,replay_authority)
        prepared=prepare_v2_adoption(task_id=command.task_id,operation='image.relight',version='1',root=_ref(staged['document']),reader=AdoptionReader(),params_ref=_ref(command.params),relight_context=replay_context,check_active=ctx.check_active)
        assert prepared.inventory and len(prepared.digest)==64

        # A hostile worker can change editable pixels and recompute every JSON/hash.
        # Safe-delta invariants still pass; independent replay must detect the edit.
        forged=rgb.copy();y,x=np.argwhere(changed==1)[0];forged[y,x,0]+=np.float32(.001)
        stream=BytesIO();np.save(stream,forged,allow_pickle=False);forged_pixels=stream.getvalue()
        forged_pixel_ref=ArtifactRef(artifact_id=staged['pixels'].artifact_id,sha256=sha256(forged_pixels).hexdigest())
        forged_image=json.loads(stage_bytes['working_image'][2]);forged_image['pixels']=forged_pixel_ref.model_dump(mode='json')
        forged_image_raw=canonical_bytes(forged_image)
        forged_image_ref=ArtifactRef(artifact_id=staged['working_image'].artifact_id,sha256=sha256(forged_image_raw).hexdigest())
        forged_document=json.loads(stage_bytes['document'][2]);forged_document['working_image']=forged_image_ref.model_dump(mode='json')
        forged_document_raw=canonical_bytes(forged_document)
        forged_root=ArtifactRef(artifact_id=staged['document'].artifact_id,sha256=sha256(forged_document_raw).hexdigest())
        forged_bodies={forged_pixel_ref.artifact_id:forged_pixels,forged_image_ref.artifact_id:forged_image_raw,forged_root.artifact_id:forged_document_raw}
        class ForgedReader(AdoptionReader):
            def raw(self,ref):return forged_bodies.get(ref.artifact_id) or super().raw(ref)
        verify_relight_output_arrays(ForgedReader(),_ref(command.params),_ref(forged_root),scratch_root=tmp_path,max_bytes=1048576,check_active=ctx.check_active)
        with pytest.raises(ValueError,match='RELIGHT_REPLAY_PIXEL_MISMATCH'):
            prepare_v2_adoption(task_id=command.task_id,operation='image.relight',version='1',root=_ref(forged_root),reader=ForgedReader(),params_ref=_ref(command.params),relight_context=replay_context,check_active=ctx.check_active)

        with pytest.raises(ValueError,match='RELIGHT_REPLAY_PIXEL_MISMATCH'):
            verify_relight_replay(ForgedReader(),_ref(command.params),_ref(forged_root),scratch_root=tmp_path,max_bytes=1048576,max_scratch_bytes=profile['resource_estimate']['scratch_bytes'],execution_profile=profile,verify_authority=replay_authority,check_active=ctx.check_active)

        assert not list(tmp_path.glob('adoption-*'))

        for artifact in (depth_data['root'],manual_data['pins']['mask_protected'].artifact_id):
            with psycopg.connect(**database) as db:db.execute('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(artifact,))
            expected='RELIGHT_DEPTH_ORIGIN_INVALID' if artifact==depth_data['root'] else 'RELIGHT_MANUAL_ORIGIN_INVALID'
            with pytest.raises(OperationFailure,match='^'+expected+'$'):admission.admit(ctx,*documents)
            with psycopg.connect(**database) as db:db.execute('UPDATE photo.artifacts SET produced_fence=1 WHERE id=%s',(artifact,))
            admission.admit(ctx,*documents)
        with psycopg.connect(**database) as db:db.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],))
        with pytest.raises(AdoptionInvalid,match='^RELIGHT_QUALIFICATION_SCOPE_STALE$'):admission.admit(ctx,*documents)

        before=dict(staged)
        with ScratchSpace(tmp_path,uuid4(),uuid4(),2,profile['resource_estimate']['scratch_bytes'],min_free_bytes=0) as scratch:
            artifacts.scratch=scratch
            with pytest.raises(OperationFailure,match='INPUT_INVALID'):execute(ctx,artifacts,admission)
            assert not list(scratch.directory.iterdir())
        assert staged==before

        # Restore fixture policy, complete the actual staged root, and replay without worker authority.
        from test_v28_relight_adoption_source import complete_relight_task
        from photo_worker.v2.relight_adoption_authority import CompletedRelightAuthority
        from photo_worker.v2.relight_adoption_qualification import verify_completed_relight_qualification
        with psycopg.connect(**database) as db:
            db.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=%s',(pin['policy_id'],))
            db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING',(command.tenant_id,))
            key,version,raw=stage_bytes['document']
            result_id,_=complete_relight_task(db,args,root_id=staged['document'].artifact_id,
                root_body=raw,root_location=(bucket,key,version),review_required=True)
            for role,ref in staged.items():
                if role=='document':continue
                key,version,raw=stage_bytes[role];media='application/x-npy' if role in ('pixels','mask_pixels') else 'application/json'
                db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp(),%s,1,1)",
                    (ref.artifact_id,command.tenant_id,bucket,key,version,ref.sha256,len(raw),media,role,command.task_id))
                db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES(%s,%s,1,1,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,'ready',%s)",
                    (command.tenant_id,command.task_id,ctx.lease.worker_id,'a'*64,uuid4(),ref.artifact_id,role,bucket,key,ref.sha256,len(raw),media,version))
        completed=SimpleNamespace(attempt=1,fence=1,result_id=result_id,root=_ref(staged['document']))
        with psycopg.connect(**database,autocommit=True) as scheduler:
            scheduler.execute('SET ROLE photo_scheduler')
            from photo_worker.v2.adoption_store import PostgresObjectEvidenceReader
            scoped_reader=PostgresObjectEvidenceReader(scheduler,client,tenant_id=command.tenant_id,
                task_id=command.task_id,attempt=1,fence=1,allowed_bucket=bucket)
            with pytest.raises(ValueError,match='ARTIFACT_EVIDENCE_MISMATCH'):
                scoped_reader.evidence(_ref(ArtifactRef(artifact_id=uuid4(),sha256='a'*64)))
            def completed_qualification(evidence,params,calibration):
                return verify_completed_relight_qualification(scheduler,scoped_reader,command.tenant_id,
                    command.task_id,completed,params,calibration,expected_runtime_sha=runtime_sha,check_active=ctx.check_active)
            authority=CompletedRelightAuthority(scheduler,scoped_reader,command.tenant_id,command.task_id,completed,
                scratch_root=tmp_path,max_bytes=1048576,verify_qualification=completed_qualification,check_active=ctx.check_active)
            completed_replay=RelightReplayContext(tmp_path,1048576,profile['resource_estimate']['scratch_bytes'],profile,authority)
            prepared=prepare_v2_adoption(task_id=command.task_id,operation='image.relight',version='1',
                root=_ref(staged['document']),reader=scoped_reader,params_ref=_ref(command.params),
                relight_context=completed_replay,check_active=ctx.check_active)
            assert prepared.inventory and len(prepared.digest)==64
            with pytest.raises(ValueError,match='RELIGHT_REPLAY_PIXEL_MISMATCH'):
                prepare_v2_adoption(task_id=command.task_id,operation='image.relight',version='1',
                    root=_ref(forged_root),reader=ForgedReader(),params_ref=_ref(command.params),
                    relight_context=completed_replay,check_active=ctx.check_active)
            from psycopg.types.json import Jsonb
            bodies=(args[8],depth_data['body'].decode(),manual_data['target_raw'].decode(),manual_data['guard_raw'].decode(),manual_data['root_raw'].decode(),manual_data['root_raw'].decode())
            evidence=scheduler.execute('SELECT photo.v28_relight_adoption_evidence('+','.join(['%s']*11)+')',
                (command.tenant_id,command.task_id,1,1,result_id,*bodies)).fetchone()[0]
            def commit():
                return scheduler.execute('SELECT photo.adopt_v28_relight('+','.join(['%s']*14)+')',
                    (command.tenant_id,command.task_id,1,1,result_id,Jsonb(list(prepared.inventory)),prepared.digest,canonical_bytes(evidence).decode(),*bodies)).fetchone()[0]
            with scheduler.transaction(force_rollback=True):
                assert commit() is True
            def publication_snapshot():
                # Owner readback only; actual adoption retains scheduler ACL.
                with psycopg.connect(**database) as readback:
                    identity=(command.tenant_id,command.task_id,result_id)
                    return dict(
                        run=readback.execute('SELECT status,revision FROM photo.runs WHERE tenant_id=%s AND id=%s',(command.tenant_id,command.run_id)).fetchall(),
                        task=readback.execute('SELECT state,attempt,fencing_token,canonical_result_id FROM photo.tasks WHERE tenant_id=%s AND id=%s',(command.tenant_id,command.task_id)).fetchall(),
                        reviews=readback.execute('SELECT id,kind,relight_params_jcs,candidate_artifact_id,base_artifact_id FROM photo.reviews WHERE tenant_id=%s AND task_id=%s ORDER BY id',(command.tenant_id,command.task_id)).fetchall(),
                        events=readback.execute('SELECT cursor,type,payload FROM photo.run_events WHERE tenant_id=%s AND run_id=%s ORDER BY cursor',(command.tenant_id,command.run_id)).fetchall(),
                        outbox=readback.execute("SELECT event_id,routing_key,payload FROM photo.outbox WHERE tenant_id=%s AND (event_id=photo.v2_adoption_event_id(%s,%s) OR (routing_key='review.created' AND payload->>'run_id'=%s)) ORDER BY event_id",(*identity,str(command.run_id))).fetchall(),
                        inbox=readback.execute("SELECT consumer,event_id FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(%s,%s)",identity).fetchall())
            if installed:
                from photo_worker.v2.adoption_service import AdoptionCoordinator
                from photo_worker.v2.relight_adoption_prepare import RelightAdoptionInstallation
                installation=RelightAdoptionInstallation(runtime_lock,runtime_sha,tmp_path,1048576,profile['resource_estimate']['scratch_bytes'])
                with psycopg.connect(**database,autocommit=True) as writer:
                    writer.execute('SET ROLE photo_scheduler')
                    coordinator=AdoptionCoordinator(scheduler,writer,client,bucket,relight_installation=installation)
                    assert coordinator.adopt(command.tenant_id,command.task_id) is True
                    committed=publication_snapshot()
                    # Published waiting_review is no longer a pending context.
                    with pytest.raises(AdoptionInvalid,match='^V2_ADOPTION_CONTEXT_STALE$'):
                        coordinator.adopt(command.tenant_id,command.task_id)
                    assert publication_snapshot()==committed
            else:
                assert commit() is True
            committed=publication_snapshot()
            assert committed['run'][0][0]=='waiting_review'
            assert len(committed['reviews'])==1
            assert committed['reviews'][0][1:3]==('image.relight',args[8])
            assert sum(row[1]=='v2.task.adopted' for row in committed['events'])==1
            assert sum(row[1]=='v2.run.waiting_review' for row in committed['events'])==1
            assert sum(row[1]=='review.created' for row in committed['outbox'])==1
            assert len(committed['outbox'])==2 and len(committed['inbox'])==1
            assert scheduler.execute('SELECT count(*) FROM photo.v2_pending_adoptions() WHERE tenant_id=%s AND task_id=%s',(command.tenant_id,command.task_id)).fetchone()==(0,)
            # Retain the exact original proof-bearing SQL inventory/body replay.
            assert commit() is False
            assert publication_snapshot()==committed
        with psycopg.connect(**database) as db:
            assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.task.adopted' AND payload->>'task_id'=%s",(command.tenant_id,command.run_id,str(command.task_id))).fetchone()[0]==1
            assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND event_id=photo.v2_adoption_event_id(%s,%s)",(command.tenant_id,command.task_id,result_id)).fetchone()[0]==1
            assert db.execute("SELECT count(*) FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(%s,%s)",(command.tenant_id,command.task_id,result_id)).fetchone()[0]==1
        assert not list(tmp_path.glob('adoption-*'))
