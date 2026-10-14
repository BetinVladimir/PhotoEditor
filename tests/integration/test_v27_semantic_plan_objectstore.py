"""Typed plan adoption over real PostgreSQL/MinIO; synthetic unsupported producer.

This tests assembly acceptance, not GPU qualification or transfer execution.
"""
from io import BytesIO
from hashlib import sha256
import json
from uuid import uuid4

import numpy as np
import psycopg
from psycopg.types.json import Jsonb
import pytest
from minio.error import S3Error

from test_database import tenant
from test_v25_lens_objectstore import lens_objects,ROOT
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.contracts.semantic import EditIntent,SemanticTransferParams,TransferCandidate,V2SemanticAssembleParams
from photo_worker.operations.session.contracts import Calibration,InputSnapshot,SnapshotItem,VersionBinding
from photo_worker.operations.semantic.assemble import assemble_semantic_plan
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.adoption_service import AdoptionCoordinator
from photo_worker.v2.semantic_snapshot_authority import semantic_snapshot_authority
from photo_worker.v2.semantic_producer_authority import adopted_semantic_producer_authority


def test_semantic_plan_exact_versions_live_authorities_and_atomic_adoption(lens_objects,database):
    objects,bucket=lens_objects;records={}
    def publish(raw,media='application/json',role=None,producer=None):
        ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest());key=str(ref.artifact_id)
        version=objects.put_object(bucket,key,BytesIO(raw),len(raw),content_type=media).version_id
        assert version
        records[ref.artifact_id]=(ref,raw,media,role,producer,key,version)
        return ref
    def document(value,**kwargs):return publish(canonical_bytes(value),**kwargs)
    image=WorkingImage.model_validate_json(json.dumps(json.loads((ROOT/'backend/internal/review/testdata/working-images-python.json').read_text())[0]))
    encoded=BytesIO();np.save(encoded,np.zeros((image.canvas.height,image.canvas.width,3),dtype='<f4'),allow_pickle=False)
    image.pixels=publish(encoded.getvalue(),'application/x-npy')
    source=document(image,role='working_image')
    encoded=BytesIO();np.save(encoded,np.zeros((image.canvas.height,image.canvas.width),dtype='<f4'),allow_pickle=False)
    mask_pixels=publish(encoded.getvalue(),'application/x-npy')
    selected=document(MaskRef(artifact=mask_pixels,canvas=image.canvas,transform_chain=image.transform_chain,
        object_id=None,selector='manual',confidence=1,model_lock_id=None,protected=False))
    fixtures=json.loads((ROOT/'docs/implementation-v2/contracts/structural-fixtures.json').read_text())['fixtures']
    calibration_values=dict(fixtures['Calibration'],qualified=True,loss_weights=[1/6]*6)
    calibration=Calibration.model_validate_json(json.dumps(calibration_values))
    calibration_ref=document(calibration)
    original=publish(b'synthetic-original','image/jpeg')
    run,workflow,producer,assembly,snapshot_id,version_id=[uuid4() for _ in range(6)]
    binding=VersionBinding(asset_version_id=version_id,source=source,canvas_hash=canonical_hash(image.canvas))
    intent=EditIntent(schema_version=1,kind='local_exposure',reference=binding,reference_mask=selected,
        target_snapshot_id=snapshot_id,strength=.5,ev=.3,preserve_persons=True,prompt=None)
    intent_ref=document(intent)
    transfer=SemanticTransferParams(schema_version=1,intent=intent_ref,reference=binding,target=binding,
        model_lock_sha=calibration.model_lock_sha,calibration=calibration_ref)
    transfer_ref=document(transfer)
    candidate=TransferCandidate(schema_version=1,target=binding,mask=None,transform=None,confidence=0,
        state='unsupported',reason='NO_PLANAR_CORRESPONDENCE')
    candidate_ref=document(candidate,role='document',producer=producer)
    params=V2SemanticAssembleParams(schema_version=1,intent=intent_ref,candidates=[candidate_ref],
        model_lock_sha=calibration.model_lock_sha,calibration=calibration_ref)
    params_ref=document(params)
    snapshot=InputSnapshot(schema_version=1,snapshot_id=snapshot_id,snapshot_digest='0'*64,
        items=[SnapshotItem(ordinal=0,binding=binding,working_image=source,recipe=intent_ref,captured_at=None,camera_hash=None)])
    snapshot.snapshot_digest=canonical_hash(snapshot.model_dump(mode='json',exclude={'snapshot_digest'}))
    snapshot_ref=document(snapshot)
    plan=assemble_semantic_plan(params,intent,snapshot,calibration,[candidate])
    root=document(plan,role='document',producer=assembly)
    transports={}
    for task,entries in [(producer,[(transfer_ref,'Params'),(intent_ref,'EditIntent'),(calibration_ref,'Calibration'),(source,'WorkingImage'),(selected,'MaskRef'),(image.pixels,'NpyArray'),(mask_pixels,'NpyArray')]),
                         (assembly,[(params_ref,'Params'),(intent_ref,'EditIntent'),(calibration_ref,'Calibration'),(source,'WorkingImage'),(candidate_ref,'TransferCandidate'),(snapshot_ref,'InputSnapshot')])]:
        entries=sorted(entries,key=lambda item:item[0].artifact_id.bytes)
        values=[dict(ref=ref.model_dump(mode='json'),contract=contract) for ref,contract in entries]
        digest=canonical_hash(values)
        chunk=document(dict(schema_version=1,entries=values))
        manifest=document(dict(schema_version=1,task_id=str(task),entry_count=len(entries),entries_sha=digest,chunks=[chunk.model_dump(mode='json')]))
        transports[task]=(entries,chunk,manifest,digest)
    with psycopg.connect(**database) as admin:
        tid,actor=tenant(admin,str(uuid4())+'@example.test')
        project=admin.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Semantic S3 plan') RETURNING id",(tid,)).fetchone()[0]
        session=admin.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') RETURNING id",(actor,uuid4().bytes)).fetchone()[0]
        for ref,raw,media,role,owner,key,version in records.values():
            admin.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp(),%s,%s,%s,%s)",(ref.artifact_id,tid,bucket,key,ref.sha256,len(raw),media,version,role,owner,1 if owner else None,1 if owner else None))
        asset=admin.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tid,project,original.artifact_id)).fetchone()[0]
        admin.execute("INSERT INTO photo.asset_versions(id,tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,%s,'{}','accepted')",(version_id,tid,asset,source.artifact_id,source.sha256))
        admin.execute("INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES(%s,%s,'v2','user',%s,%s,1,clock_timestamp()+interval '1 hour')",(snapshot_id,tid,actor,snapshot.snapshot_digest))
        admin.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,0,%s,%s,%s,'{}')",(tid,snapshot_id,version_id,source.artifact_id,source.sha256))
        admin.execute('UPDATE photo.snapshots SET expires_at=NULL WHERE id=%s',(snapshot_id,))
        from semantic_qualification_fixture import frozen_qualification
        qualification=frozen_qualification(admin,tid,project,actor,objects=lens_objects,calibration_ref=calibration_ref.model_dump(mode='json'),model_lock_sha=calibration.model_lock_sha,dataset_sha=calibration.dataset_sha)
        config=dict(project_id=str(project),input_snapshot_id=str(snapshot_id),v27_semantic=dict(auth_session_id=str(session),snapshot=snapshot_ref.model_dump(mode='json'),snapshot_digest=snapshot.snapshot_digest,qualification=qualification))
        admin.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES(%s,%s,'v2','running',%s,%s,%s,%s,'fail_fast',false)",(run,tid,snapshot_id,Jsonb(config),canonical_hash(config),actor))
        admin.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'semantic','v2.2',%s)",(workflow,tid,project,run,snapshot_id,calibration_ref.sha256))
        for task,operation,params_pin,output in [(producer,'semantic.transfer',transfer_ref,candidate_ref),(assembly,'v2.semantic.assemble',params_ref,root)]:
            entries,chunk,manifest,digest=transports[task];backend='cuda' if task==producer else 'cpu'
            profile=dict(renderer_version='v2.2',backend=backend,contracts_sha='4004dd5e69d67835b7d295bfcebded5c006125256c2f6047fd11b2f2dca0f1b4',v2_input_manifest=manifest.model_dump(mode='json'),v2_entry_count=len(entries),v2_entries_sha=digest)
            admin.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,%s,%s,'1',%s,%s,%s,%s,%s,1,1)",(task,tid,run,operation+':0',operation,'gpu' if task==producer else 'cpu',Jsonb([manifest.model_dump(mode='json')]),params_pin.artifact_id,digest,Jsonb(profile)))
            for ordinal,(ref,contract) in enumerate([*entries,(chunk,'InputClosureChunk'),(manifest,'InputClosureManifest')]):
                admin.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,%s,%s)',(tid,project,task,ref.artifact_id,ref.sha256,contract,ordinal))
            worker=uuid4();admin.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),%s,%s)",(tid,task,worker,backend,Jsonb(profile)))
            _,raw,media,role,_,key,version=records[output.artifact_id]
            admin.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES(%s,%s,1,1,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,'ready',%s)",(tid,task,worker,'a'*64,uuid4(),output.artifact_id,role,bucket,key,output.sha256,len(raw),media,version))
            result=admin.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tid,task,Jsonb([output.model_dump(mode='json')]),canonical_hash([output.model_dump(mode='json')]))).fetchone()[0]
            admin.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
            if task==producer:admin.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',photo.v2_adoption_event_id(%s,%s),clock_timestamp())",(tid,task,result))
        admin.execute('UPDATE photo.runs SET graph_sealed=true WHERE id=%s',(run,));admin.commit()
        # Replace current JSON versions; catalog pins must still select originals.
        for _,_,media,_,_,key,_ in records.values():
            if media=='application/json':objects.put_object(bucket,key,BytesIO(b'not-json'),8,content_type=media)
        with psycopg.connect(**database,autocommit=True) as read,psycopg.connect(**database) as write:
            read.execute('SET ROLE photo_scheduler');write.execute('SET ROLE photo_scheduler');write.commit()
            coordinator=AdoptionCoordinator(read,write,objects,bucket,semantic_plan_snapshot=semantic_snapshot_authority(read),
                semantic_plan_provenance=adopted_semantic_producer_authority(read,objects,bucket))
            admin.execute('UPDATE photo.projects SET archived_at=clock_timestamp() WHERE id=%s',(project,));admin.commit()
            with pytest.raises(AdoptionInvalid,match='SEMANTIC_SNAPSHOT_SCOPE_STALE'):coordinator.adopt(tid,assembly)
            assert admin.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND type='v2.task.adopted'",(tid,)).fetchone()==(0,)
            admin.execute('UPDATE photo.projects SET archived_at=NULL WHERE id=%s',(project,));admin.commit()
            assert coordinator.adopt(tid,assembly)
            assert not coordinator.adopt(tid,assembly)
            assert admin.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND type='v2.task.adopted'",(tid,)).fetchone()==(1,)
            # Loss of the exact frozen version never falls back to current bytes.
            _,_,_,_,_,key,old_version=records[root.artifact_id]
            objects.remove_object(bucket,key,version_id=old_version)
            with pytest.raises(S3Error) as missing:
                coordinator.adopt(tid,assembly)
            assert missing.value.code in ('NoSuchKey','NoSuchVersion')
            assert admin.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND type='v2.task.adopted'",(tid,)).fetchone()==(1,)
