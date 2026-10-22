"""Real immutable intent PUT/replay; source/mask metadata is synthetic."""
from specialized_go_child_evidence import run_named_go
from io import BytesIO
import json
import os
import subprocess
from pathlib import Path
from uuid import UUID,uuid4
from hashlib import sha256
import pytest
import numpy as np
from psycopg.types.json import Jsonb
import psycopg
from psycopg.conninfo import make_conninfo
from layer_original_fixture import seed_original_composition
from semantic_qualification_fixture import frozen_qualification
from test_v25_lens_objectstore import lens_objects
ROOT=Path(__file__).resolve().parents[2]

def test_ordinary_scene_full_parent_and_transfer_tail(database,lens_objects,monkeypatch):
    """Dedicated source-linked path; no external ready-parent fixture required."""
    monkeypatch.setenv('PHOTO_TEST_V27_ORIGINAL_PERSON','1')
    test_semantic_intent_publication_old_version_and_replay(database,lens_objects,True,'native-scene-tail')

def test_legacy_ordinary_unsealed_recovery(database,lens_objects):
    test_semantic_intent_publication_old_version_and_replay(database,lens_objects,True,'legacy-ordinary-recovery')

@pytest.mark.parametrize("wrapper,race",[(False,False),(True,False),(True,True),(True,"source"),(True,"batch"),(True,"fresh"),(True,"partial"),(True,"registration-revoke"),(True,"oriented"),(True,"external"),(True,"manual-lock"),(True,"manual-lock-go"),(True,"manual-lock-go-disjoint"),(True,"manual-races"),(True,"native-bootstrap"),(True,"ordinary-admission"),(True,"create-service"),(True,"create-person"),(True,"create-person-uncovered"),(True,"native-face-wire"),(True,"native-person-defect-recovery"),(True,"native-person-defect-wire"),(True,"native-terminal-cancel"),(True,"native-terminal-job-detector"),(True,"native-terminal-job-person"),(True,"native-terminal-job-person-draft"),(True,"native-terminal-worker-person"),(True,"native-terminal-worker-detector"),(True,"native-scene-wire")],ids=["publisher","proposal-wrapper","revoke-during-put","source-during-put","transfer-batch","preparer-fresh","preparer-partial","preparer-registration-revoke","transfer-batch-oriented","transfer-batch-external","manual-lock-external","manual-lock-go-external","manual-lock-go-disjoint-external","manual-races-external","native-bootstrap","ordinary-admission","create-service","create-person","create-person-uncovered","native-face-wire","native-person-defect-recovery","native-person-defect-wire","native-terminal-cancel","native-terminal-job-detector","native-terminal-job-person","native-terminal-job-person-draft","native-terminal-worker-person","native-terminal-worker-detector","native-scene-wire"])
def test_semantic_intent_publication_old_version_and_replay(database,lens_objects,wrapper,race):
    native_terminal_worker=race in ("native-terminal-worker-person","native-terminal-worker-detector")
    native_terminal_draft=race=="native-terminal-job-person-draft"
    native_terminal_job=race in ("native-terminal-job-detector","native-terminal-job-person","native-terminal-job-person-draft","native-terminal-worker-person","native-terminal-worker-detector")
    native_terminal_person=race in ("native-terminal-job-person","native-terminal-job-person-draft","native-terminal-worker-person")
    native_terminal_cancel=race=="native-terminal-cancel"
    commit_ordinary=race=="ordinary-admission"
    create_ordinary=race in ("create-service","create-person","create-person-uncovered")
    person_wire=race=="create-person-uncovered"
    create_person=race in ("create-person","create-person-uncovered")
    person_defect_execution=race=="native-person-defect-wire"
    person_defect_recovery=race in ("native-person-defect-recovery","native-person-defect-wire")
    face_wire=race in ("native-face-wire","native-person-defect-recovery","native-person-defect-wire")
    native_bootstrap=race in ("native-terminal-worker-person","native-terminal-worker-detector","native-terminal-job-person-draft","native-terminal-job-detector","native-terminal-job-person","native-terminal-cancel","native-bootstrap","native-face-wire","native-person-defect-recovery","native-person-defect-wire")
    race_only=race=="manual-races"
    native_disjoint=race=="manual-lock-go-disjoint"
    go_lock_only=race in ("manual-lock-go","manual-lock-go-disjoint")
    lock_only=race=="manual-lock"
    external_vectors=race in ("external","manual-lock","manual-lock-go","manual-lock-go-disjoint","manual-races")
    oriented=race in ("native-terminal-worker-person","native-terminal-worker-detector","native-terminal-job-person-draft","native-terminal-job-detector","native-terminal-job-person","native-terminal-cancel","native-bootstrap","native-face-wire","native-person-defect-recovery","native-person-defect-wire","oriented","external","manual-lock","manual-lock-go","manual-lock-go-disjoint","manual-races")
    fresh=race in ("fresh","partial","registration-revoke")
    revoke_registration=race=="registration-revoke"
    partial=race=="partial"
    batch=race in ("native-terminal-worker-person","native-terminal-worker-detector","native-terminal-job-person-draft","native-terminal-job-detector","native-terminal-job-person","native-terminal-cancel","native-face-wire","native-person-defect-recovery","native-person-defect-wire","create-person-uncovered","create-person","create-service","ordinary-admission","native-bootstrap","batch","fresh","partial","registration-revoke","oriented","external","manual-lock","manual-lock-go","manual-lock-go-disjoint","manual-races")
    scene_wire=race=="native-scene-wire"
    scene_tail=race=="native-scene-tail"
    legacy_ordinary=race=='legacy-ordinary-recovery'
    if scene_wire or scene_tail or legacy_ordinary: native_bootstrap=oriented=batch=True
    if batch:race=False
    client,bucket=lens_objects
    source_object=None
    source_image=None
    source_canvas=None
    if batch:
        source_image=json.loads((ROOT/'backend/internal/review/testdata/working-images-python.json').read_text())[2]
        profiles=json.loads((ROOT/'resources/icc/profiles.lock.json').read_text())
        source_image['color']['icc_sha256']=next(profile['sha256'] for profile in profiles['profiles'] if profile['id']=='work_rec2020_gamma24_v1')
        rgb_buffer=BytesIO()
        rgb=np.full((300,400,3),.5 if oriented or create_person else 1.25,dtype='<f4')
        if scene_tail:
            # Identical source/target; real ORB must recover the transform.
            rng=np.random.default_rng(20261017)
            coarse=rng.uniform(.05,.95,(75,100)).astype('<f4')
            texture=np.repeat(np.repeat(coarse,4,axis=0),4,axis=1)
            rgb[:]=texture[...,None]
        np.save(rgb_buffer,rgb,allow_pickle=False)
        rgb_body=rgb_buffer.getvalue()
        rgb_id=str(uuid4())
        rgb_sha=sha256(rgb_body).hexdigest()
        rgb_key='proposal-rgb/'+rgb_id
        rgb_version=client.put_object(bucket,rgb_key,BytesIO(rgb_body),len(rgb_body),content_type='application/x-npy',metadata={'sha256':rgb_sha}).version_id
        source_image['pixels']=dict(artifact_id=rgb_id,sha256=rgb_sha)
        alpha_buffer=BytesIO()
        np.save(alpha_buffer,np.full((300,400),32768,dtype='<u2'),allow_pickle=False)
        alpha_body=alpha_buffer.getvalue()
        alpha_id=str(uuid4())
        alpha_sha=sha256(alpha_body).hexdigest()
        alpha_key='proposal-alpha/'+alpha_id
        alpha_version=client.put_object(bucket,alpha_key,BytesIO(alpha_body),len(alpha_body),content_type='application/x-npy',metadata={'sha256':alpha_sha}).version_id
        source_image['alpha']=dict(artifact_id=alpha_id,sha256=alpha_sha)
        warp_id=str(uuid4())
        warp_buffer=BytesIO()
        grid_y,grid_x=np.indices((300,400),dtype=np.float32)
        np.save(warp_buffer,np.stack((grid_x,grid_y),axis=-1),allow_pickle=False)
        warp_body=warp_buffer.getvalue()
        warp_sha=sha256(warp_body).hexdigest()
        warp_key='proposal-warp/'+warp_id
        warp_version=client.put_object(bucket,warp_key,BytesIO(warp_body),len(warp_body),content_type='application/x-npy',metadata={'sha256':warp_sha}).version_id
        source_image['transform_chain']['steps'][0]['inverse_field']=dict(artifact_id=warp_id,sha256=warp_sha)
        source_image['transform_chain']['hash']=sha256(json.dumps(dict(steps=source_image['transform_chain']['steps']),sort_keys=True,separators=(',',':')).encode()).hexdigest()
        if oriented:
            source_image['transform_chain']=dict(steps=[],hash=sha256(b'{"steps":[]}').hexdigest())
        source_body=json.dumps(source_image,sort_keys=True,separators=(',',':')).encode()
        source_digest=sha256(source_body).hexdigest()
        source_key='proposal-source/'+str(uuid4())
        source_version=client.put_object(bucket,source_key,BytesIO(source_body),len(source_body),content_type='application/json',metadata={'sha256':source_digest}).version_id
        source_object=dict(bucket=bucket,key=source_key,version=source_version,sha256=source_digest,bytes=len(source_body))
        source_canvas=sha256(json.dumps(source_image['canvas'],sort_keys=True,separators=(',',':')).encode()).hexdigest()
    with psycopg.connect(**database,autocommit=True) as db:
        # Inspect effective grants and attempt denied calls as the actual roles.
        for function,allowed,types in (
            ('enable_v27_person_admission','photo_scheduler','uuid,uuid,uuid,uuid,uuid,uuid'),
            ('claim_v27_person_admission','photo_scheduler','uuid,integer'),
            ('claim_v27_ordinary_parent','photo_scheduler','uuid,integer'),
            ('record_v27_ordinary_parent','photo_scheduler','uuid,uuid,uuid,text'),
            ('next_v27_ordinary_parent_project','photo_scheduler','uuid'),
            ('record_v27_person_admission','photo_scheduler','uuid,uuid,uuid,text'),
            ('next_v27_person_admission_project','photo_scheduler','uuid'),
            ('semantic_proposal_face_parse_preparation_candidates','photo_api','uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb'),
            ('lock_semantic_proposal_face_parse_inventory','photo_scheduler','uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb,jsonb'),
            ('lock_semantic_proposal_qualification_inventory','photo_scheduler','uuid,uuid,uuid,uuid'),
            ('semantic_proposal_qualification_policy_inventory','photo_api','uuid,uuid,uuid,uuid'),
            ('lock_semantic_proposal_qualification_policy_snapshot','photo_scheduler','uuid,uuid,uuid,uuid,jsonb'),
            ('semantic_defect_native_policy_ready','photo_api','uuid,uuid,uuid,uuid,uuid,jsonb,text'),
            ('lock_semantic_defect_native_policy','photo_scheduler','uuid,uuid,uuid,uuid,uuid,jsonb,text,jsonb'),
            ('lock_semantic_native_selection_evidence','photo_scheduler','uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid,jsonb'),
        ):
            signature=f'photo.{function}({types})'
            owner,definer,configuration=db.execute("SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=%s::regprocedure",(signature,)).fetchone()
            assert owner=='photo_fn_owner' and definer and 'search_path=pg_catalog, photo' in configuration
            for role in ('photo_api','photo_scheduler','photo_worker','photo_publisher','photo_portal','photo_auth','photo_gateway','photo_maintenance','photo_semantic_admission'):
                permitted=db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0]
                assert permitted==(role==allowed),(function,role,permitted)
                if role!=allowed:
                    with pytest.raises(psycopg.errors.InsufficientPrivilege):
                        with db.transaction():
                            db.execute(psycopg.sql.SQL('SET LOCAL ROLE {}').format(psycopg.sql.Identifier(role)))
                            arguments=psycopg.sql.SQL(',').join(psycopg.sql.SQL('NULL::{}').format(psycopg.sql.SQL(kind)) for kind in types.split(','))
                            db.execute(psycopg.sql.SQL('SELECT * FROM photo.{}({})').format(psycopg.sql.Identifier(function),arguments))
        with db.transaction():
            case=seed_original_composition(db,snapshot_canvas=True,source_object=source_object,canvas_hash=source_canvas)
            db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)',(case['Tenant'],))
            base=json.loads(case['Params'])['base']
            if batch:
                producer_run,producer_task=uuid4(),uuid4()
                db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'v2','draft',%s,%s,%s,%s,'fail_fast')",(producer_run,case['Tenant'],case['Snapshot'],Jsonb(dict(project_id=case['Project'])),'a'*64,case['Actor']))
                db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,'semantic.transfer:0','semantic.transfer','1','gpu','[]',%s,%s,%s)",(producer_task,case['Tenant'],producer_run,case['Artifact']['artifact_id'],'a'*64,Jsonb(dict(backend='cuda'))))
                db.execute("UPDATE photo.artifacts SET created_by_task_id=%s,produced_attempt=1,produced_fence=1 WHERE id=%s",(producer_task,base['source']['artifact_id']))
                db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,'application/x-npy',%s,clock_timestamp(),'working_rgb',%s,1,1)",(rgb_id,case['Tenant'],bucket,rgb_key,rgb_sha,len(rgb_body),rgb_version,producer_task))
                db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,'application/x-npy',%s,clock_timestamp(),'alpha',%s,1,1)",(alpha_id,case['Tenant'],bucket,alpha_key,alpha_sha,len(alpha_body),alpha_version,producer_task))
                db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,'application/x-npy',%s,clock_timestamp(),'inverse_field',%s,1,1)",(warp_id,case['Tenant'],bucket,warp_key,warp_sha,len(warp_body),warp_version,producer_task))
            target_version=db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state,version_no) SELECT tenant_id,asset_id,master_artifact_id,content_hash,'{}','accepted',2 FROM photo.asset_versions WHERE id=%s RETURNING id",(base['asset_version_id'],)).fetchone()[0]
            target_binding=None
            if os.environ.get('PHOTO_TEST_V27_TARGET_PERSON_DRAFT')=='1':
                target_binding=dict(base,asset_version_id=str(target_version))
                case['Snapshot']=str(db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES(%s,'v2','user',%s,%s,%s,clock_timestamp()+interval '1 hour') RETURNING id",(case['Tenant'],case['Actor'],'d'*64,2 if os.environ.get('PHOTO_TEST_V27_NATIVE_ALL_TARGET_MIXED_REGISTRATION')=='1' else 1)).fetchone()[0])
                db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,0,%s,%s,%s,%s)",(case['Tenant'],case['Snapshot'],target_version,base['source']['artifact_id'],base['source']['sha256'],Jsonb(dict(canvas_hash=base['canvas_hash']))))
                if os.environ.get('PHOTO_TEST_V27_NATIVE_ALL_TARGET_MIXED_REGISTRATION')=='1':
                    db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,1,%s,%s,%s,%s)",(case['Tenant'],case['Snapshot'],base['asset_version_id'],base['source']['artifact_id'],base['source']['sha256'],Jsonb(dict(canvas_hash=base['canvas_hash']))))
                db.execute('UPDATE photo.snapshots SET expires_at=NULL WHERE id=%s',(case['Snapshot'],))
        intent=dict(schema_version=1,kind='local_exposure',reference=base,reference_mask=dict(artifact_id=str(uuid4()),sha256='a'*64),target_snapshot_id=case['Snapshot'],strength=.5,ev=0,preserve_persons=True,prompt=None)
        if wrapper:
            mask=intent['reference_mask']
            mask_bucket,mask_version,mask_size,mask_key,mask_document='test','v1',100,str(uuid4()),{}
            if batch:
                pixels_id=str(uuid4())
                pixels_buffer=BytesIO()
                selected_pixels=np.full((300,400),.5,dtype='<f4')
                selected_pixels[41,17]=0
                if os.environ.get("PHOTO_TEST_V27_PERSON_DEFECT_PARENT") == "1" or scene_tail:
                    # Positive native parent input must be disjoint from the
                    # independently generated protected face features/person.
                    selected_pixels.fill(0)
                    selected_pixels[:10,:10]=.5
                    if scene_tail:
                        selected_pixels.fill(0)
                        selected_pixels[40:90,40:80]=.5
                if native_disjoint:
                    selected_pixels.fill(0)
                    selected_pixels[0,0]=.5
                np.save(pixels_buffer,selected_pixels,allow_pickle=False)
                pixels_body=pixels_buffer.getvalue()
                pixels_sha=sha256(pixels_body).hexdigest()
                pixels_key=f"tenants/{case['Tenant']}/v2-drafts/{pixels_id}"
                pixels_version=client.put_object(bucket,pixels_key,BytesIO(pixels_body),len(pixels_body),content_type='application/x-npy',metadata={'sha256':pixels_sha}).version_id
                mask_document=dict(artifact=dict(artifact_id=pixels_id,sha256=pixels_sha),canvas=source_image['canvas'],transform_chain=source_image['transform_chain'],selector='manual',confidence=1,protected=False,object_id=None,model_lock_id=None)
                mask_body=json.dumps(mask_document,sort_keys=True,separators=(',',':')).encode()
                mask['sha256']=sha256(mask_body).hexdigest()
                mask_key=f"tenants/{case['Tenant']}/v2-drafts/{mask['artifact_id']}"
                mask_bucket=bucket
                mask_size=len(mask_body)
                mask_version=client.put_object(bucket,mask_key,BytesIO(mask_body),len(mask_body),content_type='application/json',metadata={'sha256':mask['sha256']}).version_id
            with db.transaction():
                if batch:
                    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,%s,%s,%s,%s,%s,'application/x-npy',%s,clock_timestamp(),'mask_pixels')",(pixels_id,case['Tenant'],bucket,pixels_key,pixels_sha,len(pixels_body),pixels_version))
                    db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at,upload_contract,mask_width,mask_height) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,'ready',%s,clock_timestamp(),'MaskPixels',400,300)",(case['Tenant'],uuid4(),case['Project'],base['asset_version_id'],pixels_id,pixels_sha,len(pixels_body),bucket,pixels_version))
                db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,%s,%s,%s,%s,%s,'application/json',%s,clock_timestamp(),'document')",(mask['artifact_id'],case['Tenant'],mask_bucket,mask_key,mask['sha256'],mask_size,mask_version))
                db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at,user_mask_binding) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,'ready',%s,clock_timestamp(),%s)",(case['Tenant'],uuid4(),case['Project'],base['asset_version_id'],mask['artifact_id'],mask['sha256'],mask_size,mask_bucket,mask_version,Jsonb(dict(source=base['source'],canvas_hash=base['canvas_hash'],mask=mask_document))))
        fixture=dict(Tenant=case['Tenant'],Actor=case['Actor'],Session=case['Session'],Document=str(uuid4()),Intent=intent,TransferBatch=batch,CommitOrdinary=commit_ordinary,CreateOrdinary=create_ordinary,CreatePerson=create_person,FailManifest=partial,RevokeManifest=revoke_registration,Objects=client._photo_test_config,RevokeDuringPut=race is True,SourceDuringPut=race=="source")
        fixture['ManualLockOnly']=go_lock_only
        fixture['LegacyOrdinary']=legacy_ordinary
        fixture['ManualRaceOnly']=race_only
        if batch:
            fixture['SourceBody']=source_image
            fixture['MaskBody']=mask_document
            with db.transaction():
                db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}',%s::jsonb) WHERE id=%s",('8589934592' if scene_tail else '1073741824',case['Tenant']))
                calibration=json.loads((ROOT/'docs/implementation-v2/contracts/structural-fixtures.json').read_text())['fixtures']['Calibration']
                calibration.update(qualified=True,model_lock_sha='c'*64,dataset_sha='d'*64,loss_weights=[1/6]*6)
                calibration_body=json.dumps(calibration,sort_keys=True,separators=(',',':')).encode()
                calibration_id=str(uuid4())
                calibration_sha=sha256(calibration_body).hexdigest()
                calibration_key='proposal-calibration/'+calibration_id
                calibration_version=client.put_object(bucket,calibration_key,BytesIO(calibration_body),len(calibration_body),content_type='application/json',metadata={'sha256':calibration_sha}).version_id
                db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,%s,%s,%s,%s,%s,'application/json',%s,clock_timestamp())",(calibration_id,case['Tenant'],bucket,calibration_key,calibration_sha,len(calibration_body),calibration_version))
                scene_policy=dict(algorithm='farneback_depth_plane_v1',background_labels=[1],proxy_edge=256,min_support=32,flow_error_px=1,homography_error_px=1,depth_plane_error=.01)
                scene_policy_sha=sha256(json.dumps(scene_policy,sort_keys=True,separators=(',',':')).encode()).hexdigest()
                fixture['Qualification']=frozen_qualification(db,case['Tenant'],case['Project'],case['Actor'],objects=lens_objects,calibration_ref=dict(artifact_id=calibration_id,sha256=calibration_sha),model_lock_sha='c'*64,resource_metrics=dict(peak_rss_bytes=101,peak_vram_bytes=202,scratch_bytes=303) if race=='external' else None,required_checks=['quality','semantic.scene.farneback_depth_plane_v1'] if scene_tail else None,evidence_sha=scene_policy_sha if scene_tail else 'f'*64)
                if scene_wire or scene_tail:
                    fixture['SceneQualifications']=[frozen_qualification(db,case['Tenant'],case['Project'],case['Actor'],objects=lens_objects,calibration_ref=dict(artifact_id=calibration_id,sha256=calibration_sha),model_lock_sha='c'*64,operation=operation,backend='cuda',resource_metrics={'peak_vram_bytes':128<<20}) for operation in ('scene.parse','depth.predict')]
                if scene_tail:
                    # Explicit synthetic imported evidence for authority testing;
                    # never a measured model/corpus qualification claim.
                    fixture['ScenePolicy']=scene_policy
                fixture['MeasuredTransferCosts']=race=='external'
                fixture['PersonQualification']=frozen_qualification(db,case['Tenant'],case['Project'],case['Actor'],objects=lens_objects,calibration_ref=dict(artifact_id=calibration_id,sha256=calibration_sha),model_lock_sha='c'*64,operation='mask.person',resource_metrics={'peak_vram_bytes':128<<20} if native_bootstrap or create_person else None)
                fixture['FaceQualifications']=[frozen_qualification(db,case['Tenant'],case['Project'],case['Actor'],objects=lens_objects,calibration_ref=dict(artifact_id=calibration_id,sha256=calibration_sha),model_lock_sha='c'*64,operation=operation,backend='cuda' if native_bootstrap else 'cpu',resource_metrics={'peak_vram_bytes':128<<20} if os.environ.get('PHOTO_TEST_V27_TARGET_FACE_PLAN')=='1' else None) for operation in ('face.landmarks','face.detect')]
                fixture['DefectQualification']=frozen_qualification(db,case['Tenant'],case['Project'],case['Actor'],objects=lens_objects,calibration_ref=dict(artifact_id=calibration_id,sha256=calibration_sha),model_lock_sha='c'*64,operation='defects.propose',backend='cpu',resource_metrics=dict(peak_rss_bytes=401,peak_vram_bytes=0,scratch_bytes=503))
                fixture['ParseQualification']=frozen_qualification(db,case['Tenant'],case['Project'],case['Actor'],objects=lens_objects,calibration_ref=dict(artifact_id=calibration_id,sha256=calibration_sha),model_lock_sha='c'*64,operation='face.parse',backend='cuda' if native_bootstrap else 'cpu',resource_metrics={'peak_vram_bytes':128<<20} if os.environ.get('PHOTO_TEST_V27_TARGET_FACE_PLAN')=='1' else None)
                protection_run=str(uuid4())
                db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'apply','running',%s,%s,%s,%s,'fail_fast')",(protection_run,case['Tenant'],case['Snapshot'],Jsonb(dict(project_id=case['Project'])),'b'*64,case['Actor']))
                protection_task=str(uuid4())
                protection_root=str(uuid4())
                protection_child=str(uuid4())
                protected_buffer=BytesIO()
                np.save(protected_buffer,np.zeros((300,400),dtype='<f4'),allow_pickle=False)
                protected_body=protected_buffer.getvalue()
                protected_sha=sha256(protected_body).hexdigest()
                protection_document=dict(mask_document,protected=True,artifact=dict(artifact_id=protection_child,sha256=protected_sha))
                protection_body=json.dumps(protection_document,sort_keys=True,separators=(',',':')).encode()
                protection_sha=sha256(protection_body).hexdigest()
                db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'protection:0','mask.rasterize','1','cpu','[]',%s,%s,'{}',1,1)",(protection_task,case['Tenant'],protection_run,case['Artifact']['artifact_id'],'a'*64))
                for aid,digest,body,role,media in ((protection_root,protection_sha,protection_body,'mask_protected_ref','application/json'),(protection_child,protected_sha,protected_body,'mask_protected','application/octet-stream')):
                    key='proposal-protection/'+aid
                    version=client.put_object(bucket,key,BytesIO(body),len(body),content_type=media,metadata={'sha256':digest}).version_id
                    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp(),%s,%s,1,1)",(aid,case['Tenant'],bucket,key,digest,len(body),media,version,role,protection_task))
                outputs=[dict(artifact_id=protection_root,sha256=protection_sha),dict(artifact_id=protection_child,sha256=protected_sha)]
                # Separate real pinned required coverage, with one positive pixel.
                required_root,required_child=str(uuid4()),str(uuid4())
                required_pixels=np.zeros((300,400),dtype='<f4')
                # The actual person-bearing worker case must preserve a soft
                # probability below .5 outside every face feature region.
                required_pixels[41,17]=.37 if person_defect_execution else 1
                if os.environ.get('PHOTO_TEST_V27_ORIGINAL_PERSON') == '1':
                    required_pixels[100:,:]=1
                required_buffer=BytesIO()
                np.save(required_buffer,required_pixels,allow_pickle=False)
                required_body=required_buffer.getvalue()
                required_sha=sha256(required_body).hexdigest()
                required_document=dict(protection_document,artifact=dict(artifact_id=required_child,sha256=required_sha))
                required_json=json.dumps(required_document,sort_keys=True,separators=(',',':')).encode()
                required_json_sha=sha256(required_json).hexdigest()
                for aid,digest,body,role,media in ((required_root,required_json_sha,required_json,'mask_protected_ref','application/json'),(required_child,required_sha,required_body,'mask_protected','application/octet-stream')):
                    key='proposal-required/'+aid
                    version=client.put_object(bucket,key,BytesIO(body),len(body),content_type=media,metadata={'sha256':digest}).version_id
                    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp(),%s,%s,1,1)",(aid,case['Tenant'],bucket,key,digest,len(body),media,version,role,protection_task))
                    outputs.append(dict(artifact_id=aid,sha256=digest))
                fixture['RequiredProtection']=dict(artifact_id=required_root,sha256=required_json_sha)

                db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')",(case['Tenant'],protection_task,uuid4()))
                result_id=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(case['Tenant'],protection_task,Jsonb(outputs),'3'*64)).fetchone()[0]
                db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result_id,protection_task))
                db.execute("INSERT INTO photo.interactive_tasks(tenant_id,run_id,task_id,kind,operation,source_artifact_id,declared_inputs,params,params_sha256,backend) VALUES(%s,%s,%s,'mask_manual','mask.rasterize',%s,'[]','{}',%s,'cpu')",(case['Tenant'],protection_run,protection_task,base['source']['artifact_id'],'a'*64))
                fixture['Protection']=dict(artifact_id=protection_root,sha256=protection_sha)
                from photo_worker.contracts.base import canonical_bytes
                for person_ordinal in range(0 if os.environ.get('PHOTO_TEST_V27_ORIGINAL_PERSON') == '1' else 2):
                    person_task,person_root,person_child=str(uuid4()),str(uuid4()),str(uuid4())
                    from photo_worker.contracts.base import canonical_bytes
                    from photo_worker.contracts.person import PersonParams,PersonReport
                    person_params=PersonParams.model_validate_json(json.dumps(dict(image=base['source'],model_lock_id='c'*64)))
                    person_params_body=canonical_bytes(person_params)
                    person_params_id=str(uuid4())
                    person_params_sha=sha256(person_params_body).hexdigest()
                    person_params_key='proposal-person-params/'+person_params_id
                    person_params_version=client.put_object(bucket,person_params_key,BytesIO(person_params_body),len(person_params_body),content_type='application/json',metadata={'sha256':person_params_sha}).version_id
                    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,%s,%s,%s,%s,%s,'application/json',%s,clock_timestamp(),'document')",(person_params_id,case['Tenant'],bucket,person_params_key,person_params_sha,len(person_params_body),person_params_version))
                    person_report_id=str(uuid4())
                    person_report=PersonReport.model_validate_json(json.dumps(dict(image=base['source'],mask=dict(artifact_id=person_child,sha256=required_sha),preview_width=768,preview_height=576,model_id=person_params.model_id,model_lock_id='c'*64,backend='cuda',preprocessing_id=person_params.preprocessing_id,confidence=1,person_fraction=1/120000,source_clip_fraction=0,alpha_excluded_fraction=0,warnings=['empty_person'])))
                    person_report_body=canonical_bytes(person_report)
                    person_report_sha=sha256(person_report_body).hexdigest()

                    person_document=dict(required_document,protected=False,selector='person',model_lock_id='c'*64,artifact=dict(artifact_id=person_child,sha256=required_sha))
                    person_json=json.dumps(person_document,sort_keys=True,separators=(',',':')).encode()
                    person_sha=sha256(person_json).hexdigest()
                    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,%s,'mask.person','1','gpu','[]',%s,%s,%s,1,1)",(person_task,case['Tenant'],protection_run,f'person:{person_ordinal}',person_params_id,'a'*64,Jsonb(dict(backend='cuda',model_lock_sha='c'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64))))
                    person_outputs=[]
                    for aid,digest,body,role,media in ((person_root,person_sha,person_json,'person_mask_ref','application/json'),(person_child,required_sha,required_body,'person_mask','application/octet-stream'),(person_report_id,person_report_sha,person_report_body,'person_report','application/json')):
                        key='proposal-person/'+aid
                        version=client.put_object(bucket,key,BytesIO(body),len(body),content_type=media,metadata={'sha256':digest}).version_id
                        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp(),%s,%s,1,1)",(aid,case['Tenant'],bucket,key,digest,len(body),media,version,role,person_task))
                        person_outputs.append(dict(artifact_id=aid,sha256=digest))
                    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cuda',%s)",(case['Tenant'],person_task,uuid4(),Jsonb(dict(backend='cuda',model_lock_sha='c'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64))))
                    person_result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(case['Tenant'],person_task,Jsonb(person_outputs),'4'*64)).fetchone()[0]
                    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(person_result,person_task))
                    db.execute("INSERT INTO photo.apply_nodes(id,tenant_id,run_id,phase,ordinal,source_item_id,source_asset_id,source_version_id,node_key,operation,inputs,params_sha256,params) VALUES(%s,%s,%s,'mask',%s,%s,%s,%s,%s,'mask.person',%s,%s,%s)",(person_task,case['Tenant'],protection_run,person_ordinal,uuid4(),db.execute('SELECT asset_id FROM photo.asset_versions WHERE id=%s',(base['asset_version_id'],)).fetchone()[0],base['asset_version_id'],f'person:{person_ordinal}',Jsonb([base['source']]),person_params_sha,Jsonb(person_params.model_dump(mode='json'))))
                    if person_ordinal == 0:
                        fixture['PersonMask']=dict(artifact_id=person_root,sha256=person_sha)
                        fixture['PersonReport']=dict(artifact_id=person_report_id,sha256=person_report_sha)
                    else:
                        fixture['AmbiguousPersonMask']=dict(artifact_id=person_root,sha256=person_sha)
                        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=%s",(person_root,))
                from photo_worker.contracts.defects import DefectProposeParams,DefectCandidates,DefectProposeReport
                from PIL import Image
                defect_task=str(uuid4())
                defect_ids={role:str(uuid4()) for role in ('params','proposal','report','labels','protection','png')}
                label_buffer=BytesIO()
                np.save(label_buffer,np.zeros((300,400),dtype='<u2'),allow_pickle=False)
                label_body=label_buffer.getvalue()
                label_sha=sha256(label_body).hexdigest()
                png_buffer=BytesIO()
                from photo_worker.algorithms.mask_png import write_png16, png16_bound
                write_png16(np.zeros((300,400),dtype=np.uint16),png_buffer,max_bytes=png16_bound(400,300))
                png_body=png_buffer.getvalue()
                png_sha=sha256(png_body).hexdigest()
                defect_params=DefectProposeParams.model_validate_json(json.dumps(dict(image=base['source'],faces=[],permanent=required_document)))
                defect_params_body=canonical_bytes(defect_params)
                defect_params_sha=sha256(defect_params_body).hexdigest()
                defect_proposal=DefectCandidates.model_validate_json(json.dumps(dict(image=base['source'],source_pixels=source_image['pixels'],label_map=dict(artifact_id=defect_ids['labels'],sha256=label_sha),canvas=source_image['canvas'],transform_chain=source_image['transform_chain'],candidates=[])))
                defect_proposal_body=canonical_bytes(defect_proposal)
                defect_proposal_sha=sha256(defect_proposal_body).hexdigest()
                defect_protection=dict(required_document,artifact=dict(artifact_id=defect_ids['protection'],sha256=required_sha))
                defect_report=DefectProposeReport.model_validate_json(json.dumps(dict(params=defect_params.model_dump(mode='json'),proposal=dict(artifact_id=defect_ids['proposal'],sha256=defect_proposal_sha),protection=defect_protection,label_png=dict(artifact_id=defect_ids['png'],sha256=png_sha),faces=[],warnings=['no_faces'])))
                defect_report_body=canonical_bytes(defect_report)
                defect_report_sha=sha256(defect_report_body).hexdigest()
                def defect_object(aid,body,role,media,producer=None):
                    digest=sha256(body).hexdigest()
                    key='proposal-defect/'+aid
                    version=client.put_object(bucket,key,BytesIO(body),len(body),content_type=media,metadata={'sha256':digest}).version_id
                    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp(),%s,%s,%s,%s)",(aid,case['Tenant'],bucket,key,digest,len(body),media,version,role,producer,1 if producer else None,1 if producer else None))
                    return dict(artifact_id=aid,sha256=digest)
                params_pin=defect_object(defect_ids['params'],defect_params_body,'document','application/json')
                db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'defect:0','defects.propose','1','cpu',%s,%s,%s,%s,1,1)",(defect_task,case['Tenant'],protection_run,Jsonb([base['source'],required_document['artifact']]),defect_ids['params'],'a'*64,Jsonb(dict(backend='cpu'))))
                defect_outputs=[]
                for role,body,output,media in (
                    ('proposal',defect_proposal_body,'defect_candidates','application/json'),
                    ('report',defect_report_body,'defect_propose_report','application/json'),
                    ('labels',label_body,'defect_labels','application/octet-stream'),
                    ('protection',required_body,'defect_protection','application/octet-stream'),
                    ('png',png_body,'defect_labels_png','image/png')):
                    defect_outputs.append(defect_object(defect_ids[role],body,output,media,defect_task))
                db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu',%s)",(case['Tenant'],defect_task,uuid4(),Jsonb(dict(backend='cpu'))))
                defect_result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(case['Tenant'],defect_task,Jsonb(defect_outputs),'5'*64)).fetchone()[0]
                db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(defect_result,defect_task))
                by_id={pin['artifact_id']:pin for pin in defect_outputs}
                fixture['DefectPins']=dict(Proposal=by_id[defect_ids['proposal']],Report=by_id[defect_ids['report']],Labels=by_id[defect_ids['labels']],Protection=by_id[defect_ids['protection']],Params=params_pin)
                # Valid original documents and ready producer-owned PNG, but the
                # immutable canonical output inventory intentionally omits PNG.
                incomplete_task=str(uuid4())
                incomplete_ids={role:str(uuid4()) for role in ('proposal','report','labels','protection','png')}
                incomplete_doc=defect_proposal.model_dump(mode='json')
                incomplete_doc['label_map']=dict(artifact_id=incomplete_ids['labels'],sha256=label_sha)
                incomplete_proposal=DefectCandidates.model_validate_json(json.dumps(incomplete_doc))
                incomplete_proposal_body=canonical_bytes(incomplete_proposal)
                incomplete_proposal_sha=sha256(incomplete_proposal_body).hexdigest()
                incomplete_report=DefectProposeReport.model_validate_json(json.dumps(dict(
                    params=defect_params.model_dump(mode='json'),
                    proposal=dict(artifact_id=incomplete_ids['proposal'],sha256=incomplete_proposal_sha),
                    protection=dict(defect_protection,artifact=dict(artifact_id=incomplete_ids['protection'],sha256=required_sha)),
                    label_png=dict(artifact_id=incomplete_ids['png'],sha256=png_sha),faces=[],warnings=['no_faces'])))
                db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'defect:png-incomplete','defects.propose','1','cpu',%s,%s,%s,%s,1,1)",(incomplete_task,case['Tenant'],protection_run,Jsonb([base['source'],required_document['artifact']]),defect_ids['params'],'a'*64,Jsonb(dict(backend='cpu'))))
                incomplete_outputs=[]
                incomplete_by_role={}
                for role,body,output,media in (
                    ('proposal',incomplete_proposal_body,'defect_candidates','application/json'),
                    ('report',canonical_bytes(incomplete_report),'defect_propose_report','application/json'),
                    ('labels',label_body,'defect_labels','application/octet-stream'),
                    ('protection',required_body,'defect_protection','application/octet-stream'),
                    ('png',png_body,'defect_labels_png','image/png')):
                    pin=defect_object(incomplete_ids[role],body,output,media,incomplete_task)
                    incomplete_by_role[role]=pin
                    if role!='png':incomplete_outputs.append(pin)
                db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu',%s)",(case['Tenant'],incomplete_task,uuid4(),Jsonb(dict(backend='cpu'))))
                incomplete_result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(case['Tenant'],incomplete_task,Jsonb(incomplete_outputs),'6'*64)).fetchone()[0]
                db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(incomplete_result,incomplete_task))
                fixture['IncompletePNGDefectPins']=dict(Proposal=incomplete_by_role['proposal'],Report=incomplete_by_role['report'],Labels=incomplete_by_role['labels'],Protection=incomplete_by_role['protection'],Params=params_pin)
                from photo_worker.contracts.face import FaceLandmarks,LandmarkParams,FaceBoxes,FaceDetectParams
                from photo_worker.contracts.images import Face68
                face_id=str(uuid4())
                landmark_task=str(uuid4())
                landmark_id=str(uuid4())
                face=Face68.model_validate_json(json.dumps(dict(object_id=face_id,points=[dict(x=.5,y=.5,confidence=1) for _ in range(68)],canvas=source_image['canvas'],transform_chain=source_image['transform_chain'])))
                detection_task=str(uuid4())
                detection_params=FaceDetectParams.model_validate_json(json.dumps(dict(image=base['source'],model_lock_id='c'*64)))
                detection_params_pin=defect_object(str(uuid4()),canonical_bytes(detection_params),'document','application/json')
                db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'detect:0','face.detect','1','gpu',%s,%s,%s,%s,1,1)",(detection_task,case['Tenant'],protection_run,Jsonb([base['source']]),detection_params_pin['artifact_id'],'a'*64,Jsonb(dict(backend='cpu',model_lock_sha='c'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64))))
                detection_doc=FaceBoxes.model_validate_json(json.dumps(dict(image=base['source'],faces=[dict(object_id=face_id,bbox_normalized=dict(x0=.25,y0=.25,x1=.75,y1=.75),confidence=1)],model_id='sfd',model_lock_id='c'*64,backend='cpu',preprocessing_id='sfd_long1024_bgr_v1',preview_width=400,preview_height=300,candidate_count=1,accepted_count=1,alpha_excluded_fraction=0,warnings=[])))
                detection_pin=defect_object(str(uuid4()),canonical_bytes(detection_doc),'face_boxes','application/json',detection_task)
                db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu',%s)",(case['Tenant'],detection_task,uuid4(),Jsonb(dict(backend='cpu',model_lock_sha='c'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64))))
                detection_result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(case['Tenant'],detection_task,Jsonb([detection_pin]),'8'*64)).fetchone()[0]
                db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(detection_result,detection_task))
                landmark_doc=FaceLandmarks.model_validate_json(json.dumps(dict(image=base['source'],faces=detection_pin,landmarks=[face.model_dump(mode='json')],model_id='fan2d',model_lock_id='c'*64,backend='cpu',preprocessing_id='fan2d_crop256_v1',mean_confidence=1,warnings=[])))
                landmark_params=LandmarkParams.model_validate_json(json.dumps(dict(image=base['source'],faces=detection_pin,model_lock_id='c'*64)))
                landmark_params_pin=defect_object(str(uuid4()),canonical_bytes(landmark_params),'document','application/json')
                db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'landmarks:0','face.landmarks','1','gpu',%s,%s,%s,%s,1,1)",(landmark_task,case['Tenant'],protection_run,Jsonb([base['source'],detection_pin]),landmark_params_pin['artifact_id'],'a'*64,Jsonb(dict(backend='cpu',model_lock_sha='c'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64))))
                landmark_pin=defect_object(landmark_id,canonical_bytes(landmark_doc),'face_landmarks','application/json',landmark_task)
                db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu',%s)",(case['Tenant'],landmark_task,uuid4(),Jsonb(dict(backend='cpu',model_lock_sha='c'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64))))
                landmark_result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(case['Tenant'],landmark_task,Jsonb([landmark_pin]),'6'*64)).fetchone()[0]
                db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(landmark_result,landmark_task))
                # No defect producer exists yet: face preparation must work independently.
                def face_preparation(ref,operation):
                    db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
                    db.execute('SET LOCAL ROLE photo_api')
                    try:
                        return db.execute("SELECT * FROM photo.semantic_proposal_face_preparation_object(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)",(case['Tenant'],case['Project'],case['Actor'],case['Session'],base['asset_version_id'],base['source']['artifact_id'],base['source']['sha256'],ref['artifact_id'],ref['sha256'],operation)).fetchall()
                    finally:
                        db.execute('RESET ROLE')
                for ref,operation,params_ref in [(detection_pin,'face.detect',detection_params_pin),(landmark_pin,'face.landmarks',landmark_params_pin)]:
                    found=face_preparation(ref,operation)
                    assert len(found)==1 and str(found[0][4])==params_ref['artifact_id'] and found[0][5]==params_ref['sha256']
                    assert not face_preparation(ref,'mask.rasterize')
                    db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(ref['artifact_id'],))
                    assert not face_preparation(ref,operation)
                    db.execute("UPDATE photo.artifacts SET expires_at=NULL WHERE id=%s",(ref['artifact_id'],))
                    assert len(face_preparation(ref,operation))==1
                face_defect_task=str(uuid4())
                face_ids={role:str(uuid4()) for role in ('params','proposal','report','labels','protection','png')}
                face_mask=dict(required_document,selector='manual',protected=False,object_id=face_id)
                face_params=DefectProposeParams.model_validate_json(json.dumps(dict(image=base['source'],faces=[dict(object_id=face_id,landmarks=landmark_pin,skin=None,manual=face_mask,manual_mode=True)],permanent=required_document)))
                face_params_pin=defect_object(face_ids['params'],canonical_bytes(face_params),'document','application/json')
                db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'defect:1','defects.propose','1','cpu',%s,%s,%s,%s,1,1)",(face_defect_task,case['Tenant'],protection_run,Jsonb([base['source'],landmark_pin,required_document['artifact']]),face_ids['params'],'a'*64,Jsonb(dict(backend='cpu'))))
                face_proposal=defect_proposal.model_copy(update={'label_map':defect_proposal.label_map.model_copy(update={'artifact_id':UUID(face_ids['labels'])})})
                face_proposal_pin=defect_object(face_ids['proposal'],canonical_bytes(face_proposal),'defect_candidates','application/json',face_defect_task)
                face_protection=dict(required_document,artifact=dict(artifact_id=face_ids['protection'],sha256=required_sha))
                face_report=DefectProposeReport.model_validate_json(json.dumps(dict(params=face_params.model_dump(mode='json'),proposal=face_proposal_pin,protection=face_protection,label_png=dict(artifact_id=face_ids['png'],sha256=png_sha),faces=[dict(object_id=face_id,reason='degenerate_landmarks',threshold=0,candidates=0,overlapping_candidates=0)],warnings=['face_skipped'])))
                face_outputs=[face_proposal_pin]
                for role,body,output,media in (('report',canonical_bytes(face_report),'defect_propose_report','application/json'),('labels',label_body,'defect_labels','application/octet-stream'),('protection',required_body,'defect_protection','application/octet-stream'),('png',png_body,'defect_labels_png','image/png')):
                    face_outputs.append(defect_object(face_ids[role],body,output,media,face_defect_task))
                db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu',%s)",(case['Tenant'],face_defect_task,uuid4(),Jsonb(dict(backend='cpu'))))
                face_result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(case['Tenant'],face_defect_task,Jsonb(face_outputs),'7'*64)).fetchone()[0]
                db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(face_result,face_defect_task))
                face_by_id={pin['artifact_id']:pin for pin in face_outputs}
                fixture['FaceDefectPins']=dict(Proposal=face_by_id[face_ids['proposal']],Report=face_by_id[face_ids['report']],Labels=face_by_id[face_ids['labels']],Protection=face_by_id[face_ids['protection']],Params=face_params_pin)
                from photo_worker.contracts.manual_mask import MaskRasterParams,RasterizedMask
                from photo_worker.contracts.images import MaskRef
                from photo_worker.algorithms.manual_mask import rasterize
                from photo_worker.algorithms.mask_png import write_png16,png16_bound
                manual_task=str(uuid4())
                manual_params=MaskRasterParams.model_validate_json(json.dumps(dict(image=base['source'],inline_vectors=dict(source_sha256=source_image['canvas']['source_sha256'],canvas=source_image['canvas'],shapes=[dict(kind='polygon',sequence=0,mode='add',points=[dict(x=.2,y=.2),dict(x=.8,y=.2),dict(x=.8,y=.8),dict(x=.2,y=.8)]),dict(kind='stroke',sequence=1,mode='protect',points=[dict(x=.5,y=.5)],radius=.03,hardness=1)]),object_id=face_id)))
                manual_vectors=manual_params.inline_vectors
                manual_inputs=[base['source']]
                manual_vector_pin=None
                if external_vectors:
                    manual_vector_pin=defect_object(str(uuid4()),canonical_bytes(manual_vectors),'document','application/json')
                    manual_inputs.append(manual_vector_pin)
                    manual_params=MaskRasterParams.model_validate_json(json.dumps(dict(image=base['source'],vectors=manual_vector_pin,object_id=face_id)))
                fixture['ManualVectors']=manual_vector_pin
                manual_params_id=str(uuid4())
                manual_params_pin=defect_object(manual_params_id,canonical_bytes(manual_params),'document','application/json')
                db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'manual:positive','mask.rasterize','1','cpu',%s,%s,%s,%s,1,1)",(manual_task,case['Tenant'],protection_run,Jsonb(manual_inputs),manual_params_id,'a'*64,Jsonb(dict(backend='cpu'))))
                manual_arrays=[np.zeros((300,400),dtype='<f4'),np.zeros((300,400),dtype='<f4')]
                for tile in rasterize(manual_vectors,source_image['canvas']['source_sha256'],tile_size=256):
                    th,tw=tile.target.shape
                    for full,part in zip(manual_arrays,(tile.target,tile.protected)):
                        full[tile.y:tile.y+th,tile.x:tile.x+tw]=part
                assert np.count_nonzero(manual_arrays[0])>0 and np.count_nonzero(manual_arrays[1])>0
                manual_binary_pins=[]
                manual_png_pins=[]
                for array,role in zip(manual_arrays,('mask_target','mask_protected')):
                    encoded=BytesIO()
                    np.save(encoded,array,allow_pickle=False)
                    manual_binary_pins.append(defect_object(str(uuid4()),encoded.getvalue(),role,'application/octet-stream',manual_task))
                    encoded_png=BytesIO()
                    write_png16(array,encoded_png,max_bytes=png16_bound(400,300))
                    manual_png_pins.append(defect_object(str(uuid4()),encoded_png.getvalue(),role+'_png','image/png',manual_task))
                manual_pixel,manual_protected_pixel=manual_binary_pins
                fixture['ManualOutputs']=[manual_protected_pixel,*manual_png_pins]
                manual_target=MaskRef.model_validate_json(json.dumps(dict(required_document,artifact=manual_pixel,selector='manual',protected=False,object_id=face_id)))
                manual_protected=MaskRef.model_validate_json(json.dumps(dict(required_document,artifact=manual_protected_pixel,selector='manual',protected=True,object_id=face_id)))
                manual_root=RasterizedMask.model_validate_json(json.dumps(dict(image=base['source'],vectors=manual_vector_pin or manual_params_pin,target=manual_target.model_dump(mode='json'),protected=manual_protected.model_dump(mode='json'),target_png=manual_png_pins[0],protected_png=manual_png_pins[1])))
                manual_root_pin=defect_object(str(uuid4()),canonical_bytes(manual_root),'rasterized_mask','application/json',manual_task)
                db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu',%s)",(case['Tenant'],manual_task,uuid4(),Jsonb(dict(backend='cpu'))))
                manual_result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(case['Tenant'],manual_task,Jsonb([*manual_binary_pins,*manual_png_pins,manual_root_pin]),'8'*64)).fetchone()[0]
                db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(manual_result,manual_task))
                if go_lock_only:
                    second_manual_face_id=str(uuid4())
                    second_manual_task=str(uuid4())
                    second_manual_params=MaskRasterParams.model_validate_json(json.dumps(dict(image=base['source'],inline_vectors=dict(source_sha256=source_image['canvas']['source_sha256'],canvas=source_image['canvas'],shapes=[dict(kind='polygon',sequence=0,mode='add',points=[dict(x=.2,y=.2),dict(x=.8,y=.2),dict(x=.8,y=.8),dict(x=.2,y=.8)]),dict(kind='stroke',sequence=1,mode='protect',points=[dict(x=.5,y=.5)],radius=.03,hardness=1)]),object_id=second_manual_face_id)))
                    second_manual_vectors=second_manual_params.inline_vectors
                    second_manual_inputs=[base['source']]
                    second_manual_vector_pin=None
                    if external_vectors:
                        second_manual_vector_pin=defect_object(str(uuid4()),canonical_bytes(second_manual_vectors),'document','application/json')
                        second_manual_inputs.append(second_manual_vector_pin)
                        second_manual_params=MaskRasterParams.model_validate_json(json.dumps(dict(image=base['source'],vectors=second_manual_vector_pin,object_id=second_manual_face_id)))
                    fixture['SecondManualVectors']=second_manual_vector_pin
                    second_manual_params_id=str(uuid4())
                    second_manual_params_pin=defect_object(second_manual_params_id,canonical_bytes(second_manual_params),'document','application/json')
                    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'manual:second','mask.rasterize','1','cpu',%s,%s,%s,%s,1,1)",(second_manual_task,case['Tenant'],protection_run,Jsonb(second_manual_inputs),second_manual_params_id,'a'*64,Jsonb(dict(backend='cpu'))))
                    second_manual_arrays=[np.zeros((300,400),dtype='<f4'),np.zeros((300,400),dtype='<f4')]
                    for tile in rasterize(second_manual_vectors,source_image['canvas']['source_sha256'],tile_size=256):
                        th,tw=tile.target.shape
                        for full,part in zip(second_manual_arrays,(tile.target,tile.protected)):
                            full[tile.y:tile.y+th,tile.x:tile.x+tw]=part
                    assert np.count_nonzero(second_manual_arrays[0])>0 and np.count_nonzero(second_manual_arrays[1])>0
                    second_manual_binary_pins=[]
                    second_manual_png_pins=[]
                    for array,role in zip(second_manual_arrays,('mask_target','mask_protected')):
                        encoded=BytesIO()
                        np.save(encoded,array,allow_pickle=False)
                        second_manual_binary_pins.append(defect_object(str(uuid4()),encoded.getvalue(),role,'application/octet-stream',second_manual_task))
                        encoded_png=BytesIO()
                        write_png16(array,encoded_png,max_bytes=png16_bound(400,300))
                        second_manual_png_pins.append(defect_object(str(uuid4()),encoded_png.getvalue(),role+'_png','image/png',second_manual_task))
                    second_manual_pixel,second_manual_protected_pixel=second_manual_binary_pins
                    fixture['SecondManualOutputs']=[second_manual_protected_pixel,*second_manual_png_pins]
                    second_manual_target=MaskRef.model_validate_json(json.dumps(dict(required_document,artifact=second_manual_pixel,selector='manual',protected=False,object_id=second_manual_face_id)))
                    second_manual_protected=MaskRef.model_validate_json(json.dumps(dict(required_document,artifact=second_manual_protected_pixel,selector='manual',protected=True,object_id=second_manual_face_id)))
                    second_manual_root=RasterizedMask.model_validate_json(json.dumps(dict(image=base['source'],vectors=second_manual_vector_pin or second_manual_params_pin,target=second_manual_target.model_dump(mode='json'),protected=second_manual_protected.model_dump(mode='json'),target_png=second_manual_png_pins[0],protected_png=second_manual_png_pins[1])))
                    second_manual_root_pin=defect_object(str(uuid4()),canonical_bytes(second_manual_root),'rasterized_mask','application/json',second_manual_task)
                    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu',%s)",(case['Tenant'],second_manual_task,uuid4(),Jsonb(dict(backend='cpu'))))
                    second_manual_result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(case['Tenant'],second_manual_task,Jsonb([*second_manual_binary_pins,*second_manual_png_pins,second_manual_root_pin]),'8'*64)).fetchone()[0]
                    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(second_manual_result,second_manual_task))
                    fixture['SecondManualTarget']=second_manual_target.model_dump(mode='json')
                # Exercise exact retained-pin locks through the restricted scheduler role.
                roles={'rasterized_mask':manual_root_pin,'task_params':manual_params_pin,
                       'mask_target':manual_pixel,'mask_protected':manual_protected_pixel,
                       'mask_target_png':manual_png_pins[0],'mask_protected_png':manual_png_pins[1]}
                if manual_vector_pin is not None:
                    roles['manual_vectors']=manual_vector_pin
                lock_pins=[]
                for role,ref in sorted(roles.items()):
                    row=db.execute("SELECT bucket,object_key,object_version,bytes,media_type FROM photo.artifacts WHERE tenant_id=%s AND id=%s",(case['Tenant'],ref['artifact_id'])).fetchone()
                    lock_pins.append(dict(role=role,artifact_id=ref['artifact_id'],sha256=ref['sha256'],bucket=row[0],key=row[1],version=row[2],bytes=row[3],media_type=row[4]))
                def manual_lock(pins,tenant=None):
                    db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
                    db.execute('SET LOCAL ROLE photo_scheduler')
                    try:
                        return db.execute("SELECT photo.lock_semantic_proposal_manual_evidence(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)",(tenant or case['Tenant'],case['Project'],case['Actor'],case['Session'],base['asset_version_id'],base['source']['artifact_id'],base['source']['sha256'],manual_pixel['artifact_id'],manual_pixel['sha256'],Jsonb(pins))).fetchone()[0]
                    finally:
                        db.execute('RESET ROLE')
                assert manual_lock(lock_pins) is True
                assert manual_lock(lock_pins,str(uuid4())) is False
                assert manual_lock(lock_pins[:-1]) is False
                for field,value in [('version','different-version'),('sha256','f'*64),('bytes',1),('media_type','text/plain'),('key','different-key'),('bucket','different-bucket')]:
                    changed=json.loads(json.dumps(lock_pins))
                    changed[0][field]=value
                    assert manual_lock(changed) is False
                changed=json.loads(json.dumps(lock_pins))
                changed[0]['artifact_id']=changed[1]['artifact_id']
                assert manual_lock(changed) is False
                db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(manual_protected_pixel['artifact_id'],))
                assert manual_lock(lock_pins) is False
                db.execute("UPDATE photo.artifacts SET expires_at=NULL WHERE id=%s",(manual_protected_pixel['artifact_id'],))
                assert manual_lock(lock_pins) is True
                fixture['OrientedSource']=oriented
                fixture['ManualFaceMaskRoot']=manual_root_pin
                positive_face_defect_task=str(uuid4())
                positive_face_ids={role:str(uuid4()) for role in ('params','proposal','report','labels','protection','png')}
                positive_face_mask=manual_target.model_dump(mode='json')
                positive_face_params=DefectProposeParams.model_validate_json(json.dumps(dict(image=base['source'],faces=[dict(object_id=face_id,landmarks=landmark_pin,skin=None,manual=positive_face_mask,manual_mode=True)],permanent=required_document,protections=[manual_protected.model_dump(mode='json')])))
                positive_face_params_pin=defect_object(positive_face_ids['params'],canonical_bytes(positive_face_params),'document','application/json')
                db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'defect:3','defects.propose','1','cpu',%s,%s,%s,%s,1,1)",(positive_face_defect_task,case['Tenant'],protection_run,Jsonb([base['source'],landmark_pin,required_document['artifact'],manual_pixel,manual_protected_pixel]),positive_face_ids['params'],'a'*64,Jsonb(dict(backend='cpu'))))
                positive_face_proposal=defect_proposal.model_copy(update={'label_map':defect_proposal.label_map.model_copy(update={'artifact_id':UUID(positive_face_ids['labels'])})})
                positive_face_proposal_pin=defect_object(positive_face_ids['proposal'],canonical_bytes(positive_face_proposal),'defect_candidates','application/json',positive_face_defect_task)
                covered_protection_encoded=BytesIO()
                required_array=np.load(BytesIO(required_body),allow_pickle=False)
                np.save(covered_protection_encoded,np.maximum(required_array,manual_arrays[1]),allow_pickle=False)
                covered_protection_body=covered_protection_encoded.getvalue()
                covered_protection_sha=sha256(covered_protection_body).hexdigest()
                positive_face_protection=dict(required_document,artifact=dict(artifact_id=positive_face_ids['protection'],sha256=covered_protection_sha))
                positive_face_report=DefectProposeReport.model_validate_json(json.dumps(dict(params=positive_face_params.model_dump(mode='json'),proposal=positive_face_proposal_pin,protection=positive_face_protection,label_png=dict(artifact_id=positive_face_ids['png'],sha256=png_sha),faces=[dict(object_id=face_id,reason='degenerate_landmarks',threshold=0,candidates=0,overlapping_candidates=0)],warnings=['face_skipped'])))
                positive_face_outputs=[positive_face_proposal_pin]
                for role,body,output,media in (('report',canonical_bytes(positive_face_report),'defect_propose_report','application/json'),('labels',label_body,'defect_labels','application/octet-stream'),('protection',covered_protection_body,'defect_protection','application/octet-stream'),('png',png_body,'defect_labels_png','image/png')):
                    positive_face_outputs.append(defect_object(positive_face_ids[role],body,output,media,positive_face_defect_task))
                db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu',%s)",(case['Tenant'],positive_face_defect_task,uuid4(),Jsonb(dict(backend='cpu'))))
                positive_face_result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(case['Tenant'],positive_face_defect_task,Jsonb(positive_face_outputs),'7'*64)).fetchone()[0]
                db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(positive_face_result,positive_face_defect_task))
                positive_face_by_id={pin['artifact_id']:pin for pin in positive_face_outputs}
                fixture['ManualFaceDefectTask']=positive_face_defect_task
                fixture['ManualFaceDefectPins']=dict(Proposal=positive_face_by_id[positive_face_ids['proposal']],Report=positive_face_by_id[positive_face_ids['report']],Labels=positive_face_by_id[positive_face_ids['labels']],Protection=positive_face_by_id[positive_face_ids['protection']],Params=positive_face_params_pin)
                # Same original manual inputs, valid objects and canonical result,
                # but deliberately missing the manual protected stroke in output.
                uncovered_task=str(uuid4())
                uncovered_ids={role:str(uuid4()) for role in ('proposal','report','labels','protection','png')}
                uncovered_proposal=positive_face_proposal.model_copy(update={'label_map':positive_face_proposal.label_map.model_copy(update={'artifact_id':UUID(uncovered_ids['labels'])})})
                uncovered_proposal_body=canonical_bytes(uncovered_proposal)
                uncovered_proposal_ref=dict(artifact_id=uncovered_ids['proposal'],sha256=sha256(uncovered_proposal_body).hexdigest())
                uncovered_report=DefectProposeReport.model_validate_json(json.dumps(dict(params=positive_face_params.model_dump(mode='json'),proposal=uncovered_proposal_ref,protection=dict(required_document,artifact=dict(artifact_id=uncovered_ids['protection'],sha256=required_sha)),label_png=dict(artifact_id=uncovered_ids['png'],sha256=png_sha),faces=[dict(object_id=face_id,reason='degenerate_landmarks',threshold=0,candidates=0,overlapping_candidates=0)],warnings=['face_skipped'])))
                db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'defect:manual-uncovered','defects.propose','1','cpu',%s,%s,%s,%s,1,1)",(uncovered_task,case['Tenant'],protection_run,Jsonb([base['source'],landmark_pin,required_document['artifact'],manual_pixel,manual_protected_pixel]),positive_face_ids['params'],'a'*64,Jsonb(dict(backend='cpu'))))
                uncovered_outputs=[]
                uncovered_by_role={}
                for role,body,output,media in (('proposal',uncovered_proposal_body,'defect_candidates','application/json'),('report',canonical_bytes(uncovered_report),'defect_propose_report','application/json'),('labels',label_body,'defect_labels','application/octet-stream'),('protection',required_body,'defect_protection','application/octet-stream'),('png',png_body,'defect_labels_png','image/png')):
                    pin=defect_object(uncovered_ids[role],body,output,media,uncovered_task)
                    uncovered_outputs.append(pin);uncovered_by_role[role]=pin
                db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu',%s)",(case['Tenant'],uncovered_task,uuid4(),Jsonb(dict(backend='cpu'))))
                uncovered_result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(case['Tenant'],uncovered_task,Jsonb(uncovered_outputs),'7'*64)).fetchone()[0]
                db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(uncovered_result,uncovered_task))
                fixture['UncoveredManualDefectPins']=dict(Proposal=uncovered_by_role['proposal'],Report=uncovered_by_role['report'],Labels=uncovered_by_role['labels'],Protection=uncovered_by_role['protection'],Params=positive_face_params_pin)
                from photo_worker.contracts.face_parse import FaceParseParams,FaceLabels19
                from photo_worker.contracts.face_skin import FaceSkinParams
                from photo_worker.operations.mask.face_skin import skin_probability
                def start_skin_task(operation,node,params,inputs,resource):
                    task=str(uuid4())
                    params_pin=defect_object(str(uuid4()),canonical_bytes(params),'document','application/json')
                    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,%s,%s,'1',%s,%s,%s,%s,%s,1,1)",(task,case['Tenant'],protection_run,node,operation,resource,Jsonb(inputs),params_pin['artifact_id'],'a'*64,Jsonb(dict(backend='cpu',model_lock_sha='c'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64))))
                    return task
                def finish_skin_task(task,outputs):
                    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu',%s)",(case['Tenant'],task,uuid4(),Jsonb(dict(backend='cpu',model_lock_sha='c'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64))))
                    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(case['Tenant'],task,Jsonb(outputs),'9'*64)).fetchone()[0]
                    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
                parse_params=FaceParseParams.model_validate_json(json.dumps(dict(image=base['source'],faces=detection_pin,model_lock_id='c'*64)))
                parse_task=start_skin_task('face.parse','parse:positive',parse_params,[base['source'],detection_pin],'gpu')
                parse_array=np.zeros((300,400),dtype='<u2')
                parse_array[70:230,90:310]=1
                parse_array[140:150,170:230]=10
                parse_encoded=BytesIO()
                np.save(parse_encoded,parse_array,allow_pickle=False)
                parse_pixel=defect_object(str(uuid4()),parse_encoded.getvalue(),'face_labels','application/octet-stream',parse_task)
                parse_manifest=FaceLabels19.model_validate_json(json.dumps(dict(image=base['source'],faces=detection_pin,labels=parse_pixel,canvas=source_image['canvas'],transform_chain=source_image['transform_chain'],model_id='bisenet_face19',model_lock_id='c'*64,backend='cpu',preprocessing_id='bisenet_face19_crop512_v1',parsed_faces=1,mean_confidence=.9,warnings=[])))
                parse_pin=defect_object(str(uuid4()),canonical_bytes(parse_manifest),'face_labels_ref','application/json',parse_task)
                finish_skin_task(parse_task,[parse_pixel,parse_pin])
                # Independent parser catalog does not require a live defects.propose output.
                parse_found=face_preparation(parse_pin,'face.parse')
                assert len(parse_found)==1
                parse_params_id=db.execute('SELECT params_artifact_id FROM photo.tasks WHERE id=%s',(parse_task,)).fetchone()[0]
                assert str(parse_found[0][4])==str(parse_params_id)
                db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(parse_params_id,))
                assert not face_preparation(parse_pin,'face.parse')
                db.execute('UPDATE photo.artifacts SET expires_at=NULL WHERE id=%s',(parse_params_id,))
                assert len(face_preparation(parse_pin,'face.parse'))==1
                assert not face_preparation(parse_pin,'face.detect')
                def parse_pixels_catalog():
                    db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
                    db.execute('SET LOCAL ROLE photo_api')
                    try:
                        return db.execute('SELECT * FROM photo.semantic_proposal_face_parse_preparation_pixels(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',(case['Tenant'],case['Project'],case['Actor'],case['Session'],base['asset_version_id'],base['source']['artifact_id'],base['source']['sha256'],parse_pin['artifact_id'],parse_pin['sha256'],detection_pin['artifact_id'],detection_pin['sha256'],parse_pixel['artifact_id'],parse_pixel['sha256'])).fetchall()
                    finally:
                        db.execute('RESET ROLE')
                assert len(parse_pixels_catalog())==1
                db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(parse_pixel['artifact_id'],))
                assert not parse_pixels_catalog()
                db.execute('UPDATE photo.artifacts SET expires_at=NULL WHERE id=%s',(parse_pixel['artifact_id'],))
                assert len(parse_pixels_catalog())==1
                original_parse_inputs=db.execute('SELECT inputs FROM photo.tasks WHERE id=%s',(parse_task,)).fetchone()[0]
                db.execute('UPDATE photo.tasks SET inputs=%s WHERE id=%s',(Jsonb([base['source']]),parse_task))
                assert not parse_pixels_catalog()
                db.execute('UPDATE photo.tasks SET inputs=%s WHERE id=%s',(Jsonb(original_parse_inputs),parse_task))
                assert len(parse_pixels_catalog())==1
                skin_params=FaceSkinParams.model_validate_json(json.dumps(dict(labels=parse_pin)))
                skin_task=start_skin_task('mask.face_skin','skin:positive',skin_params,[parse_pin,parse_pixel],'cpu')
                skin_array=skin_probability(parse_array)
                skin_encoded=BytesIO()
                np.save(skin_encoded,skin_array,allow_pickle=False)
                skin_pixel=defect_object(str(uuid4()),skin_encoded.getvalue(),'skin_mask','application/octet-stream',skin_task)
                skin_mask=MaskRef.model_validate_json(json.dumps(dict(artifact=skin_pixel,canvas=source_image['canvas'],transform_chain=source_image['transform_chain'],selector='skin',confidence=.9,model_lock_id='c'*64,protected=False)))
                skin_root=defect_object(str(uuid4()),canonical_bytes(skin_mask),'skin_mask_ref','application/json',skin_task)
                skin_png_encoded=BytesIO()
                write_png16(skin_array,skin_png_encoded,max_bytes=png16_bound(400,300))
                skin_png=defect_object(str(uuid4()),skin_png_encoded.getvalue(),'skin_mask_png','image/png',skin_task)
                finish_skin_task(skin_task,[skin_pixel,skin_root,skin_png])
                fixture['SkinPNG']=skin_png
                fixture['SkinMaskRoot']=skin_root
                fixture['SkinLabels']=parse_pixel
                fixture['SkinParse']=parse_pin
                skin_face_defect_task=str(uuid4())
                skin_face_ids={role:str(uuid4()) for role in ('params','proposal','report','labels','protection','png')}
                skin_face_mask=skin_mask.model_dump(mode='json')
                skin_face_params=DefectProposeParams.model_validate_json(json.dumps(dict(image=base['source'],faces=[dict(object_id=face_id,landmarks=landmark_pin,skin=skin_face_mask,manual=None,manual_mode=False)],permanent=required_document)))
                skin_face_params_pin=defect_object(skin_face_ids['params'],canonical_bytes(skin_face_params),'document','application/json')
                db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'defect:4','defects.propose','1','cpu',%s,%s,%s,%s,1,1)",(skin_face_defect_task,case['Tenant'],protection_run,Jsonb([base['source'],landmark_pin,required_document['artifact'],skin_pixel]),skin_face_ids['params'],'a'*64,Jsonb(dict(backend='cpu'))))
                skin_face_proposal=defect_proposal.model_copy(update={'label_map':defect_proposal.label_map.model_copy(update={'artifact_id':UUID(skin_face_ids['labels'])})})
                skin_face_proposal_pin=defect_object(skin_face_ids['proposal'],canonical_bytes(skin_face_proposal),'defect_candidates','application/json',skin_face_defect_task)
                skin_face_protection=dict(required_document,artifact=dict(artifact_id=skin_face_ids['protection'],sha256=required_sha))
                skin_face_report=DefectProposeReport.model_validate_json(json.dumps(dict(params=skin_face_params.model_dump(mode='json'),proposal=skin_face_proposal_pin,protection=skin_face_protection,label_png=dict(artifact_id=skin_face_ids['png'],sha256=png_sha),faces=[dict(object_id=face_id,reason='degenerate_landmarks',threshold=0,candidates=0,overlapping_candidates=0)],warnings=['face_skipped'])))
                skin_face_outputs=[skin_face_proposal_pin]
                for role,body,output,media in (('report',canonical_bytes(skin_face_report),'defect_propose_report','application/json'),('labels',label_body,'defect_labels','application/octet-stream'),('protection',required_body,'defect_protection','application/octet-stream'),('png',png_body,'defect_labels_png','image/png')):
                    skin_face_outputs.append(defect_object(skin_face_ids[role],body,output,media,skin_face_defect_task))
                db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu',%s)",(case['Tenant'],skin_face_defect_task,uuid4(),Jsonb(dict(backend='cpu'))))
                skin_face_result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(case['Tenant'],skin_face_defect_task,Jsonb(skin_face_outputs),'7'*64)).fetchone()[0]
                db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(skin_face_result,skin_face_defect_task))
                skin_face_by_id={pin['artifact_id']:pin for pin in skin_face_outputs}
                fixture['SkinFaceDefectPins']=dict(Proposal=skin_face_by_id[skin_face_ids['proposal']],Report=skin_face_by_id[skin_face_ids['report']],Labels=skin_face_by_id[skin_face_ids['labels']],Protection=skin_face_by_id[skin_face_ids['protection']],Params=skin_face_params_pin)
                fixture['DefectLandmark']=landmark_pin
                fixture['DefectDetection']=detection_pin
                fixture['DefectFaceID']=face_id
                second_face_id=str(uuid4())
                second_landmark_task=str(uuid4())
                second_landmark_id=str(uuid4())
                second_face=Face68.model_validate_json(json.dumps(dict(object_id=second_face_id,points=[dict(x=.5,y=.5,confidence=1) for _ in range(68)],canvas=source_image['canvas'],transform_chain=source_image['transform_chain'])))
                second_detection_task=str(uuid4())
                second_detection_params=FaceDetectParams.model_validate_json(json.dumps(dict(image=base['source'],model_lock_id='c'*64)))
                second_detection_params_pin=defect_object(str(uuid4()),canonical_bytes(second_detection_params),'document','application/json')
                db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'detect:1','face.detect','1','gpu',%s,%s,%s,%s,1,1)",(second_detection_task,case['Tenant'],protection_run,Jsonb([base['source']]),second_detection_params_pin['artifact_id'],'a'*64,Jsonb(dict(backend='cpu',model_lock_sha='c'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64))))
                second_detection_doc=FaceBoxes.model_validate_json(json.dumps(dict(image=base['source'],faces=[dict(object_id=second_face_id,bbox_normalized=dict(x0=.25,y0=.25,x1=.75,y1=.75),confidence=1)],model_id='sfd',model_lock_id='c'*64,backend='cpu',preprocessing_id='sfd_long1024_bgr_v1',preview_width=400,preview_height=300,candidate_count=1,accepted_count=1,alpha_excluded_fraction=0,warnings=[])))
                second_detection_pin=defect_object(str(uuid4()),canonical_bytes(second_detection_doc),'face_boxes','application/json',second_detection_task)
                db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu',%s)",(case['Tenant'],second_detection_task,uuid4(),Jsonb(dict(backend='cpu',model_lock_sha='c'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64))))
                second_detection_result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(case['Tenant'],second_detection_task,Jsonb([second_detection_pin]),'8'*64)).fetchone()[0]
                db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(second_detection_result,second_detection_task))
                second_landmark_doc=FaceLandmarks.model_validate_json(json.dumps(dict(image=base['source'],faces=second_detection_pin,landmarks=[second_face.model_dump(mode='json')],model_id='fan2d',model_lock_id='c'*64,backend='cpu',preprocessing_id='fan2d_crop256_v1',mean_confidence=1,warnings=[])))
                second_landmark_params=LandmarkParams.model_validate_json(json.dumps(dict(image=base['source'],faces=second_detection_pin,model_lock_id='c'*64)))
                second_landmark_params_pin=defect_object(str(uuid4()),canonical_bytes(second_landmark_params),'document','application/json')
                db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'landmarks:1','face.landmarks','1','gpu',%s,%s,%s,%s,1,1)",(second_landmark_task,case['Tenant'],protection_run,Jsonb([base['source'],second_detection_pin]),second_landmark_params_pin['artifact_id'],'a'*64,Jsonb(dict(backend='cpu',model_lock_sha='c'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64))))
                second_landmark_pin=defect_object(second_landmark_id,canonical_bytes(second_landmark_doc),'face_landmarks','application/json',second_landmark_task)
                db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu',%s)",(case['Tenant'],second_landmark_task,uuid4(),Jsonb(dict(backend='cpu',model_lock_sha='c'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64))))
                second_landmark_result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(case['Tenant'],second_landmark_task,Jsonb([second_landmark_pin]),'6'*64)).fetchone()[0]
                db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(second_landmark_result,second_landmark_task))
                multi_face_defect_task=str(uuid4())
                multi_face_ids={role:str(uuid4()) for role in ('params','proposal','report','labels','protection','png')}
                multi_face_mask=dict(required_document,selector='manual',protected=False,object_id=second_face_id)
                multi_face_params=DefectProposeParams.model_validate_json(json.dumps(dict(image=base['source'],faces=[face_params.faces[0].model_dump(mode='json'),dict(object_id=second_face_id,landmarks=second_landmark_pin,skin=None,manual=multi_face_mask,manual_mode=True)],permanent=required_document)))
                multi_face_params_pin=defect_object(multi_face_ids['params'],canonical_bytes(multi_face_params),'document','application/json')
                db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'defect:2','defects.propose','1','cpu',%s,%s,%s,%s,1,1)",(multi_face_defect_task,case['Tenant'],protection_run,Jsonb([base['source'],landmark_pin,second_landmark_pin,required_document['artifact']]),multi_face_ids['params'],'a'*64,Jsonb(dict(backend='cpu'))))
                multi_face_proposal=defect_proposal.model_copy(update={'label_map':defect_proposal.label_map.model_copy(update={'artifact_id':UUID(multi_face_ids['labels'])})})
                multi_face_proposal_pin=defect_object(multi_face_ids['proposal'],canonical_bytes(multi_face_proposal),'defect_candidates','application/json',multi_face_defect_task)
                multi_face_protection=dict(required_document,artifact=dict(artifact_id=multi_face_ids['protection'],sha256=required_sha))
                multi_face_report=DefectProposeReport.model_validate_json(json.dumps(dict(params=multi_face_params.model_dump(mode='json'),proposal=multi_face_proposal_pin,protection=multi_face_protection,label_png=dict(artifact_id=multi_face_ids['png'],sha256=png_sha),faces=[face_report.faces[0].model_dump(mode='json'),dict(object_id=second_face_id,reason='degenerate_landmarks',threshold=0,candidates=0,overlapping_candidates=0)],warnings=['face_skipped'])))
                multi_face_outputs=[multi_face_proposal_pin]
                for role,body,output,media in (('report',canonical_bytes(multi_face_report),'defect_propose_report','application/json'),('labels',label_body,'defect_labels','application/octet-stream'),('protection',required_body,'defect_protection','application/octet-stream'),('png',png_body,'defect_labels_png','image/png')):
                    multi_face_outputs.append(defect_object(multi_face_ids[role],body,output,media,multi_face_defect_task))
                db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu',%s)",(case['Tenant'],multi_face_defect_task,uuid4(),Jsonb(dict(backend='cpu'))))
                multi_face_result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(case['Tenant'],multi_face_defect_task,Jsonb(multi_face_outputs),'7'*64)).fetchone()[0]
                db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(multi_face_result,multi_face_defect_task))
                multi_face_by_id={pin['artifact_id']:pin for pin in multi_face_outputs}
                fixture['MultiFaceDefectPins']=dict(Proposal=multi_face_by_id[multi_face_ids['proposal']],Report=multi_face_by_id[multi_face_ids['report']],Labels=multi_face_by_id[multi_face_ids['labels']],Protection=multi_face_by_id[multi_face_ids['protection']],Params=multi_face_params_pin)
                fixture['SecondDefectLandmark']=second_landmark_pin
                fixture['SecondDefectFaceID']=second_face_id

                recipe=dict(schema_version=3,source=base,base_style=base['source'],session_corrections=None,learning_version_id=None,overrides=[],layer_stack=None,renderer_version='v2.2',parameter_delta=dict(ev=0,log_r=0,log_g=0,log_b=0,saturation_delta=0,retouch_delta=0))
                recipe_body=json.dumps(recipe,sort_keys=True,separators=(',',':')).encode()
                recipe_id=str(uuid4())
                recipe_sha=sha256(recipe_body).hexdigest()
                recipe_key='proposal-recipe/'+recipe_id
                recipe_version=client.put_object(bucket,recipe_key,BytesIO(recipe_body),len(recipe_body),content_type='application/json',metadata={'sha256':recipe_sha}).version_id
                db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,%s,%s,%s,%s,%s,'application/json',%s,clock_timestamp(),'document')",(recipe_id,case['Tenant'],bucket,recipe_key,recipe_sha,len(recipe_body),recipe_version))
                stream=db.execute("INSERT INTO photo.v2_document_streams(tenant_id,project_id,asset_version_id,stream_type) VALUES(%s,%s,%s,'draft') RETURNING id",(case['Tenant'],case['Project'],base['asset_version_id'])).fetchone()[0]
                document=db.execute("INSERT INTO photo.v2_documents(tenant_id,project_id,stream_id,version_no,document_type,artifact_id,sha256) VALUES(%s,%s,%s,1,'EditingRecipeV3',%s,%s) RETURNING id",(case['Tenant'],case['Project'],stream,recipe_id,recipe_sha)).fetchone()[0]
                db.execute("UPDATE photo.v2_document_streams SET current_document_id=%s WHERE id=%s",(document,stream))
                if target_binding is not None:
                    target_recipe=dict(recipe,source=target_binding)
                    target_body=json.dumps(target_recipe,sort_keys=True,separators=(',',':')).encode()
                    target_id=str(uuid4());target_sha=sha256(target_body).hexdigest();target_key='proposal-recipe/'+target_id
                    target_object=client.put_object(bucket,target_key,BytesIO(target_body),len(target_body),content_type='application/json',metadata={'sha256':target_sha}).version_id
                    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,%s,%s,%s,%s,%s,'application/json',%s,clock_timestamp(),'document')",(target_id,case['Tenant'],bucket,target_key,target_sha,len(target_body),target_object))
                    target_stream=db.execute("INSERT INTO photo.v2_document_streams(tenant_id,project_id,asset_version_id,stream_type) VALUES(%s,%s,%s,'draft') RETURNING id",(case['Tenant'],case['Project'],target_version)).fetchone()[0]
                    target_document=db.execute("INSERT INTO photo.v2_documents(tenant_id,project_id,stream_id,version_no,document_type,artifact_id,sha256) VALUES(%s,%s,%s,1,'EditingRecipeV3',%s,%s) RETURNING id",(case['Tenant'],case['Project'],target_stream,target_id,target_sha)).fetchone()[0]
                    db.execute('UPDATE photo.v2_document_streams SET current_document_id=%s WHERE id=%s',(target_document,target_stream))


        if lock_only:
            # The fixture has committed. A separate writer must wait on selected
            # evidence until the scheduler transaction releases its SHARE locks.
            with db.transaction():
                db.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
                db.execute('SET LOCAL ROLE photo_scheduler')
                inventory=db.execute("SELECT * FROM photo.lock_semantic_proposal_inventory(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)",(case['Tenant'],case['Actor'],case['Session'],base['asset_version_id'],base['source']['artifact_id'],base['source']['sha256'],base['canvas_hash'],intent['reference_mask']['artifact_id'],intent['reference_mask']['sha256'],case['Snapshot'])).fetchall()
                assert inventory
                db.execute('RESET ROLE')
                assert manual_lock(lock_pins) is True
                with psycopg.connect(**database,autocommit=True) as writer:
                    writer.execute("SET lock_timeout='250ms'")
                    for table,column,identifier in [('artifacts','id',manual_protected_pixel['artifact_id']),('tasks','id',manual_task),('task_results','id',manual_result),('users','id',case['Actor']),('sessions','id',case['Session']),('projects','id',case['Project']),('snapshots','id',case['Snapshot']),('asset_versions','id',base['asset_version_id']),('artifacts','id',base['source']['artifact_id'])]:
                        with pytest.raises(psycopg.errors.LockNotAvailable):
                            writer.execute(f'UPDATE photo.{table} SET {column}={column} WHERE {column}=%s',(identifier,))
            # Frozen inventory is immutable even after the admission TX releases.
            with psycopg.connect(**database,autocommit=True) as writer:
                for mutation in (
                    "DELETE FROM photo.snapshot_items WHERE tenant_id=%s AND snapshot_id=%s",
                    "INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) SELECT tenant_id,snapshot_id,ordinal+1,asset_version_id,artifact_id,sha256,payload FROM photo.snapshot_items WHERE tenant_id=%s AND snapshot_id=%s",
                ):
                    with pytest.raises(psycopg.errors.CheckViolation,match='V22_FROZEN_ITEMS_IMMUTABLE'):
                        writer.execute(mutation,(case['Tenant'],case['Snapshot']))
                assert writer.execute("SELECT count(*) FROM photo.snapshot_items WHERE tenant_id=%s AND snapshot_id=%s",(case['Tenant'],case['Snapshot'])).fetchone()[0]==len(inventory)
            with psycopg.connect(**database,autocommit=True) as writer:
                writer.execute("UPDATE photo.users SET status='disabled' WHERE id=%s",(case['Actor'],))
            with db.transaction():
                assert manual_lock(lock_pins) is False
            with psycopg.connect(**database,autocommit=True) as writer:
                writer.execute("UPDATE photo.users SET status='active' WHERE id=%s",(case['Actor'],))
            with db.transaction():
                assert manual_lock(lock_pins) is True
            with psycopg.connect(**database,autocommit=True) as writer:
                writer.execute("SET lock_timeout='1s'")
                writer.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(manual_protected_pixel['artifact_id'],))
            with db.transaction():
                assert manual_lock(lock_pins) is False
            return

        if native_bootstrap:
            db.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation IN ('face.detect','mask.person','face.landmarks','face.parse','scene.parse','depth.predict') AND version='1'")
        if face_wire or scene_tail:
            db.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation IN ('mask.face_skin','defects.propose') AND version='1'")
        if os.environ.get("PHOTO_TEST_V27_PERSON_DEFECT_PARENT") == "1" or scene_tail or legacy_ordinary:
            # Isolated imported-qualification fixture only. The deployed default
            # remains disabled pending real model/corpus acceptance.
            assert db.execute("SELECT v2_enabled FROM photo.operation_registry WHERE operation='semantic.transfer' AND version='1'").fetchone() == (False,)
            db.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='semantic.transfer' AND version='1'")
        before=db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['Tenant'],)).fetchone()
        env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_SEMANTIC_INTENT_PUBLISH_CASE=json.dumps(fixture),GOCACHE=str(ROOT/'.cache/go-build'))
        if scene_wire: env['PHOTO_TEST_V27_SCENE_WIRE']='1'
        if native_terminal_worker: env['PHOTO_TEST_V27_NATIVE_TERMINAL_WORKER_FAILURE']='1'
        if native_terminal_draft: env['PHOTO_TEST_V27_NATIVE_TERMINAL_DRAFT']='1'
        if native_terminal_job: env['PHOTO_TEST_V27_NATIVE_TERMINAL_JOB']='mask.person' if native_terminal_person else 'face.detect'
        if native_terminal_cancel: env['PHOTO_TEST_V27_NATIVE_TERMINAL_CANCEL']='1'
        if person_wire: env['PHOTO_TEST_V27_PERSON_WIRE']='1'
        if face_wire: env['PHOTO_TEST_V27_FACE_WIRE']='1'
        if person_defect_recovery: env['PHOTO_TEST_V27_PERSON_DEFECT_RECOVERY']='1'
        if person_defect_execution: env['PHOTO_TEST_V27_PERSON_DEFECT_EXECUTION']='1'
        package='./internal/semanticproposal' if wrapper else './internal/v2draft'
        probe='^TestIntegrationNativePreparationBootstrap$' if native_bootstrap else '^TestIntegrationTransferDraftPreparer$' if fresh else '^TestIntegrationProposalIntentPublication$' if wrapper else '^TestIntegrationSemanticIntentPublication$'
        if scene_tail: probe='^TestIntegrationOrdinaryScenePipelineTail$'
        if legacy_ordinary: probe='^TestIntegrationOrdinaryScenePipelineTail$'
        parent_probe = os.environ.get('PHOTO_TEST_V27_PERSON_DEFECT_PARENT') == '1'
        full_recover = os.environ.get('PHOTO_TEST_V27_ORIGINAL_PERSON_RECOVER') == '1'
        result=run_named_go(['go','test',package,'-run',probe,'-count=1','-timeout','1200s' if scene_tail else '390s' if scene_wire else '540s' if go_lock_only else '960s' if full_recover else '660s' if parent_probe else '210s','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=1230 if scene_tail else 420 if scene_wire else 570 if go_lock_only else 990 if full_recover else 690 if parent_probe else 240)
        if os.environ.get('PHOTO_TEST_NATIVE_TIMING') == '1':
            print(result.stdout, end='')
            print(result.stderr, end='')
        assert result.returncode==0,result.stdout+result.stderr
        if legacy_ordinary:
            assert 'V27_LEGACY_ORDINARY_UNSEALED_RECOVERY_OK' in result.stdout
            return
        if scene_tail:
            assert 'V27_ORDINARY_SCENE_FINAL_PARENT_AND_RECEIPT_OK' in result.stdout
            assert 'V27_ORDINARY_SCENE_TRANSFER_TAIL_OK' in result.stdout
            return
        retry_success=os.environ.get('PHOTO_TEST_V27_TARGET_WORKER_RETRY_SUCCESS_OPERATION')
        if retry_success:
            assert 'V27_TARGET_WORKER_RETRY_SUCCESS_OK' in result.stdout
            stage_key={'mask.person':'v27_target_person_prepare','face.detect':'v27_target_detector_prepare','face.landmarks':'v27_target_face_prepare','face.parse':'v27_target_face_prepare','mask.face_skin':'v27_target_skin_prepare','defects.propose':'v27_target_defect_prepare'}[retry_success]
            tasks=db.execute("SELECT t.id,t.state,t.attempt,t.retry_count,t.next_attempt_at,t.canonical_result_id FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE r.tenant_id=%s AND r.config_snapshot ? %s AND t.operation=%s",(case['Tenant'],stage_key,retry_success)).fetchall()
            assert len(tasks)==1
            recovered=tasks[0]
            assert recovered[1:5]==('succeeded',2,1,None) and recovered[5] is not None
            assert db.execute("SELECT attempt,error->>'code',error->'retryable',finished_at IS NOT NULL FROM photo.task_attempts WHERE task_id=%s ORDER BY attempt",(recovered[0],)).fetchall()==[(1,'INFERENCE_FAILED',True,True),(2,None,None,True)]
            assert db.execute('SELECT count(*) FROM photo.task_results WHERE task_id=%s',(recovered[0],)).fetchone()[0]==1
            assert db.execute('SELECT attempt FROM photo.task_results WHERE id=%s',(recovered[5],)).fetchone()[0]==2
            output_count=db.execute('SELECT jsonb_array_length(outputs) FROM photo.task_results WHERE id=%s',(recovered[5],)).fetchone()[0]
            assert output_count>0
            assert db.execute("SELECT count(*) FROM photo.task_results z CROSS JOIN LATERAL jsonb_array_elements(z.outputs) ref(value) JOIN photo.artifacts a ON a.tenant_id=z.tenant_id AND a.id=(ref.value->>'artifact_id')::uuid WHERE z.id=%s AND a.created_by_task_id=z.task_id AND a.produced_attempt=2 AND a.produced_fence=z.fencing_token AND a.sha256=(ref.value->>'sha256')",(recovered[5],)).fetchone()[0]==output_count
            old_worker,old_fence,old_error,old_metrics=db.execute('SELECT worker_id,fencing_token,error,metrics FROM photo.task_attempts WHERE task_id=%s AND attempt=1',(recovered[0],)).fetchone()
            before_late=db.execute('SELECT to_jsonb(t) FROM photo.tasks t WHERE id=%s',(recovered[0],)).fetchone()
            before_events=db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND payload->>'task_id'=%s",(case['Tenant'],str(recovered[0]))).fetchone()
            db.execute('SET LOCAL ROLE photo_worker')
            assert db.execute('SELECT photo.commit_task_failure(%s,%s,%s,%s,%s,%s)',(recovered[0],1,old_fence,old_worker,Jsonb(old_error),Jsonb(old_metrics))).fetchone()[0]=='stale'
            db.execute('SET LOCAL ROLE NONE')
            assert db.execute('SELECT to_jsonb(t) FROM photo.tasks t WHERE id=%s',(recovered[0],)).fetchone()==before_late
            assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND payload->>'task_id'=%s",(case['Tenant'],str(recovered[0]))).fetchone()==before_events
            assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(recovered[0],)).fetchone()[0]==0
            assert db.execute("SELECT count(*) FROM photo.resource_reservations WHERE task_id=%s AND state='reserved'",(recovered[0],)).fetchone()[0]==0
        if native_terminal_job:
            assert 'V27_NATIVE_TERMINAL_PROCESS_ONE_OK' in result.stdout
            assert 'V27_NATIVE_TERMINAL_RECORDER_WRITER_LOCK_OK' in result.stdout
            parent=db.execute("SELECT (config_snapshot->'v27_detector_prepare'->>'parent_run_id')::uuid FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v27_detector_prepare'",(case['Tenant'],)).fetchone()[0]
            terminal_args=(case['Tenant'],case['Project'],case['Actor'],case['Session'],parent)
            for denied in ('photo_scheduler','photo_worker','photo_gateway','photo_maintenance','photo_publisher','photo_portal','photo_auth'):
                with db.transaction():
                    db.execute('SET LOCAL ROLE '+denied)
                    with pytest.raises(psycopg.errors.InsufficientPrivilege):
                        with db.transaction():
                            db.execute('SELECT * FROM photo.v27_native_initial_terminal_state(%s,%s,%s,%s,%s)',terminal_args)

            operation='mask.person' if native_terminal_person else 'face.detect'
            terminal=db.execute("SELECT t.state,t.cancel_requested,t.canonical_result_id,t.fencing_token FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE r.tenant_id=%s AND t.operation=%s AND (r.config_snapshot ? 'v27_detector_prepare' OR r.config_snapshot ? 'v27_person_prepare')",(case['Tenant'],operation)).fetchone()
            assert terminal==(('failed',False,None,1) if native_terminal_worker else ('cancelled',True,None,1))
            if native_terminal_worker:
                assert 'V27_NATIVE_TERMINAL_WORKER_FAILURE_OK' in result.stdout
                failure=db.execute("SELECT a.error,a.finished_at,t.attempt FROM photo.task_attempts a JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.task_id JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE r.tenant_id=%s AND t.operation=%s AND (r.config_snapshot ? 'v27_detector_prepare' OR r.config_snapshot ? 'v27_person_prepare')",(case['Tenant'],operation)).fetchone()
                assert failure[0]['code']=='INFERENCE_FAILED' and failure[0]['retryable'] is False and failure[1] is not None and failure[2]==1
                if native_terminal_person:
                    assert 'V27_NATIVE_FAILED_PERSON_ADMISSION_RETIRED_OK' in result.stdout
                    assert db.execute("SELECT v27_initial_recovery_done,v27_initial_recovery_token FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v27_person_prepare'",(case['Tenant'],)).fetchone()==(True,None)

            if native_terminal_draft:
                assert 'V27_NATIVE_TERMINAL_UNSEALED_DRAFT_OK' in result.stdout
                draft=db.execute("SELECT r.graph_sealed,r.status,r.cancel_requested,t.attempt FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=%s AND r.config_snapshot ? 'v27_person_prepare'",(case['Tenant'],)).fetchone()
                assert draft==(False,'cancelling',True,0)
                assert db.execute("SELECT count(*) FROM photo.v2_task_inputs i JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE r.tenant_id=%s AND r.config_snapshot ? 'v27_person_prepare'",(case['Tenant'],)).fetchone()==(0,)
                assert db.execute("SELECT count(*) FROM photo.idempotency_records i JOIN photo.runs r ON r.tenant_id=i.tenant_id AND r.id=i.resource_id WHERE r.tenant_id=%s AND r.config_snapshot ? 'v27_person_prepare' AND i.response_status=202",(case['Tenant'],)).fetchone()==(0,)

            detector=db.execute("SELECT v27_initial_recovery_done,v27_initial_recovery_token,v27_initial_manual_selection,cancel_requested FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v27_detector_prepare'",(case['Tenant'],)).fetchone()
            assert detector==(True,None,None,False)
            assert db.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v27_semantic'",(case['Tenant'],)).fetchone()==(0,)
            return
        if native_terminal_cancel:
            assert 'V27_NATIVE_TERMINAL_CANCEL_LATE_SELECTION_DENIED_OK' in result.stdout
            row=db.execute("SELECT t.state,t.cancel_requested,t.canonical_result_id,r.cancel_requested,r.v27_initial_recovery_done,r.v27_initial_recovery_token,r.v27_initial_manual_selection FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=%s AND r.config_snapshot ? 'v27_detector_prepare'",(case['Tenant'],)).fetchone()
            assert row==('cancelled',True,None,False,True,None,None)
            assert db.execute("SELECT count(*) FROM photo.task_results result JOIN photo.tasks task ON task.tenant_id=result.tenant_id AND task.id=result.task_id JOIN photo.runs stage ON stage.tenant_id=task.tenant_id AND stage.id=task.run_id WHERE stage.tenant_id=%s AND stage.config_snapshot ? 'v27_detector_prepare'",(case['Tenant'],)).fetchone()==(0,)
            assert db.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v27_semantic'",(case['Tenant'],)).fetchone()==(0,)
            return
        target_failure_operation=os.environ.get('PHOTO_TEST_V27_TARGET_WORKER_FAILURE_OPERATION') or ('mask.person' if os.environ.get('PHOTO_TEST_V27_TARGET_PERSON_WORKER_FAILURE')=='1' else '')
        if target_failure_operation:
            assert 'V27_TARGET_WORKER_FAILURE_RECOVERY_OK:'+target_failure_operation in result.stdout
            assert 'V27_NATIVE_TERMINAL_RECORDER_WRITER_LOCK_OK' in result.stdout
            config_key={'mask.person':'v27_target_person_prepare','face.detect':'v27_target_detector_prepare','face.landmarks':'v27_target_face_prepare','face.parse':'v27_target_face_prepare','mask.face_skin':'v27_target_skin_prepare','defects.propose':'v27_target_defect_prepare'}[target_failure_operation]
            row=db.execute("SELECT r.id,t.id,t.state,t.cancel_requested,t.canonical_result_id,t.attempt,r.graph_sealed FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=%s AND r.config_snapshot ? %s AND t.operation=%s",(case['Tenant'],config_key,target_failure_operation)).fetchone()
            retried=os.environ.get('PHOTO_TEST_V27_TARGET_WORKER_RETRY_ONCE')=='1'
            exhausted=os.environ.get('PHOTO_TEST_V27_TARGET_WORKER_RETRY_EXHAUST')=='1'
            expected_attempt=4 if exhausted else 2 if retried else 1
            assert row and row[2:]==('failed',False,None,expected_attempt,True)
            oom=os.environ.get('PHOTO_TEST_V27_FAILURE_CODE')=='RESOURCE_OOM'
            assert not oom or not (retried or exhausted)
            assert db.execute("SELECT error->>'code',error->'retryable',finished_at IS NOT NULL FROM photo.task_attempts WHERE task_id=%s AND attempt=%s",(row[1],expected_attempt)).fetchone()==('RESOURCE_OOM' if oom else 'INFERENCE_FAILED',exhausted or oom,True)
            if oom:
                assert 'V27_TARGET_WORKER_OOM_REFUSED_OK' in result.stdout
                assert db.execute('SELECT retry_count,oom_count,next_attempt_at FROM photo.tasks WHERE id=%s',(row[1],)).fetchone()==(0,0,None)
                assert db.execute('SELECT count(*) FROM photo.task_attempts WHERE task_id=%s',(row[1],)).fetchone()==(1,)
            if exhausted:
                assert 'V27_TARGET_WORKER_RETRY_EXHAUST_OK' in result.stdout
                assert db.execute("SELECT attempt,error->'retryable',finished_at IS NOT NULL FROM photo.task_attempts WHERE task_id=%s ORDER BY attempt",(row[1],)).fetchall()==[(i,True,True) for i in range(1,5)]
                assert db.execute('SELECT retry_count,next_attempt_at FROM photo.tasks WHERE id=%s',(row[1],)).fetchone()==(3,None)
            if retried:
                assert 'V27_TARGET_WORKER_RETRY_ONCE_OK' in result.stdout
                assert db.execute("SELECT attempt,error->'retryable',finished_at IS NOT NULL FROM photo.task_attempts WHERE task_id=%s ORDER BY attempt",(row[1],)).fetchall()==[(1,True,True),(2,False,True)]
                assert db.execute('SELECT retry_count,next_attempt_at FROM photo.tasks WHERE id=%s',(row[1],)).fetchone()==(1,None)
            assert db.execute('SELECT count(*) FROM photo.task_results WHERE task_id=%s',(row[1],)).fetchone()==(0,)
            assert db.execute('SELECT count(*) FROM photo.artifacts WHERE created_by_task_id=%s',(row[1],)).fetchone()==(0,)
            assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(row[1],)).fetchone()==(0,)
            assert db.execute("SELECT count(*) FROM photo.resource_reservations WHERE task_id=%s AND state='reserved'",(row[1],)).fetchone()==(0,)
            assert db.execute("SELECT v27_initial_recovery_done,v27_initial_recovery_token,v27_initial_manual_selection,cancel_requested FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v27_detector_prepare'",(case['Tenant'],)).fetchone()==(True,None,None,False)
            assert db.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v27_semantic'",(case['Tenant'],)).fetchone()==(0,)
            return
        if os.environ.get('PHOTO_TEST_V27_TARGET_PERSON_DRAFT')=='1' and os.environ.get('PHOTO_TEST_V27_TARGET_PERSON_ADVANCE')=='1':
            assert 'V27_TARGET_PERSON_AUTOMATIC_ADVANCE_RESTORE_FINISH_OK' in result.stdout
            assert 'V27_TARGET_MANIFEST_PRODUCTION_CHECKPOINT_REPLAY_OK' in result.stdout
            from v27_face_wire_probe import verify_target_person_draft
            verify_target_person_draft(db,client,case['Tenant'])
            if os.environ.get('PHOTO_TEST_V27_TARGET_DETECTOR_DRAFT')=='1':
                if os.environ.get('PHOTO_TEST_V27_TARGET_DETECTOR_ADVANCE')!='1':
                    assert 'V27_TARGET_DETECTOR_PHYSICAL_BLOCKED_BEGIN_REPLAY_REVOKE_OK' in result.stdout
                from v27_face_wire_probe import verify_target_detector_draft
                verify_target_detector_draft(db,client,case['Tenant'])
                if os.environ.get('PHOTO_TEST_V27_TARGET_DETECTOR_ADVANCE')=='1':
                    assert 'V27_TARGET_DETECTOR_AUTOMATIC_ADVANCE_RESTORE_FINISH_OK' in result.stdout
                    return
                if os.environ.get('PHOTO_TEST_V27_TARGET_DETECTOR_REGISTER')=='1':
                    assert 'V27_TARGET_DETECTOR_PHYSICAL_REGISTRATION_REPLAY_REVOKE_OK' in result.stdout
                    assert 'V27_TARGET_DETECTOR_PHYSICAL_ADMISSION_EVIDENCE_OK' in result.stdout
                if os.environ.get('PHOTO_TEST_V27_TARGET_DETECTOR_SEAL')=='1':
                    assert 'V27_TARGET_DETECTOR_PHYSICAL_ATOMIC_SEAL_REPLAY_REVOKE_OK' in result.stdout
                if os.environ.get('PHOTO_TEST_V27_TARGET_DETECTOR_EXECUTE')=='1':
                    assert 'V27_TARGET_DETECTOR_PHYSICAL_CANONICAL_EXECUTION_REPLAY_OK' in result.stdout
                    assert 'V27_TARGET_DETECTOR_PHYSICAL_RESULT_READ_LOCK_REPLAY_REVOKE_OK' in result.stdout
                if os.environ.get('PHOTO_TEST_V27_TARGET_DETECTOR_FINISH')=='1':
                    assert 'V27_TARGET_DETECTOR_PHYSICAL_FINISH_QUOTA_COMPLETED_REPLAY_OK' in result.stdout
                    assert 'V27_TARGET_DETECTOR_SAVED_QUEUED_COMPLETED_TRANSPORT_RESTORE_OK' in result.stdout
                if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_ADVANCE')=='1':
                    if os.environ.get('PHOTO_TEST_V27_NATIVE_ALL_TARGET_TRANSFER')=='1':
                        assert 'V27_TARGET_DEFECT_AUTOMATIC_ADVANCE_READY_FOR_TRANSFER_OK' in result.stdout
                        assert 'V27_NATIVE_ALL_TARGET_TRANSFER_RESTORE_FREEZE_REPLAY_SUBSTITUTION_OK' in result.stdout
                        assert 'V27_NATIVE_ALL_TARGET_TRANSFER_PERSISTED_CONFIG_OK' in result.stdout
                        assert 'V27_NATIVE_ALL_TARGET_TRANSFER_ORIGINAL_REFERENCE_SQL_OK' in result.stdout
                        assert 'V27_NATIVE_ALL_TARGET_TRANSFER_SAVED_SCOPE_OK' in result.stdout
                        assert 'V27_NATIVE_ALL_TARGET_TRANSFER_ADMISSION_BARRIER_OK' in result.stdout
                        assert 'V27_NATIVE_ALL_TARGET_TRANSFER_BLOCKED_BEGIN_ROLLBACK_OK' in result.stdout
                    else:
                        assert 'V27_TARGET_DEFECT_AUTOMATIC_ADVANCE_RESTORE_FINISH_OK' in result.stdout
                    if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_PROTECTION')=='1':
                        assert 'V27_TARGET_DEFECT_PROTECTION_PUBLICATION_REGISTRATION_REPLAY_REVOKE_OK' in result.stdout
                    from v27_face_wire_probe import verify_target_defect_prepared_params, verify_target_defect_transport
                    verify_target_defect_prepared_params(db,client,case['Tenant'])
                    verify_target_defect_transport(db,client,case['Tenant'])
                if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_REGISTER')=='1':
                    assert 'V27_TARGET_DEFECT_REGISTRATION_OWNERSHIP_REPLAY_REVOKE_OK' in result.stdout
                if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_TRANSPORT')=='1':
                    assert 'V27_TARGET_DEFECT_PHYSICAL_TRANSPORT_BINDING_REPLAY_REVOKE_OK' in result.stdout
                    if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_ADMISSION') == '1':
                        assert 'V27_TARGET_DEFECT_ADMISSION_REPLAY_SUBSTITUTION_REVOKE_QUOTA_OK' in result.stdout
                    if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_SEAL') == '1':
                        assert 'V27_TARGET_DEFECT_ATOMIC_SEAL_ROLLBACK_REPLAY_REVOKE_OK' in result.stdout
                    if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_EXECUTE') == '1':
                        assert 'V27_TARGET_DEFECT_NATIVE_EXECUTION_CANONICAL_REPLAY_OK' in result.stdout
                    if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_FINISH') == '1':
                        assert 'V27_TARGET_DEFECT_FINISH_CANONICAL_REPLAY_REVOKE_QUOTA_OK' in result.stdout
                    if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_CONTENT') == '1':
                        assert 'V27_TARGET_DEFECT_PHYSICAL_CONTENT_REPLAY_REVOKE_OK' in result.stdout
                    if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_CONSUMER') == '1':
                        assert 'V27_TARGET_DEFECT_CONSUMER_PACKET_LOCK_REPLAY_SUBSTITUTION_REVOKE_OK' in result.stdout
                    if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_RECOVERY') == '1':
                        assert 'V27_TARGET_DEFECT_SAVED_RECOVERY_TRANSPORT_REPLAY_SUBSTITUTION_REVOKE_OK' in result.stdout
                    from v27_face_wire_probe import verify_target_defect_transport
                    verify_target_defect_transport(db,client,case['Tenant'])
                if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_BEGIN')=='1':
                    assert 'V27_TARGET_DEFECT_BLOCKED_BEGIN_REPLAY_REVOKE_OK' in result.stdout
                if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_DEPENDENCIES')=='1':
                    assert 'V27_TARGET_DEFECT_DEPENDENCY_LOCK_REPLAY_REVOKE_OK' in result.stdout
                    assert 'V27_TARGET_DEFECT_COMPLETED_DEPENDENCIES_RUNNING_REFUSAL_OK' in result.stdout
                if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_PLAN')=='1':
                    assert 'V27_TARGET_DEFECT_PLAN_REPLAY_SUBSTITUTION_OK' in result.stdout
                if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_TASK')=='1':
                    assert 'V27_TARGET_DEFECT_PHYSICAL_TASK_INVENTORY_REPLAY_OK' in result.stdout
                if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_QUALIFICATION')=='1':
                    assert 'V27_TARGET_DEFECT_QUALIFICATION_REPLAY_SUBSTITUTION_REVOKE_OK' in result.stdout
                if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_PARAMS')=='1':
                    assert 'V27_TARGET_DEFECT_PHYSICAL_PARAMS_REPLAY_SUBSTITUTION_REVOKE_OK' in result.stdout
                    from v27_face_wire_probe import verify_target_defect_prepared_params
                    verify_target_defect_prepared_params(db,client,case['Tenant'])
                if os.environ.get('PHOTO_TEST_V27_TARGET_SKIN_PLAN')=='1':
                    if os.environ.get('PHOTO_TEST_V27_TARGET_SKIN_ADVANCE')=='1':
                        assert 'V27_TARGET_SKIN_AUTOMATIC_ADVANCE_RESTORE_FINISH_OK' in result.stdout
                    else:
                        assert 'V27_TARGET_SKIN_PHYSICAL_FACTORY_COMPILER_OK' in result.stdout
                        assert 'V27_TARGET_SKIN_BLOCKED_BEGIN_REPLAY_REVOKE_OK' in result.stdout
                        assert 'V27_TARGET_SKIN_REGISTRATION_REPLAY_REVOKE_OK' in result.stdout
                        assert 'V27_TARGET_SKIN_ADMISSION_EVIDENCE_REVOKE_OK' in result.stdout
                        assert 'V27_TARGET_SKIN_ATOMIC_SEAL_REPLAY_REVOKE_OK' in result.stdout
                        assert 'V27_TARGET_SKIN_CANONICAL_EXECUTION_REPLAY_OK' in result.stdout
                        assert 'V27_TARGET_SKIN_PHYSICAL_RESULT_CONSUMER_REPLAY_REVOKE_OK' in result.stdout
                        assert 'V27_TARGET_SKIN_FINISH_QUOTA_COMPLETED_REPLAY_OK' in result.stdout
                        assert 'V27_TARGET_SKIN_SAVED_QUEUED_TRANSPORT_RESTORE_OK' in result.stdout
                        assert 'V27_TARGET_SKIN_SAVED_COMPLETED_TRANSPORT_RESTORE_OK' in result.stdout
                    from v27_face_wire_probe import verify_target_skin_executed_draft
                    verify_target_skin_executed_draft(db,client,case['Tenant'])
                if os.environ.get('PHOTO_TEST_V27_TARGET_FACE_ADVANCE')=='1':
                    assert 'V27_TARGET_FACE_AUTOMATIC_ADVANCE_RESTORE_FINISH_OK' in result.stdout
                    from v27_face_wire_probe import verify_target_face_blocked_drafts
                    verify_target_face_blocked_drafts(db,client,case['Tenant'])
                if os.environ.get('PHOTO_TEST_V27_TARGET_FACE_PLAN')=='1' and os.environ.get('PHOTO_TEST_V27_TARGET_FACE_ADVANCE')!='1':
                    assert 'V27_TARGET_FACE_PHYSICAL_FACTORY_COMPILER_OK:face.landmarks' in result.stdout
                    assert 'V27_TARGET_FACE_PHYSICAL_FACTORY_COMPILER_OK:face.parse' in result.stdout
                    assert 'V27_TARGET_FACE_BLOCKED_DRAFT_REPLAY_OK:face.landmarks' in result.stdout
                    assert 'V27_TARGET_FACE_BLOCKED_DRAFT_REPLAY_OK:face.parse' in result.stdout
                    assert 'V27_TARGET_FACE_REGISTERED_REPLAY_OK:face.landmarks' in result.stdout
                    assert 'V27_TARGET_FACE_REGISTERED_REPLAY_OK:face.parse' in result.stdout
                    assert 'V27_TARGET_FACE_REGISTRATION_REVOKE_OK:face.landmarks' in result.stdout
                    assert 'V27_TARGET_FACE_REGISTRATION_REVOKE_OK:face.parse' in result.stdout
                    assert 'V27_TARGET_FACE_ADMISSION_PROOF_NEGATIVES_OK:face.landmarks' in result.stdout
                    assert 'V27_TARGET_FACE_ADMISSION_PROOF_NEGATIVES_OK:face.parse' in result.stdout
                    assert 'V27_TARGET_FACE_ATOMIC_SEAL_REPLAY_REVOKE_OK:face.landmarks' in result.stdout
                    assert 'V27_TARGET_FACE_ATOMIC_SEAL_REPLAY_REVOKE_OK:face.parse' in result.stdout
                    assert 'V27_TARGET_FACE_SAVED_QUEUED_TRANSPORT_RESTORE_OK:face.landmarks' in result.stdout
                    assert 'V27_TARGET_FACE_SAVED_QUEUED_TRANSPORT_RESTORE_OK:face.parse' in result.stdout
                    if os.environ.get('PHOTO_TEST_V27_TARGET_FACE_EXECUTE')=='1':
                        assert 'V27_TARGET_FACE_CANONICAL_EXECUTION_REPLAY_OK:face.landmarks' in result.stdout
                        assert 'V27_TARGET_FACE_CANONICAL_EXECUTION_REPLAY_OK:face.parse' in result.stdout
                        assert 'V27_TARGET_FACE_PHYSICAL_RESULT_READ_LOCK_REPLAY_REVOKE_OK:face.landmarks' in result.stdout
                        assert 'V27_TARGET_FACE_PHYSICAL_RESULT_READ_LOCK_REPLAY_REVOKE_OK:face.parse' in result.stdout
                        if os.environ.get('PHOTO_TEST_V27_TARGET_FACE_FINISH')=='1':
                            assert 'V27_TARGET_FACE_FINISH_QUOTA_COMPLETED_REPLAY_OK:face.landmarks' in result.stdout
                            assert 'V27_TARGET_FACE_FINISH_QUOTA_COMPLETED_REPLAY_OK:face.parse' in result.stdout
                            assert 'V27_TARGET_FACE_SAVED_COMPLETED_TRANSPORT_RESTORE_OK:face.landmarks' in result.stdout
                            assert 'V27_TARGET_FACE_SAVED_COMPLETED_TRANSPORT_RESTORE_OK:face.parse' in result.stdout
                    from v27_face_wire_probe import verify_target_face_blocked_drafts
                    verify_target_face_blocked_drafts(db,client,case['Tenant'])
            if os.environ.get('PHOTO_TEST_V27_NATIVE_ALL_TARGET_ADMIT')=='1':
                if os.environ.get('PHOTO_TEST_V27_NATIVE_ALL_TARGET_FIRST')=='1':
                    if os.environ.get('PHOTO_TEST_V27_NATIVE_ALL_TARGET_JOB')=='1':
                        if os.environ.get('PHOTO_TEST_V27_NATIVE_ALL_TARGET_CHECKPOINT_JOB')=='1':
                            assert 'V27_NATIVE_ALL_TARGET_CHECKPOINT_JOB_PARENT_ABSENT_OK' in result.stdout
                        elif os.environ.get('PHOTO_TEST_V27_NATIVE_ALL_TARGET_MIXED_REGISTRATION')=='1':
                            assert 'V27_NATIVE_ALL_TARGET_MIXED_REGISTRATION_PREPARED_OK' in result.stdout
                            assert 'V27_NATIVE_ALL_TARGET_MIXED_CORRUPT_NO_PUBLICATION_OK' in result.stdout
                            assert 'V27_NATIVE_ALL_TARGET_MIXED_STALE_INPUTS_NO_PUBLICATION_OK' in result.stdout
                            assert 'V27_NATIVE_REGISTERED_PREFLIGHT_NO_PUBLICATION_OK' in result.stdout
                            assert 'V27_NATIVE_REGISTERED_PREFLIGHT_CONCURRENT_WRITER_BLOCKED_OK' in result.stdout
                            assert 'V27_NATIVE_ALL_TARGET_MIXED_REGISTRATION_RECOVERED_OK' in result.stdout
                        elif os.environ.get('PHOTO_TEST_V27_NATIVE_ALL_TARGET_REGISTRATION_ACK')=='1':
                            assert 'V27_NATIVE_ALL_TARGET_REGISTRATION_COMMIT_ACK_LOST_OK' in result.stdout
                            assert 'V27_NATIVE_ALL_TARGET_REGISTRATION_COMMIT_DURABLE_REPLAY_OK' in result.stdout
                        elif os.environ.get('PHOTO_TEST_V27_NATIVE_ALL_TARGET_COMMIT_ACK')=='1':
                            assert 'V27_NATIVE_ALL_TARGET_SEALED_COMMIT_ACK_LOST_OK' in result.stdout
                            assert 'V27_NATIVE_ALL_TARGET_SEALED_COMMIT_DURABLE_REPLAY_OK' in result.stdout
                        else:
                            assert 'V27_NATIVE_ALL_TARGET_TRANSFER_JOB_BLOCKED_PARENT_OK' in result.stdout
                        assert 'V27_NATIVE_ALL_TARGET_TRANSFER_RECOVERY_JOB_HANDOFF_OK' in result.stdout
                        if os.environ.get('PHOTO_TEST_V27_NATIVE_ALL_TARGET_PARTIAL_JOB')=='1':
                            assert 'V27_NATIVE_ALL_TARGET_PARTIAL_PUBLICATION_INTERRUPTED_OK' in result.stdout
                            assert 'V27_NATIVE_ALL_TARGET_PARTIAL_PUBLICATION_RECOVERED_OK' in result.stdout
                            assert 'V27_NATIVE_ALL_TARGET_EXPIRED_RESERVATION_NO_PUBLICATION_OK' in result.stdout
                            if os.environ.get('PHOTO_TEST_V27_NATIVE_ALL_TARGET_LOST_ACK')=='1':
                                assert 'V27_NATIVE_ALL_TARGET_LOST_PUBLICATION_ACK_INJECTED_OK' in result.stdout
                                assert 'V27_NATIVE_ALL_TARGET_LOST_PUBLICATION_ACK_RECOVERED_OK' in result.stdout
                        if os.environ.get('PHOTO_TEST_V27_NATIVE_ALL_TARGET_HANDOFF')=='1':
                            assert 'V27_NATIVE_ALL_TARGET_HANDOFF_TARGET_LIFECYCLE_REFUSAL_OK' in result.stdout
                        if os.environ.get('PHOTO_TEST_V27_NATIVE_ALL_TARGET_HANDOFF_LOCKS')=='1':
                            assert 'V27_NATIVE_ALL_TARGET_HANDOFF_CONCURRENT_WRITERS_BLOCKED_OK' in result.stdout
                            assert 'V27_NATIVE_ALL_TARGET_HANDOFF_EXPIRED_LEASE_REFUSED_OK' in result.stdout
                            assert 'V27_NATIVE_ALL_TARGET_HANDOFF_CLOCK_EXPIRY_REFUSED_OK' in result.stdout
                            assert 'V27_NATIVE_ALL_TARGET_HANDOFF_POLICY_REVOKE_REFUSED_OK' in result.stdout
                            assert 'V27_NATIVE_ALL_TARGET_HANDOFF_POLICY_WRITER_BLOCKED_OK' in result.stdout
                            assert 'V27_NATIVE_ALL_TARGET_HANDOFF_UPSTREAM_QUALIFICATION_REVOKE_REFUSED_OK' in result.stdout
                            assert 'V27_NATIVE_ALL_TARGET_HANDOFF_QUALIFICATION_WRITER_BLOCKED_OK' in result.stdout
                    else:
                        assert 'V27_NATIVE_ALL_TARGET_TRANSFER_FIRST_PARENT_OK' in result.stdout
                elif os.environ.get('PHOTO_TEST_V27_NATIVE_ALL_TARGET_ROUTE')=='1':
                    assert 'V27_NATIVE_ALL_TARGET_TRANSFER_SAVED_PARENT_ROUTE_OK' in result.stdout
                elif os.environ.get('PHOTO_TEST_V27_NATIVE_ALL_TARGET_PROCESS')=='1':
                    assert 'V27_NATIVE_ALL_TARGET_TRANSFER_SEPARATE_PROCESS_OK' in result.stdout
                elif os.environ.get('PHOTO_TEST_V27_NATIVE_ALL_TARGET_LOAD')=='1':
                    assert 'V27_NATIVE_ALL_TARGET_TRANSFER_SERVER_LOAD_OK' in result.stdout
                else:
                    assert 'V27_NATIVE_ALL_TARGET_TRANSFER_DURABLE_ADMISSION_REPLAY_OK' in result.stdout
                    assert 'V27_NATIVE_ALL_TARGET_TRANSFER_REGISTERED_RECOVERY_OK' in result.stdout
                    assert 'V27_NATIVE_ALL_TARGET_TRANSFER_SAVED_PLAN_RESTORE_OK' in result.stdout
                    assert 'V27_NATIVE_ALL_TARGET_TRANSFER_ORIGINAL_RESTORE_OK' in result.stdout
                parent=db.execute("SELECT id,graph_sealed,status FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v27_native_targets_transfer'",(case['Tenant'],)).fetchone()
                assert parent and parent[1:] == (True,'queued')
                tasks=db.execute("SELECT state FROM photo.tasks WHERE tenant_id=%s AND run_id=%s",(case['Tenant'],parent[0])).fetchall()
                assert tasks and all(row[0]=='ready' for row in tasks)
                assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.run.queued'",(case['Tenant'],parent[0])).fetchone()[0]==1
                assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND payload->>'run_id'=%s AND routing_key='v2.run.queued'",(case['Tenant'],str(parent[0]))).fetchone()[0]==1
            else:
                assert db.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v27_semantic'",(case['Tenant'],)).fetchone()[0]==0
            return
        if os.environ.get('PHOTO_TEST_V27_TARGET_PERSON_DRAFT')=='1':
            assert 'V27_TARGET_PERSON_PHYSICAL_BLOCKED_DRAFT_REPLAY_OK' in result.stdout
            assert 'V27_TARGET_PERSON_PHYSICAL_TRANSPORT_REPLAY_REVOKE_OK' in result.stdout
            assert 'V27_TARGET_PERSON_PHYSICAL_ADMISSION_EVIDENCE_OK' in result.stdout
            if os.environ.get('PHOTO_TEST_V27_TARGET_PERSON_SEAL')=='1':
                assert 'V27_TARGET_PERSON_PHYSICAL_ATOMIC_SEAL_REPLAY_REVOKE_OK' in result.stdout
            if os.environ.get('PHOTO_TEST_V27_TARGET_PERSON_EXECUTE')=='1':
                assert 'V27_TARGET_PERSON_PHYSICAL_CANONICAL_EXECUTION_REPLAY_OK' in result.stdout
                assert 'V27_TARGET_PERSON_PHYSICAL_RESULT_AUTHORITY_OK' in result.stdout
            if os.environ.get('PHOTO_TEST_V27_TARGET_PERSON_FINISH')=='1':
                assert 'V27_TARGET_PERSON_PHYSICAL_FINALIZE_QUOTA_REPLAY_OK' in result.stdout
            assert 'V27_TARGET_MANIFEST_PRODUCTION_CHECKPOINT_REPLAY_OK' in result.stdout
            from v27_face_wire_probe import verify_target_person_draft
            verify_target_person_draft(db,client,case['Tenant'])
            assert db.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v27_semantic'",(case['Tenant'],)).fetchone()[0]==0
            return
        if person_wire:
            assert 'V27_CREATE_PERSON_NEW_OUTPUTS_COMMITTED_OK' in result.stdout
            assert 'V27_CREATE_PERSON_NEW_CANDIDATE_SELECTED_OK' in result.stdout
            assert 'V27_CREATE_PERSON_UNCOVERED_PARENT_REFUSED_OK' in result.stdout
            from v27_create_probe import verify_uncovered_person
            verify_uncovered_person(db,client,bucket,case)
            return
        if create_ordinary:
            assert 'V27_CREATE_COMMITTED_AND_REPLAYED_OK' in result.stdout
            assert 'V27_CREATE_HTTP_AUTH_ADMISSION_REPLAY_OK' in result.stdout
            if create_person:
                assert 'V27_CREATE_HTTP_MEASURED_PERSON_QUEUED_REPLAY_OK' in result.stdout
                assert 'V27_PERSON_STANDALONE_DURABLE_REPLAY_OK' in result.stdout
                assert 'V27_ORDINARY_PARENT_DURABLE_CONTINUATION_OK' in result.stdout
            assert 'V27_CREATE_DEPLOYMENT_BUILD_OK' in result.stdout
            assert 'V27_CREATE_EXECUTABLE_REPLAY_SHUTDOWN_OK' in result.stdout
            from v27_create_probe import verify_created_transfer
            verify_created_transfer(db,client,bucket,case,fixture,before)
            return
        if person_defect_recovery:
            parent_created = os.environ.get("PHOTO_TEST_V27_PERSON_DEFECT_PARENT") == "1"
            if os.environ.get('PHOTO_TEST_V27_ORIGINAL_PERSON') == '1':
                assert 'V27_ORIGINAL_PERSON_NEW_RESULT_VERIFIED_OK' in result.stdout
                assert 'V27_AUTOMATIC_INITIAL_DURABLE_HANDOFF_OK' in result.stdout
                if os.environ.get('PHOTO_TEST_V27_INITIAL_PROCESS_ONE') == '1':
                    assert 'V27_INITIAL_PROCESS_ONE_EXPIRED_LEASE_COMPLETED_OK' in result.stdout
                elif os.environ.get('PHOTO_TEST_V27_ORIGINAL_PERSON_RECOVER') == '1':
                    assert 'V27_ORIGINAL_PERSON_FULL_RECOVER_OK' in result.stdout
            if parent_created:
                assert "V27_NATIVE_DEFECT_PERSON_PARENT_ADMITTED_OK" in result.stdout
                assert "V27_NATIVE_PARENT_AMBIGUOUS_TARGET_REFUSED_OK" in result.stdout
                assert "V27_TARGET_MANIFEST_PRODUCTION_CHECKPOINT_REPLAY_OK" in result.stdout
                from v27_face_wire_probe import verify_native_parent_admission
                verify_native_parent_admission(db,client,case["Tenant"],fixture["RequiredProtection"])
            if os.environ.get("PHOTO_TEST_V27_PERSON_DEFECT_FRESH") == "1":
                assert "V27_NATIVE_DEFECT_PERSON_AUTOMATIC_FRESH_OK" in result.stdout
                assert "V27_NATIVE_DEFECT_PERSON_AUTOMATIC_ADVANCE_OK" in result.stdout
            assert "V27_NATIVE_FACE_NEW_OUTPUT_CHAIN_OK" in result.stdout
            assert "V27_NATIVE_DEFECT_PERSON_DEPENDENCIES_OK" in result.stdout
            assert "V27_NATIVE_DEFECT_PERSON_SAVED_RECOVERY_OK" in result.stdout
            from v27_face_wire_probe import verify_new_face_chain, verify_saved_person_defect
            verify_new_face_chain(db,client,case["Tenant"],fixture["Intent"]["reference"]["source"],parent_created=parent_created)
            verify_saved_person_defect(db,client,case["Tenant"],fixture["Intent"]["reference"]["source"],completed=person_defect_execution,parent_created=parent_created)
            if person_defect_execution:
                assert "V27_NATIVE_DEFECT_PERSON_CANONICAL_OUTPUT_OK" in result.stdout
                from v27_face_wire_probe import verify_new_defect_chain
                verify_new_defect_chain(db,client,case["Tenant"],fixture["Intent"]["reference"]["source"],parent_created=parent_created)
            return
        if face_wire:
            assert "V27_NATIVE_FACE_NEW_OUTPUT_CHAIN_OK" in result.stdout
            assert "V27_NATIVE_DEFECT_NEW_PROTECTION_OK" in result.stdout
            assert "V27_NATIVE_DEFECT_PERSON_OUTPUT_UNCOVERED_OK" in result.stdout
            assert "V27_ORIGINAL_PERSON_INPUT_AUTHORITY_OK" in result.stdout
            assert "V27_NATIVE_DEFECT_PERSON_DEPENDENCIES_OK" in result.stdout
            assert "V27_NATIVE_PERSON_COVERAGE_UNAVAILABLE_OK" in result.stdout
            assert "V27_NESTED_SEMANTIC_EQUALITY_OK" in result.stdout
            from v27_face_wire_probe import verify_new_face_chain, verify_new_defect_chain
            verify_new_face_chain(db,client,case["Tenant"],fixture["Intent"]["reference"]["source"])
            verify_new_defect_chain(db,client,case["Tenant"],fixture["Intent"]["reference"]["source"])
            return
        if native_bootstrap:
            assert 'V27_NATIVE_BOOTSTRAP_AND_REPLAY_OK' in result.stdout
            assert 'V27_PERSON_SAVED_PLAN_RECONSTRUCTED_OK' in result.stdout
            assert 'V27_PERSON_SAVED_ANCHOR_RECONSTRUCTED_OK' in result.stdout
            assert 'V27_PERSON_PRODUCTION_INITIAL_ENSURE_OK' in result.stdout
            assert 'V27_PERSON_PRODUCTION_INTERRUPTED_TRANSPORT_OK' in result.stdout
            assert 'V27_CREATE_PERSON_PREPARATION_REPLAY_OK' in result.stdout
            assert 'V27_PERSON_DURABLE_ADMISSION_RECOVERY_OK' in result.stdout
            assert 'V27_INITIAL_PERSON_ORDINARY_CONTINUATION_DENIED_OK' in result.stdout
            assert 'V27_PERSON_SAVED_ANCHOR_REVOKED_OK' in result.stdout
            assert 'V27_PERSON_SAVED_PLAN_REVOKED_OK' in result.stdout
            return
        if go_lock_only:
            assert 'V27_MANUAL_RETAINED_TRANSACTION_LOCK_OK' in result.stdout
            assert 'V27_MANUAL_MULTI_ROOT_LOCK_OK' in result.stdout
            assert 'V27_FACE_PREPARATION_CONTENT_OK' in result.stdout
            assert 'V27_FACE_PARSE_PREPARATION_CONTENT_OK' in result.stdout
            assert 'V27_FACE_PARSE_PREPARATION_PROFILES_OK' in result.stdout
            assert 'V27_FACE_PARSE_PREPARATION_DISCOVERY_OK' in result.stdout
            assert 'V27_FACE_PARSE_DISCOVERY_EXPIRY_OK' in result.stdout
            assert 'V27_FACE_PARSE_INVENTORY_EXACT_PINS_OK' in result.stdout
            assert 'V27_FACE_PARSE_PREPARATION_INVENTORY_CONTENT_OK' in result.stdout
            assert 'V27_FACE_PARSE_PREPARATION_QUALIFIED_OK' in result.stdout
            assert 'V27_FACE_PARSE_PREPARATION_RETAINED_PROOF_OK' in result.stdout
            assert 'V27_FACE_PARSE_QUALIFICATION_LOCK_OK' in result.stdout
            assert 'V27_FACE_PARSE_EVIDENCE_LOCK_OK' in result.stdout
            assert 'V27_FACE_PARSE_BATCH_TRANSACTION_LOCK_OK' in result.stdout
            assert 'V27_FACE_PARSE_INVENTORY_TRANSACTION_LOCK_OK' in result.stdout
            assert 'V27_FACE_PARSE_SOURCE_INVENTORY_LOCK_OK' in result.stdout
            assert 'V27_FACE_PARSE_INVENTORY_PHANTOM_WRITER_OK' in result.stdout
            assert 'V27_FACE_PARSE_INVENTORY_WRITER_RELEASE_OK' in result.stdout
            assert 'V27_SOURCE_PREPARATION_RETAINED_PROOF_OK' in result.stdout
            assert 'V27_SOURCE_AUTHORIZED_INPUT_SIZES_OK' in result.stdout
            assert 'V27_SOURCE_EVIDENCE_LOCK_OK' in result.stdout
            assert 'V27_SOURCE_EVIDENCE_SHAPE_OK' in result.stdout
            assert 'V27_FACE_PARSE_SOURCE_TRANSACTION_LOCK_OK' in result.stdout
            assert 'V27_FACE_PREPARATION_DISCOVERY_OK' in result.stdout
            assert 'V27_FACE_PREPARATION_INVENTORY_CONTENT_OK' in result.stdout
            assert 'V27_FACE_INVENTORY_TRANSACTION_LOCK_OK' in result.stdout
            assert 'V27_FACE_INVENTORY_PHANTOM_WRITER_OK' in result.stdout
            assert 'V27_FACE_SOURCE_INVENTORY_LOCK_OK' in result.stdout
            assert 'V27_FACE_AND_PARSE_SOURCE_INVENTORY_LOCK_OK' in result.stdout
            assert 'V27_FACE_PARSE_MANUAL_SOURCE_LOCK_OK' in result.stdout
            assert 'V27_PERSON_SHARED_SOURCE_INVENTORY_LOCK_OK' in result.stdout
            assert 'V27_GLOBAL_QUALIFICATION_POLICY_BARRIER_OK' in result.stdout
            assert 'V27_GLOBAL_POLICY_PHANTOM_AND_RELEASE_OK' in result.stdout
            assert 'V27_GLOBAL_POLICY_SNAPSHOT_OK' in result.stdout
            assert 'V27_GLOBAL_POLICY_STALE_SNAPSHOT_OK' in result.stdout
            assert 'V27_POLICY_SHARED_SOURCE_INVENTORY_LOCK_OK' in result.stdout
            assert 'V27_TRANSFER_QUALIFIED_SHARED_SOURCE_LOCK_OK' in result.stdout
            assert 'V27_EXECUTION_QUALIFIED_SHARED_SOURCE_LOCK_OK' in result.stdout
            assert 'V27_DEFECT_ORIGINAL_QUALIFICATION_PREPARATION_OK' in result.stdout
            assert 'V27_DEFECT_QUALIFICATION_RESOURCE_METRICS_OK' in result.stdout
            assert 'V27_DEFECT_MEASURED_COST_DENIAL_OK' in result.stdout
            assert 'V27_DEFECT_ORIGINAL_QUALIFIED_PROFILE_OK' in result.stdout
            assert 'V27_DEFECT_AUTHORIZED_PROFILE_CATALOG_OK' in result.stdout
            assert 'V27_DEFECT_PROFILE_LANDMARK_CATALOG_OK' in result.stdout
            assert 'V27_DEFECT_PROFILE_ORIGINAL_LANDMARK_BYTES_OK' in result.stdout
            assert 'V27_DEFECT_PROFILE_MULTIFACE_SOURCE_OK' in result.stdout
            assert 'V27_DEFECT_PROFILE_FACE_QUALIFIED_OK' in result.stdout
            assert 'V27_DEFECT_FACE_EXECUTION_RETAINED_PROOFS_OK' in result.stdout
            assert 'V27_DEFECT_PROFILE_FINAL_FACE_REVOCATION_OK' in result.stdout
            assert 'V27_DEFECT_PROFILE_FINAL_FACE_SOURCE_EXPIRY_OK' in result.stdout
            assert 'V27_DEFECT_PROFILE_SKIN_QUALIFIED_OK' in result.stdout
            assert 'V27_DEFECT_MASK_EXECUTION_RETAINED_PROOFS_OK' in result.stdout
            assert 'V27_DEFECT_PROFILE_SKIN_ROOT_REQUIRED_OK' in result.stdout
            assert 'V27_DEFECT_PROFILE_FINAL_MASK_FACE_REVOCATION_OK' in result.stdout
            assert 'V27_DEFECT_PROFILE_FINAL_MASK_PARSER_DETECTOR_REVOCATION_OK' in result.stdout
            assert 'V27_DEFECT_PROFILE_FINAL_MASK_PRODUCER_CHANGE_OK' in result.stdout
            assert 'V27_DEFECT_SOURCE_DERIVED_PROFILE_OK' in result.stdout
            assert 'V27_DEFECT_SOURCE_EXECUTION_RETAINED_PROOFS_OK' in result.stdout
            assert 'V27_DEFECT_SOURCE_EXECUTION_FILE_PINS_OK' in result.stdout
            assert 'V27_DEFECT_SOURCE_EXECUTION_FILE_PIN_MUTATION_DENIED_OK' in result.stdout
            assert 'V27_DEFECT_EXECUTION_SOURCE_TRANSACTION_OK' in result.stdout
            assert 'V27_DEFECT_FACE_ORIGINAL_PRODUCER_PROOFS_OK' in result.stdout
            assert 'V27_DEFECT_FACE_EXECUTION_TRANSACTION_OK' in result.stdout
            assert 'V27_DEFECT_MASK_ORIGINAL_PARSER_PROOFS_OK' in result.stdout
            assert 'V27_DEFECT_PARSER_EXECUTION_TRANSACTION_OK' in result.stdout
            assert 'V27_DEFECT_MANUAL_EXECUTION_ORIGINAL_PROOFS_OK' in result.stdout
            assert 'V27_DEFECT_MANUAL_EXECUTION_TRANSACTION_OK' in result.stdout
            assert 'V27_DEFECT_DECLARED_EXECUTION_FILE_LOCKS_OK' in result.stdout
            assert 'V27_DEFECT_EXECUTION_INVENTORY_TRANSACTION_OK' in result.stdout
            assert 'V27_DEFECT_ORIGIN_EXECUTION_TRANSACTION_OK' in result.stdout
            assert 'V27_DEFECT_ORIGIN_CONCURRENT_WRITE_DENIED_OK' in result.stdout
            assert 'V27_DEFECT_POLICY_EXECUTION_TRANSACTION_OK' in result.stdout
            assert 'V27_DEFECT_AUTHORIZED_EXECUTION_TRANSACTION_OK' in result.stdout
            assert 'V27_DEFECT_AUTHORITY_CONCURRENT_WRITE_DENIED_OK' in result.stdout
            assert 'V27_DEFECT_PROFILE_REQUIRED_PROTECTION_OK' in result.stdout
            assert 'V27_PROTECTION_ROOT_DISCOVERY_OK' in result.stdout
            assert 'V27_DEFECT_PROFILE_ORIGINAL_PARAMS_BYTES_OK' in result.stdout
            assert 'V27_DEFECT_PROFILE_ORIGINAL_MASK_BYTES_OK' in result.stdout
            assert 'V27_DEFECT_PROFILE_CAPACITY_BEFORE_MASK_IO_OK' in result.stdout
            assert 'V27_DEFECT_PROFILE_CATALOG_REVOCATION_OK' in result.stdout
            assert 'V27_DEFECT_QUALIFICATION_FINAL_RECHECK_OK' in result.stdout
            assert 'V27_DEFECT_ORIGINAL_QUALIFICATION_LOCK_OK' in result.stdout
            assert 'V27_DEFECT_QUALIFICATION_AUTHORITY_LOCK_OK' in result.stdout
            assert 'V27_DEFECT_QUALIFICATION_LOCK_ACL_OK' in result.stdout
            assert 'V27_DEFECT_EXECUTION_LOCK_ACL_OK' in result.stdout
            assert 'V27_DEFECT_EXECUTION_SCOPE_SUBSTITUTION_DENIED_OK' in result.stdout
            assert 'V27_DEFECT_MANUAL_PARAMS_RETAINED_OK' in result.stdout
            assert 'V27_DEFECT_MANUAL_PARAMS_RECHECK_OK' in result.stdout
            assert 'V27_DEFECT_NATIVE_PARAMS_OWNER_DENIED_OK' in result.stdout
            assert 'V27_DEFECT_VERSION_PARAMS_PUBLICATION_OK' in result.stdout
            assert 'V27_DEFECT_VERSION_PARAMS_PROOF_LOCK_OK' in result.stdout
            assert 'V27_DEFECT_PUBLISHED_TASK_COMPILE_OK' in result.stdout
            assert 'V27_DEFECT_PUBLISHED_EXECUTION_LOCKS_OK' in result.stdout
            assert 'V27_DEFECT_BLOCKED_DRAFT_CREATE_REPLAY_OK' in result.stdout
            assert 'V27_DEFECT_INPUT_REGISTRATION_REPLAY_OK' in result.stdout
            assert 'V27_DEFECT_INPUT_DURABLE_REPLAY_OK' in result.stdout
            assert 'V27_DEFECT_PERSISTENCE_COORDINATOR_OK' in result.stdout
            assert 'V27_DEFECT_PERSON_ADMISSION_PREPARATION_LOCK_OK' in result.stdout
            assert 'V27_DEFECT_AUTHORITY_PACKETS_ROUNDTRIP_OK' in result.stdout
            assert 'V27_DEFECT_REGISTERED_PERSON_ADMISSION_OK' in result.stdout
            assert 'V27_DEFECT_UNIFIED_ADMISSION_PACKET_OK' in result.stdout
            assert 'V27_DEFECT_COMPOSED_ADMISSION_SUBSTITUTION_DENIED_OK' in result.stdout
            assert 'V27_DEFECT_SQL_CLOSURE_MATCH_OK' in result.stdout
            assert 'V27_DEFECT_COMPOSED_ADMISSION_ACL_OK' in result.stdout
            assert 'V27_DEFECT_SQL_LANDMARK_INVENTORY_OK' in result.stdout
            assert 'V27_DEFECT_SQL_MASK_ROOT_INVENTORY_OK' in result.stdout
            assert 'V27_DEFECT_SKIN_ROOT_LINEAGE_DENIED_OK' in result.stdout
            assert 'V27_DEFECT_SQL_POLICY_EXPECTATIONS_OK' in result.stdout
            assert 'V27_DEFECT_SQL_INTENT_SNAPSHOT_ANCHOR_OK' in result.stdout
            assert 'V27_DEFECT_SQL_GRAPH_BUDGET_OK' in result.stdout
            assert 'V27_DEFECT_NATIVE_DRAFT_RESULT_DENIED_OK' in result.stdout
            assert 'V27_DEFECT_NATIVE_SEAL_REPLAY_OK' in result.stdout
            assert 'V27_DEFECT_NATIVE_DISPATCH_CLAIM_OK' in result.stdout
            assert 'V27_DEFECT_NATIVE_GATEWAY_INPUTS_OK' in result.stdout
            assert 'V27_DEFECT_NATIVE_GATEWAY_OUTPUT_METADATA_OK' in result.stdout
            assert 'V27_DEFECT_NATIVE_TASK_RESULT_METADATA_OK' in result.stdout
            assert 'V27_DEFECT_NATIVE_RESULT_CATALOG_OK' in result.stdout
            assert 'V27_DEFECT_NATIVE_PENDING_COORDINATOR_OK' in result.stdout
            assert 'V27_DEFECT_FIRST_PLAN_RECONSTRUCTION_OK' in result.stdout
            assert 'V27_DEFECT_FIRST_STAGE_ORCHESTRATOR_OK' in result.stdout
            assert 'V27_DEFECT_ORIGIN_FIRST_STAGE_OK' in result.stdout
            assert 'V27_DEFECT_EXACT_PRODUCER_DISCOVERY_STAGE_OK' in result.stdout
            assert 'V27_DEFECT_SERVER_POLICY_SELECTION_OK' in result.stdout
            assert 'V27_DEFECT_REPORT_DISCOVERY_OK' in result.stdout
            assert 'V27_DEFECT_DISCOVERY_SQL_ACL_OK' in result.stdout
            assert 'V27_DEFECT_NATIVE_COMPLETED_AUTHORITY_OK' in result.stdout
            assert 'V27_DEFECT_NATIVE_COMPLETED_READ_ONLY_OK' in result.stdout
            assert 'V27_DEFECT_NATIVE_COMPLETED_TRANSPORT_RECOVERY_OK' in result.stdout
            assert 'V27_DEFECT_NATIVE_COMMITTED_ADMISSION_OK' in result.stdout
            assert 'V27_DEFECT_SQL_CONTENT_HASH_MATCH_OK' in result.stdout
            assert 'V27_DEFECT_GENERIC_SEAL_BYPASS_DENIED_OK' in result.stdout
            assert 'V27_DEFECT_GENERIC_SEAL_RUN_FENCE_OK' in result.stdout
            assert 'V27_DEFECT_NATIVE_PARAMS_REGISTRATION_ACL_OK' in result.stdout
            assert 'V27_DEFECT_REGISTRATION_STATE_SUBSTITUTION_DENIED_OK' in result.stdout
            assert 'V27_DEFECT_INPUT_MANIFEST_SUBSTITUTION_DENIED_OK' in result.stdout
            assert 'V27_TRANSFER_QUALIFICATION_RETAINED_LOCK_OK' in result.stdout
            assert 'V27_TRANSFER_QUALIFICATION_REVOKE_LOCK_OK' in result.stdout
            assert 'V27_TRANSFER_QUALIFICATION_LOCK_ACL_OK' in result.stdout
            assert 'V27_TRANSFER_QUALIFICATION_IMPORT_LOCK_OK' in result.stdout
            assert 'V27_REQUIRED_POLICY_SELECTION_OK' in result.stdout
            assert 'V27_SELECTED_EVIDENCE_POLICY_MATCH_OK' in result.stdout
            assert 'V27_REQUIRED_POLICY_PREFLIGHT_DENIAL_OK' in result.stdout
            assert 'V27_PERSON_SHARED_QUALIFICATION_REJECTION_OK' in result.stdout
            assert 'V27_FACE_PARSE_MANUAL_REJECTION_OK' in result.stdout
            assert 'V27_FACE_AND_PARSE_INVENTORY_REJECTION_OK' in result.stdout
            assert 'V27_PROTECTION_PREPARATION_RETAINED_PROOF_OK' in result.stdout
            assert 'V27_PROTECTION_EVIDENCE_LOCK_OK' in result.stdout
            assert 'V27_PROTECTION_BATCH_DECLARATION_LOCK_OK' in result.stdout
            assert 'V27_PERSON_COVERAGE_PROTECTION_PROOF_OK' in result.stdout
            assert 'V27_PERSON_RETAINED_PROTECTION_LOCK_OK' in result.stdout
            assert 'V27_FACE_PREPARATION_QUALIFIED_OK' in result.stdout
            assert 'V27_FACE_PREPARATION_RETAINED_PROOF_OK' in result.stdout
            assert 'V27_FACE_QUALIFICATION_LOCK_OK' in result.stdout
            assert 'V27_FACE_EVIDENCE_LOCK_OK' in result.stdout
            assert 'V27_FACE_BATCH_TRANSACTION_LOCK_OK' in result.stdout
            native_tasks=db.execute("SELECT t.id FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.tenant_id=%s AND r.config_snapshot ? 'v27_defect_prepare' AND r.graph_sealed AND r.status='queued' AND t.state='ready'",(case['Tenant'],)).fetchall()
            assert len(native_tasks)==1
            objects_config=client._photo_test_config
            wire_env=dict(env,PHOTO_TEST_V27_GATEWAY_TASK=str(native_tasks[0][0]),PHOTO_TEST_V27_REPO_ROOT=str(ROOT),PHOTO_TEST_MINIO_URL=objects_config['Endpoint'],PHOTO_TEST_MINIO_BUCKET=bucket,PHOTO_TEST_MINIO_ACCESS=objects_config['AccessKey'],PHOTO_TEST_MINIO_SECRET=objects_config['SecretKey'])
            wire=run_named_go(['go','test','./internal/gateway','-run','^TestIntegrationV27IndexedDownloads$','-count=1','-timeout','150s','-v'],cwd=ROOT/'backend',env=wire_env,capture_output=True,text=True,timeout=180)
            assert wire.returncode==0,wire.stdout+wire.stderr
            with db.transaction():
                foreign=seed_original_composition(db,snapshot_canvas=True)
                db.execute("INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES(%s,%s,'editor')",(foreign['Tenant'],case['Actor']))
            foreign_base=json.loads(foreign['Params'])['base']
            foreign_scope={key:foreign[key] for key in ('Tenant','Actor','Session','Project')}
            foreign_scope['Reference']=foreign_base
            def native_oracle_bytes(ref):
                row=db.execute("SELECT bucket,object_key,object_version,bytes FROM photo.artifacts WHERE tenant_id=%s AND id=%s AND sha256=%s AND state='ready' AND verified_at IS NOT NULL",(case['Tenant'],ref['artifact_id'],ref['sha256'])).fetchone()
                assert row is not None and 0 < row[3] <= 8*1024*1024
                response=client.get_object(row[0],row[1],version_id=row[2])
                try:
                    payload=response.read(row[3]+1)
                finally:
                    response.close()
                    response.release_conn()
                assert len(payload)==row[3] and sha256(payload).hexdigest()==ref['sha256']
                return payload
            selected_root=json.loads(native_oracle_bytes(intent['reference_mask']))
            selected_oracle=np.load(BytesIO(native_oracle_bytes(selected_root['artifact'])),allow_pickle=False)
            native_outputs=db.execute("SELECT z.outputs FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id WHERE t.tenant_id=%s AND t.id=%s",(case['Tenant'],native_tasks[0][0])).fetchone()[0]
            protection_oracle=np.load(BytesIO(native_oracle_bytes(native_outputs[1])),allow_pickle=False)
            assert selected_oracle.shape==protection_oracle.shape==(300,400)
            for array in (selected_oracle,protection_oracle):
                assert array.dtype==np.dtype('<f4') and np.isfinite(array).all() and ((array>=0)&(array<=1)).all()
            native_selection_count=int(np.count_nonzero(selected_oracle>0))
            assert native_selection_count>0
            native_selection_overlap=bool(np.any((selected_oracle>0)&(protection_oracle>0)))
            if native_disjoint:
                assert native_selection_count==1 and not native_selection_overlap, "independent native disjoint selection oracle"
            native_config=db.execute("SELECT r.config_snapshot->'v27_defect_prepare' FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.tenant_id=%s AND t.id=%s",(case['Tenant'],native_tasks[0][0])).fetchone()[0]
            content_fixture=dict(fixture,Project=case['Project'],NativeForeign=foreign_scope,NativeIntent=native_config['intent'],NativeSnapshot=native_config['snapshot'],NativeSelectionOverlap=native_selection_overlap,NativeSelectionCount=native_selection_count)
            content_env=dict(wire_env,PHOTO_SEMANTIC_INTENT_PUBLISH_CASE=json.dumps(content_fixture))
            content=run_named_go(['go','test','./internal/semanticproposal','-run','^TestIntegration(NativeDefectContent|SavedDefectConfigIsolation)$','-count=1','-timeout','360s','-v'],cwd=ROOT/'backend',env=content_env,capture_output=True,text=True,timeout=390)
            assert content.returncode==0,content.stdout+content.stderr
            assert 'V27_SAVED_DEFECT_CONFIG_ISOLATION_OK' in content.stdout
            assert 'V27_NATIVE_DEFECT_WORKER_CONTENT_OK' in content.stdout
            assert 'V27_NATIVE_DEFECT_QUALIFIED_PRODUCER_TRANSACTION_LOCK_OK' in content.stdout
            assert 'V27_NATIVE_DEFECT_WORKER_PROVENANCE_SCOPE_OK' in content.stdout
            assert 'V27_NATIVE_DEFECT_REAL_TENANT_ISOLATION_OK' in content.stdout
            assert 'V27_NATIVE_DEFECT_RESULT_TRANSACTION_LOCK_OK' in content.stdout
            assert 'V27_NATIVE_DEFECT_PARENT_BINDING_OK' in content.stdout
            assert 'V27_NATIVE_DEFECT_PROTECTION_DOCUMENT_PREPARED_OK' in content.stdout
            assert 'V27_NATIVE_DEFECT_PROTECTION_DOCUMENT_PUBLICATION_OK' in content.stdout
            assert 'V27_NATIVE_DEFECT_PROTECTION_REGISTRATION_OK' in content.stdout
            assert 'V27_NATIVE_DEFECT_REGISTERED_PROTECTION_READ_OK' in content.stdout
            assert 'V27_NATIVE_DEFECT_PROTECTION_COORDINATOR_OK' in content.stdout
            assert 'V27_NATIVE_DEFECT_SELECTION_SEPARATION_OK' in content.stdout
            if native_disjoint:
                assert 'V27_NATIVE_DEFECT_SELECTION_TRANSACTION_LOCK_OK' in content.stdout
                assert 'V27_NATIVE_DEFECT_REFERENCE_PROTECTION_TRANSACTION_LOCK_OK' in content.stdout
                assert 'V27_NATIVE_DEFECT_REFERENCE_CONTINUATION_TRANSACTION_LOCK_OK' in content.stdout
                assert 'V27_NATIVE_DEFECT_SOURCE_CONTINUATION_TRANSACTION_LOCK_OK' in content.stdout
                assert 'V27_NATIVE_DEFECT_SOURCE_PREPARATION_TRANSACTION_LOCK_OK' in content.stdout
                assert 'V27_NATIVE_DEFECT_TRANSFER_REFERENCE_DOCUMENTS_OK' in content.stdout
                assert 'V27_NATIVE_DEFECT_TRANSFER_PLAN_TARGET_GATES_OK' in content.stdout
                assert 'V27_NATIVE_DEFECT_TRANSFER_REGISTRATION_REPLAY_OK' in content.stdout
                assert 'V27_NATIVE_DEFECT_TRANSFER_STORED_INPUTS_OK' in content.stdout
                assert 'V27_NATIVE_DEFECT_TRANSFER_PREPARER_RECOVERY_OK' in content.stdout
                assert 'V27_NATIVE_DEFECT_TRANSFER_ACTIVATION_ROLLBACK_OK' in content.stdout
                assert 'V27_NATIVE_DEFECT_TRANSFER_ACTIVATION_COMMIT_OK' in content.stdout
                assert 'V27_NATIVE_DEFECT_TRANSFER_COMPLETED_REPLAY_OK' in content.stdout
                assert 'V27_NATIVE_DEFECT_TRANSFER_ACTIVATION_WRAPPER_OK' in content.stdout
                assert 'V27_NATIVE_DEFECT_TRANSFER_COORDINATOR_RECOVERY_OK' in content.stdout
                assert 'V27_NATIVE_DEFECT_TRANSFER_CANCELLATION_REPLAY_OK' in content.stdout
                assert 'V27_NATIVE_DEFECT_TRANSFER_READY_TRANSPORT_SCOPE_OK' in content.stdout
                assert 'V27_NATIVE_DEFECT_POLICY_REVOCATION_AND_RECOVERY_OK' in content.stdout
            assert 'V27_NATIVE_DEFECT_QUALIFIED_PERSON_COVERAGE_OK' in content.stdout
            assert 'V27_NATIVE_DEFECT_QUALIFIED_PERSON_TRANSACTION_LOCK_OK' in content.stdout
            return
        if race_only:
            assert 'V27_MANUAL_IO_RACES_OK' in result.stdout
            return
        if race:
            assert ('V27_PROPOSAL_SOURCE_DURING_PUT_OK' if race=='source' else 'V27_PROPOSAL_REVOKE_DURING_PUT_OK') in result.stdout
            pending=db.execute("SELECT artifact_id,bytes,state,object_version FROM photo.v2_draft_uploads WHERE tenant_id=%s AND bucket=%s",(case['Tenant'],bucket)).fetchall()
            assert len(pending)==1
            artifact,size,state,version=pending[0]
            assert state=='pending' and version is None
            assert db.execute('SELECT count(*) FROM photo.artifacts WHERE id=%s',(artifact,)).fetchone()[0]==0
            key=f"tenants/{case['Tenant']}/v2-drafts/{artifact}"
            obj=client.stat_object(bucket,key)
            response=client.get_object(bucket,key,version_id=obj.version_id)
            try:payload=response.read()
            finally:response.close();response.release_conn()
            assert json.loads(payload)==intent and len(payload)==size
            assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==(before[0],before[1]+size)
            restored=db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') RETURNING id",(case['Actor'],uuid4().bytes)).fetchone()[0]
            if race=='source':
                db.execute("UPDATE photo.asset_versions SET review_state='accepted' WHERE id=%s",(base['asset_version_id'],))
            fixture.update(Session=str(restored),RevokeDuringPut=False,SourceDuringPut=False)
            env['PHOTO_SEMANTIC_INTENT_PUBLISH_CASE']=json.dumps(fixture)
            recovered=run_named_go(['go','test',package,'-run',probe,'-count=1','-timeout','210s','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=240)
            assert recovered.returncode==0,recovered.stdout+recovered.stderr
            assert 'V27_PROPOSAL_PUBLICATION_OK' in recovered.stdout
            ready=db.execute("SELECT a.object_version,u.state FROM photo.v2_draft_uploads u JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id WHERE u.artifact_id=%s",(artifact,)).fetchone()
            assert ready==(obj.version_id,'ready')
            assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==(before[0]+size,before[1])
            return
        assert ('V27_PROPOSAL_PUBLICATION_OK' if wrapper else 'V27_INTENT_PUBLICATION_OK') in result.stdout
        rows=db.execute("SELECT a.bytes,a.object_key,a.object_version,a.sha256 FROM photo.v2_draft_uploads u JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id WHERE u.tenant_id=%s AND u.bucket=%s AND u.state='ready' AND u.upload_contract='Document' AND u.artifact_id<>%s",(case['Tenant'],bucket,intent['reference_mask']['artifact_id'])).fetchall()
        total_size=0
        documents=[]
        for size,key,version,digest in rows:
            response=client.get_object(bucket,key,version_id=version)
            try:payload=response.read()
            finally:response.close();response.release_conn()
            assert len(payload)==size and sha256(payload).hexdigest()==digest
            total_size+=size
            documents.append(json.loads(payload))
        # Oriented/native coverage also publishes the independently verified
        # original manual defect Params under an accepted-version document.
        expected_count = (4 if oriented else 3) if batch else 2 if not wrapper else 1
        assert len(rows)==expected_count, [(size, sorted(document.keys())) for (size, *_), document in zip(rows, documents)]
        if batch and oriented:
            assert 'V27_DEFECT_VERSION_PARAMS_PUBLICATION_OK' in result.stdout
            expected_manual = json.loads(canonical_bytes(positive_face_params))
            manual_documents = [(row, document) for row, document in zip(rows, documents)
                if row[3] == fixture['ManualFaceDefectPins']['Params']['sha256']]
            assert len(manual_documents)==1 and manual_documents[0][1]==expected_manual
        assert intent in documents
        if batch or not wrapper:
            assert ('V27_TRANSFER_BATCH_PUBLICATION_OK' if batch else 'V27_TRANSFER_PARAMS_PUBLICATION_OK') in result.stdout
            if batch:
                if fresh:
                    assert 'V27_PREPARER_FRESH_REPLAY_OK' in result.stdout
                    assert 'V27_PREPARER_QUALIFICATION_RECOVERY_OK' in result.stdout
                    assert 'V27_PREPARER_SOURCE_LOCK_RELEASE_OK' in result.stdout
                    assert 'V27_PREPARER_QUALIFICATION_LATE_IO_RACE_OK' in result.stdout
                    assert 'V27_PREPARER_SOURCE_LATE_IO_RACE_OK' in result.stdout
                    assert 'V27_PREPARER_SOURCE_FINAL_IO_RACE_OK' in result.stdout
                    assert 'V27_PREPARER_CONFIG_PREFLIGHT_OK' in result.stdout
                    if partial:assert 'V27_PREPARER_PARTIAL_FAILURE_OK' in result.stdout
                    if revoke_registration:assert 'V27_PREPARER_REVOKED_REGISTRATION_OK' in result.stdout
                else:
                    assert 'V27_PROPOSAL_DRAFT_ROLLBACK_OK' in result.stdout
                    assert 'V27_PROPOSAL_STALE_RECIPE_REJECTED_OK' in result.stdout
                    assert 'V27_PROPOSAL_SOURCE_READ_OK' in result.stdout
                    assert 'V27_PROPOSAL_SOURCE_PIXELS_OK' in result.stdout
                    assert 'V27_PROPOSAL_QUALIFIED_COLOR_OK' in result.stdout
                    assert 'V27_PROPOSAL_MASK_READ_OK' in result.stdout
                    assert 'V27_PROPOSAL_MASK_PIXELS_OK' in result.stdout
                    assert 'V27_PROPOSAL_PROTECTION_READ_OK' in result.stdout
                    assert 'V27_PROPOSAL_COVERAGE_REQUIRED_OK' in result.stdout
                    assert 'V27_PROPOSAL_MANUAL_PERSON_DENIED_OK' in result.stdout
                    assert 'V27_PROPOSAL_PERSON_MASK_READ_OK' in result.stdout
                    assert 'V27_DEFECT_PERSON_PRODUCER_DENIED_OK' in result.stdout
                    assert 'V27_DEFECT_CANONICAL_ORIGIN_OK' in result.stdout
                    assert 'V27_DEFECT_ORIGINAL_EVIDENCE_OK' in result.stdout
                    assert 'V27_DEFECT_LANDMARK_PERSON_DENIED_OK' in result.stdout
                    assert 'V27_DEFECT_EMPTY_FACES_READ_OK' in result.stdout
                    assert 'V27_DEFECT_NONEMPTY_FACES_READ_OK' in result.stdout
                    assert 'V27_DEFECT_DETECTION_ORIGIN_AND_READ_OK' in result.stdout
                    assert 'V27_DEFECT_FINISHED_ATTEMPT_BACKEND_REQUIRED_OK' in result.stdout
                    assert 'V27_DEFECT_FACE_PROFILES_MATCH_REQUIRED_OK' in result.stdout
                    assert 'V27_DEFECT_TRUSTED_FACE_QUALIFICATION_READ_OK' in result.stdout
                    assert 'V27_DEFECT_QUALIFIED_MULTI_COLLECTION_OK' in result.stdout
                    assert 'V27_DEFECT_FACE_QUALIFICATION_POLICY_DISABLED_DENIED_OK' in result.stdout
                    assert 'V27_DEFECT_FACE_QUALIFICATION_READ_REVOCATION_DENIED_OK' in result.stdout
                    assert 'V27_DEFECT_FACE_PARAMS_EXPIRY_CORRUPTION_DENIED_OK' in result.stdout
                    assert 'V27_DEFECT_DETECTION_EXPIRY_CORRUPTION_DENIED_OK' in result.stdout
                    assert 'V27_DEFECT_LANDMARK_CORRUPTION_DENIED_OK' in result.stdout
                    assert 'V27_DEFECT_LANDMARK_UNDECLARED_DENIED_OK' in result.stdout
                    assert 'V27_DEFECT_LANDMARK_READ_RACE_DENIED_OK' in result.stdout
                    assert 'V27_DEFECT_PERMANENT_COVERAGE_OK' in result.stdout
                    assert 'V27_DEFECT_PERMANENT_COVERAGE_RACE_DENIED_OK' in result.stdout
                    assert 'V27_DEFECT_CORRUPT_PIXELS_DENIED_OK' in result.stdout
                    assert 'V27_DEFECT_OBJECT_VERSION_IMMUTABLE_OK' in result.stdout
                    assert 'V27_DEFECT_REPORT_READ_RACE_DENIED_OK' in result.stdout
                    assert 'V27_PROPOSAL_PERSON_REPORT_REQUIRED_OK' in result.stdout
                    assert 'V27_PROPOSAL_PERSON_EVIDENCE_OK' in result.stdout
                    assert 'V27_PROPOSAL_PERSON_COVERAGE_OK' in result.stdout
                    assert 'V27_PROPOSAL_PERSON_COVERAGE_REPORT_RACE_OK' in result.stdout
                    assert 'V27_PROPOSAL_PERSON_QUALIFICATION_SEPARATE_OK' in result.stdout
                    assert 'V27_PROPOSAL_PERSON_QUALIFICATION_OK' in result.stdout
                    assert 'V27_PROPOSAL_PERSON_QUALIFIED_COVERAGE_OK' in result.stdout
                    assert 'V27_PROPOSAL_PERSON_QUALIFICATION_RACE_OK' in result.stdout
                    assert 'V27_PROPOSAL_PERSON_PROFILE_LOCK_OK' in result.stdout
                    assert 'V27_PROPOSAL_PERSON_DISCOVERY_OK' in result.stdout
                    assert 'V27_PROPOSAL_PERSON_AMBIGUITY_OK' in result.stdout
                    assert 'V27_PROPOSAL_PERSON_SELECTION_COVERAGE_RACE_OK' in result.stdout
                    assert 'V27_TRANSFER_PERSON_PROTECTION_REQUIRED_OK' in result.stdout
                    assert 'V27_TRANSFER_PERSON_TARGET_PROTECTION_OK' in result.stdout
                    assert 'V27_TRANSFER_POLICY_OWNERSHIP_OK' in result.stdout
                    assert 'V27_TRANSFER_PERSON_PLAN_PROOF_OK' in result.stdout
                    assert 'V27_TRANSFER_PLAN_QUALIFICATION_OWNERSHIP_OK' in result.stdout
                    assert 'V27_TRANSFER_PLAN_REQUIRED_QUALIFICATION_OK' in result.stdout
                    assert 'V27_TRANSFER_PERSON_PRODUCER_LOCK_OK' in result.stdout
                    assert 'V27_TRANSFER_PERSON_EXACT_EVIDENCE_LOCK_OK' in result.stdout
                    assert 'V27_TRANSFER_PERSON_QUALIFICATION_LOCK_OK' in result.stdout
                    assert 'V27_TRANSFER_PERSON_QUALIFICATION_REVOCATION_OK' in result.stdout
                    assert 'V27_TRANSFER_PERSON_ADMISSION_DISCOVERY_OK' in result.stdout
                    assert 'V27_TRANSFER_PERSON_INVENTORY_BARRIER_OK' in result.stdout
                    assert 'V27_REGISTRATION_PHYSICAL_DEADLOCK_RETRY_OK' in result.stdout
                    assert 'V27_TRANSFER_PERSON_PRODUCER_CONCURRENT_LOCK_OK' in result.stdout
                    assert 'V27_PROPOSAL_MASK_SEPARATION_OK' in result.stdout
                    assert 'V27_PROPOSAL_QUALIFICATION_BYTES_OK' in result.stdout
                    assert 'V27_PROPOSAL_PREPARED_INPUTS_OK' in result.stdout
                    assert 'V27_PROPOSAL_METADATA_BUDGET_OK' in result.stdout
                    assert 'V27_TRANSFER_PERSON_METADATA_BOUNDARY_OK' in result.stdout
                    if race=='external':
                        assert 'V27_TRANSFER_MEASURED_COST_DENIAL_OK' in result.stdout
                    assert 'V27_PROPOSAL_SNAPSHOT_PUBLICATION_OK' in result.stdout
                snapshot_document=next(document for document in documents if 'snapshot_digest' in document)
                assert snapshot_document['snapshot_id']==intent['target_snapshot_id']
                assert snapshot_document['items'][0]['recipe']==dict(artifact_id=recipe_id,sha256=recipe_sha)
            params=next(document for document in documents if 'model_lock_sha' in document)
            assert params['reference']==base and params['target']==base
            assert params['model_lock_sha']=='c'*64 and params['calibration']==(fixture['Qualification']['calibration'] if batch else intent['reference_mask'])
            intent_artifact=db.execute("SELECT artifact_id FROM photo.v2_draft_uploads WHERE tenant_id=%s AND sha256=%s",(case['Tenant'],sha256(json.dumps(intent,sort_keys=True,separators=(',',':')).encode()).hexdigest())).fetchone()[0]
            assert params['intent']['artifact_id']==str(intent_artifact)
            assert params['intent']['sha256']==sha256(json.dumps(intent,sort_keys=True,separators=(',',':')).encode()).hexdigest()
        size,key,version,digest=next(row for row in rows if row[3]==sha256(json.dumps(intent,sort_keys=True,separators=(',',':')).encode()).hexdigest())
        response=client.get_object(bucket,key,version_id=version)
        try:payload=response.read()
        finally:response.close();response.release_conn()
        assert len(payload)==size and sha256(payload).hexdigest()==digest
        assert json.loads(payload)==intent
        if batch:
            if not fresh:
                assert 'V27_PROPOSAL_TRANSPORT_PUBLICATION_OK' in result.stdout
                assert 'V27_PROPOSAL_INPUT_REGISTRATION_ROLLBACK_OK' in result.stdout
                assert 'V27_PROPOSAL_PROTECTION_LOCKS_OK' in result.stdout
                assert 'V27_PROPOSAL_INPUT_REGISTRATION_COMMIT_OK' in result.stdout
                assert 'V27_PROPOSAL_INPUT_REGISTRATION_REPLAY_OK' in result.stdout
                assert 'V27_PROPOSAL_PREPARER_RECOVERY_OK' in result.stdout
            runs=db.execute("SELECT r.id,t.operation,r.status,r.graph_sealed,t.state FROM photo.v2_workflows w JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE w.tenant_id=%s AND w.workflow_type='semantic'",(case['Tenant'],)).fetchall()
            parent_runs=[row for row in runs if row[1]=='semantic.transfer']
            child_runs=[row for row in runs if row[1]=='defects.propose']
            assert len(parent_runs)==1 and parent_runs[0][2:]==(('queued',True,'ready') if commit_ordinary else ('draft',False,'blocked'))
            if commit_ordinary:assert 'V27_ORDINARY_TRANSFER_COMMITTED_OK' in result.stdout
            assert len(child_runs)==(1 if oriented else 0)
            assert len(runs)==len(parent_runs)+len(child_runs)
            if child_runs:
                assert child_runs[0][2:]==('draft',False,'blocked')
                assert 'V27_DEFECT_VERSION_PARAMS_PUBLICATION_OK' in result.stdout
                assert db.execute('SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s',(case['Tenant'],child_runs[0][0])).fetchone()==(0,)
            closures={}
            for run, *_ in runs:
                transports=db.execute("SELECT b.purpose,a.bytes,a.object_key,a.object_version,a.sha256,a.id FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id WHERE b.tenant_id=%s AND b.run_id=%s AND b.state='ready' ORDER BY b.purpose",(case['Tenant'],run)).fetchall()
                assert len(transports)==2 and [row[0] for row in transports]==['v2_input_chunk_0','v2_input_manifest']
                transport_bodies={}
                for purpose,size,key,version,digest,aid in transports:
                    response=client.get_object(bucket,key,version_id=version)
                    try:payload=response.read()
                    finally:response.close();response.release_conn()
                    assert len(payload)==size and sha256(payload).hexdigest()==digest
                    total_size+=size
                    transport_bodies[purpose]=json.loads(payload)
                manifest=transport_bodies['v2_input_manifest']
                chunk=transport_bodies['v2_input_chunk_0']
                assert manifest['entry_count']==len(chunk['entries'])>0
                assert manifest['chunks']==[dict(artifact_id=str(transports[0][5]),sha256=transports[0][4])]
                assert manifest['entries_sha']==sha256(json.dumps(chunk['entries'],sort_keys=True,separators=(',',':')).encode()).hexdigest()
                scoped_entries={(entry['ref']['artifact_id'],entry['ref']['sha256'],entry['contract']) for entry in chunk['entries']}
                registered=db.execute("SELECT i.artifact_id,i.sha256,i.contract FROM photo.v2_task_inputs i JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id WHERE t.tenant_id=%s AND t.run_id=%s",(case['Tenant'],run)).fetchall()
                assert {(str(aid),digest,contract) for aid,digest,contract in registered if contract not in ('InputClosureChunk','InputClosureManifest')}==scoped_entries
                assert {(str(aid),digest,contract) for aid,digest,contract in registered if contract in ('InputClosureChunk','InputClosureManifest')}=={(str(transports[0][5]),transports[0][4],'InputClosureChunk'),(str(transports[1][5]),transports[1][4],'InputClosureManifest')}
                closures[run]=scoped_entries
            entries=closures[parent_runs[0][0]]
            assert (base['source']['artifact_id'],base['source']['sha256'],'WorkingImage') in entries
            assert (rgb_id,rgb_sha,'NpyArray') in entries
            assert (alpha_id,alpha_sha,'NpyArray') in entries
            if oriented:
                assert (warp_id,warp_sha,'NpyArray') not in entries
            else:
                assert (warp_id,warp_sha,'NpyArray') in entries
            assert (fixture['Protection']['artifact_id'],fixture['Protection']['sha256'],'MaskRef') in entries

        after=db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['Tenant'],)).fetchone()
        expected_reservation=(268435456+(64<<20 if oriented else 0)) if batch else 0
        assert after==(before[0]+total_size,before[1]+expected_reservation)
        if batch:
            if not fresh:assert 'V27_PROPOSAL_DRAFT_COMMIT_REPLAY_OK' in result.stdout
            reservations=db.execute("SELECT holder_id,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND state='reserved' AND holder_id=ANY(%s) ORDER BY holder_id",(case['Tenant'],[row[0] for row in runs])).fetchall()
            assert dict(reservations)=={row[0]:268435456 if row[1]=='semantic.transfer' else 64<<20 for row in runs}



        if wrapper:
            db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(case['Session'],))
            fixture['Revoked']=True
            env['PHOTO_SEMANTIC_INTENT_PUBLISH_CASE']=json.dumps(fixture)
            denied=run_named_go(['go','test',package,'-run',probe,'-count=1','-timeout','210s','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=240)
            assert denied.returncode==0,denied.stdout+denied.stderr
            assert 'V27_PROPOSAL_REVOKED_OK' in denied.stdout
            assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['Tenant'],)).fetchone()==after
