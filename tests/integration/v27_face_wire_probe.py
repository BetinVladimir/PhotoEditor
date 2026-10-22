"""Real native face adapters/leases/mTLS; explicit deterministic model doubles.
Nonempty face/landmarks/labels, not scientific GPU qualification.
"""
from contextlib import contextmanager
from hashlib import sha256
from io import BytesIO
import json
import os
from pathlib import Path
from types import SimpleNamespace
from uuid import UUID
import numpy as np
from PIL import Image
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.face_skin import FaceSkinParams
from photo_worker.operations.mask import face_skin
import psycopg
from minio import Minio
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.face import FaceBoxes,FaceLandmarks,FaceDetectParams,LandmarkParams
from photo_worker.contracts.face_parse import FaceLabels19,FaceParseParams
from photo_worker.contracts.wire import TaskCommand
from photo_worker.runtime.database import Lease, TaskDatabase
from photo_worker.runtime.executor import ExecutionContext
from photo_worker.runtime.gateway import client_tls
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.operations.face import detect,landmarks,parse


class FixtureEngine:
    def __init__(self, model): self.model = model
    def infer(self, value):
        assert value.dtype == np.float32 and np.isfinite(value).all()
        if self.model == 'sfd':
            h,w=value.shape[:2]
            return np.array([[w*.3,h*.2,w*.7,h*.8,.99]],np.float32)
        if self.model == 'fan2d':
            # Deterministic anatomical fixture, large enough for real feature protection.
            points=[(128+90*np.cos(t),105+105*np.sin(t)) for t in np.linspace(np.pi,0,17)]
            points.extend([(70,85),(80,80),(90,78),(100,80),(110,85),(146,85),(156,80),(166,78),(176,80),(186,85)])
            points.extend([(128,95),(128,110),(128,125),(128,140),(110,145),(119,150),(128,152),(137,150),(146,145)])
            for cx in (90,166):
                points.extend([(cx+18*np.cos(t),104+9*np.sin(t)) for t in np.linspace(0,2*np.pi,6,endpoint=False)])
            points.extend([(128+32*np.cos(t),179+12*np.sin(t)) for t in np.linspace(0,2*np.pi,20,endpoint=False)])
            return np.array([(x,y,1.) for x,y in points],np.float32)
        assert self.model == 'bisenet_face19'
        logits=np.full((19,512,512),-20,np.float32);logits[1]=20
        return logits

class FixturePool:
    lock=SimpleNamespace(lock_id='c'*64)
    @contextmanager
    def acquire(self,model_id,backend):
        assert model_id in ('sfd','fan2d','bisenet_face19') and backend=='cuda'
        yield FixtureEngine(model_id)


def main(runtime_dsn):
    dsn = os.environ['PHOTO_TEST_DATABASE_URL']
    task = UUID(os.environ['PHOTO_TEST_V27_GATEWAY_TASK'])
    with psycopg.connect(dsn) as db:
        row = db.execute('SELECT worker_id,attempt,fencing_token,lease_until,deadline,execution_profile,input_digest,dispatch_command FROM photo.tasks WHERE id=%s', (task,)).fetchone()
    command = TaskCommand.model_validate_json(json.dumps(row[7]))
    assert command.task_id == task and command.operation in ('face.detect','face.landmarks','face.parse','mask.face_skin') and command.attempt == row[1]
    lease = Lease(command, row[0], row[2], row[3], row[4], row[5], row[6]); ctx = ExecutionContext(lease)
    directory = Path(os.environ['PHOTO_TEST_V27_CERT_DIR'])
    tls = client_tls(directory/'ca.pem', directory/'client.pem', directory/'client.key')
    scratch = directory/'scratch'; scratch.mkdir()
    config = ArtifactConfig(scratch, os.environ['PHOTO_TEST_V27_GATEWAY_ORIGIN'], tls, 1024**3, 8*1024**2, 1024**3)
    spec={
        'face.detect':(detect,FaceDetectParams,('face_boxes',)),
        'face.landmarks':(landmarks,LandmarkParams,('face_landmarks',)),
        'face.parse':(parse,FaceParseParams,('face_labels','face_labels_ref')),
        'mask.face_skin':(face_skin,FaceSkinParams,('skin_mask','skin_mask_png','skin_mask_ref')),
    }
    adapter,contract,roles=spec[command.operation]
    if command.operation != "mask.face_skin":
        assert lease.execution_profile['model_lock_id']==lease.execution_profile['model_lock_sha']=='c'*64
        assert lease.execution_profile['runtime_lock_sha']=='b'*64 and lease.execution_profile['device_profile_sha']=='c'*64
    with TaskArtifacts.create(ctx, config, frozenset(roles)) as artifacts:
        params=artifacts.params(contract)
        result = adapter.execute(ctx, artifacts) if command.operation=='mask.face_skin' else adapter.execute(ctx, artifacts, FixturePool(), 'cuda')
        assert len(result.outputs) == len(roles)
    assert not artifacts.scratch.directory.exists()
    objects = Minio(os.environ['PHOTO_TEST_MINIO_URL'].removeprefix('http://'), access_key=os.environ['PHOTO_TEST_MINIO_ACCESS'], secret_key=os.environ['PHOTO_TEST_MINIO_SECRET'], secure=False)
    bodies = []
    with psycopg.connect(dsn) as db:
        for ref, role in zip(result.outputs, roles):
            stored = db.execute('SELECT bucket,object_key,object_version,sha256,output_role,verified_at,bytes FROM photo.artifacts WHERE tenant_id=%s AND id=%s', (command.tenant_id, ref.artifact_id)).fetchone()
            assert stored[4] == role and stored[5] is not None and stored[2] not in ('', 'null', None)
            response = objects.get_object(stored[0], stored[1], version_id=stored[2])
            try: body = response.read()
            finally: response.close(); response.release_conn()
            assert len(body) == stored[6] and sha256(body).hexdigest() == stored[3] == ref.sha256
            bodies.append(body)
    if command.operation=='face.detect':
        manifest=FaceBoxes.model_validate_json(bodies[0])
        assert manifest.accepted_count==len(manifest.faces)==1 and manifest.candidate_count==1
    elif command.operation=='face.landmarks':
        manifest=FaceLandmarks.model_validate_json(bodies[0])
        assert len(manifest.landmarks)==1 and len(manifest.landmarks[0].points)==68
        assert manifest.mean_confidence==1 and manifest.faces==params.faces
    elif command.operation=='face.parse':
        pixels=np.load(BytesIO(bodies[0]),allow_pickle=False)
        assert pixels.dtype==np.uint16 and pixels.shape==(300,400) and np.any(pixels==1)
        assert set(np.unique(pixels))=={0,1}
        manifest=FaceLabels19.model_validate_json(bodies[1])
        assert manifest.labels==result.outputs[0] and manifest.parsed_faces==1 and manifest.faces==params.faces
    else:
        pixels=np.load(BytesIO(bodies[0]),allow_pickle=False)
        assert pixels.dtype==np.float32 and pixels.shape==(300,400) and np.any(pixels==1)
        assert set(np.unique(pixels))=={0,1}
        with Image.open(BytesIO(bodies[1])) as png:
            assert np.array_equal(np.asarray(png),np.rint(pixels*65535).astype(np.uint16))
        manifest=MaskRef.model_validate_json(bodies[2])
        assert manifest.artifact==result.outputs[0] and manifest.selector=='skin' and not manifest.protected
    assert manifest.model_lock_id=='c'*64
    if command.operation!='mask.face_skin':
        assert manifest.image==params.image and manifest.backend=='cuda' 
    # Exact target detector output provenance before canonical commit.
    with psycopg.connect(dsn) as db:
        target=db.execute("SELECT r.config_snapshot ? 'v27_target_detector_prepare', r.config_snapshot ? 'v27_target_face_prepare', r.config_snapshot ? 'v27_target_skin_prepare' FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.id=%s",(task,)).fetchone()
        if any(target):
            from psycopg.types.json import Jsonb
            refs=[ref.model_dump(mode='json') for ref in result.outputs]
            def ready(outputs,attempt=command.attempt,fence=lease.fence):
                function='semantic_target_skin_result_ready' if target[2] else 'semantic_target_face_result_ready' if target[1] else 'semantic_target_detector_result_ready'
                return db.execute('SELECT photo.'+function+'(%s,%s,%s,%s,%s)',(command.tenant_id,task,attempt,fence,Jsonb(outputs))).fetchone()[0]
            assert ready(refs)
            assert not ready(refs,attempt=command.attempt+1) and not ready(refs,fence=lease.fence+1)
            for altered in [[],refs+refs,[dict(refs[0],sha256='0'*64)], [dict(refs[0],artifact_id=str(params.labels.artifact_id if command.operation=='mask.face_skin' else params.image.artifact_id))]]:
                assert not ready(altered)
            if len(refs)>1: assert not ready(list(reversed(refs)))
            assert db.execute('SELECT count(*) FROM photo.task_results WHERE task_id=%s',(task,)).fetchone()==(0,)
    database = TaskDatabase(runtime_dsn)
    payload = canonical_bytes(dict(outputs=[ref.model_dump(mode='json') for ref in result.outputs], metrics=dict(duration_ms=0, peak_memory_bytes=0, bytes_written=result.bytes_written), review_required=result.review_required))
    committed = database.commit_success(lease, payload)
    assert committed[0] == 'committed', committed
    assert database.commit_success(lease, payload) == ('replayed', *committed[1:])
    with psycopg.connect(dsn) as db:
        assert db.execute('SELECT state,canonical_result_id FROM photo.tasks WHERE id=%s', (task,)).fetchone() == ('succeeded', committed[1])
        assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s', (task,)).fetchone() == (0,)
        assert db.execute("SELECT count(*) FROM photo.resource_reservations WHERE task_id=%s AND state='reserved'", (task,)).fetchone() == (0,)
    print('V27_FACE_ADAPTER_NEW_OUTPUTS_COMMITTED_OK:'+command.operation)
    print('V27_WORKER_WIRE_OK')


def verify_new_face_chain(db, objects, tenant, source, parent_created=False):
    """Independent outer verifier after all nested Go/Python processes exit."""
    rows=db.execute("""SELECT r.id,r.status,r.config_snapshot,t.id,t.operation,t.state,
        t.canonical_result_id,t.attempt,t.fencing_token,t.execution_profile,tr.outputs
        FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
        JOIN photo.task_results tr ON tr.id=t.canonical_result_id AND tr.tenant_id=t.tenant_id
        WHERE r.tenant_id=%s AND r.config_snapshot ?| array['v27_detector_prepare','v27_face_prepare','v27_skin_prepare']
        ORDER BY t.operation""",(tenant,)).fetchall()
    assert len(rows)==4 and {r[4] for r in rows}=={'face.detect','face.landmarks','face.parse','mask.face_skin'}
    parents=set();detector=None;face_params=[];parsed_pixels=None;parsed_outputs=None;skin_pixels=None;skin_stage=None
    roles={'face.detect':('face_boxes',),'face.landmarks':('face_landmarks',),'face.parse':('face_labels','face_labels_ref'),'mask.face_skin':('skin_mask','skin_mask_png','skin_mask_ref')}
    for run,status,config,task,operation,state,result,attempt,fence,profile,outputs in rows:
        stage=config['v27_detector_prepare' if operation=='face.detect' else 'v27_skin_prepare' if operation=='mask.face_skin' else 'v27_face_prepare']
        parents.add(stage['parent_run_id'])
        assert stage['source']['source']==source and stage['task_id']==str(task)
        assert state=='succeeded' and status in ('running','completed') and attempt==1 and fence>0
        if operation=='mask.face_skin':
            assert stage['recovery']['skin_profile_version']==1
            assert profile['input_index_scratch_bytes']==16<<20 and profile['backend']=='cpu'
            assert profile['resource_estimate']['vram_bytes']==0
            assert all(k not in profile for k in ('model_lock_id','model_lock_sha','runtime_lock_sha','device_profile_sha'))
        else:
            assert stage['recovery']['face_profile_version']==1
            assert profile['model_lock_id']==profile['model_lock_sha']=='c'*64
            assert profile['runtime_lock_sha']=='b'*64 and profile['device_profile_sha']=='c'*64
        assert profile['resource_estimate']['scratch_bytes']>=16<<20
        assert db.execute('SELECT count(*) FROM photo.task_results WHERE tenant_id=%s AND task_id=%s',(tenant,task)).fetchone()[0]==1
        assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(task,)).fetchone()[0]==0
        assert db.execute("SELECT count(*) FROM photo.resource_reservations WHERE task_id=%s AND state='reserved'",(task,)).fetchone()[0]==0
        attempt_profile=db.execute("SELECT profile FROM photo.task_attempts WHERE tenant_id=%s AND task_id=%s AND attempt=%s AND fencing_token=%s AND finished_at IS NOT NULL",(tenant,task,attempt,fence)).fetchone()[0]
        assert attempt_profile==profile
        if operation!='mask.face_skin':
            assert db.execute("SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s AND artifact_id=%s AND sha256=%s AND contract='WorkingImage'",(tenant,task,source['artifact_id'],source['sha256'])).fetchone()[0]==1
        else:
            assert db.execute("SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s AND contract='FaceLabels19'",(tenant,task)).fetchone()[0]==1
        bodies=[]
        assert len(outputs)==len(roles[operation])
        for ref,role in zip(outputs,roles[operation]):
            bucket,key,version,digest,size,stored_role,producer,a,f,verified=db.execute('SELECT bucket,object_key,object_version,sha256,bytes,output_role,created_by_task_id,produced_attempt,produced_fence,verified_at FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(tenant,ref['artifact_id'])).fetchone()
            assert producer==task and a==attempt and f==fence and stored_role==role and verified is not None
            assert version not in ('','null',None)
            response=objects.get_object(bucket,key,version_id=version)
            try:body=response.read()
            finally:response.close();response.release_conn()
            assert len(body)==size and sha256(body).hexdigest()==digest==ref['sha256']
            bodies.append(body)
        if operation=='face.detect':
            assert len(FaceBoxes.model_validate_json(bodies[0]).faces)==1
            detector=outputs[0]
        elif operation=='face.landmarks':
            manifest=FaceLandmarks.model_validate_json(bodies[0]);assert len(manifest.landmarks)==1
            face_params.append(manifest.faces.model_dump(mode='json'))
        elif operation=='face.parse':
            manifest=FaceLabels19.model_validate_json(bodies[1]);assert manifest.parsed_faces==1
            parsed_pixels=np.load(BytesIO(bodies[0]),allow_pickle=False);parsed_outputs=outputs
            assert np.any(parsed_pixels==1)
            face_params.append(manifest.faces.model_dump(mode='json'))
        else:
            skin_pixels=np.load(BytesIO(bodies[0]),allow_pickle=False);skin_stage=stage
            assert skin_pixels.dtype==np.float32 and np.any(skin_pixels==1)
            manifest=MaskRef.model_validate_json(bodies[2]);assert manifest.artifact.model_dump(mode='json')==outputs[0]
            assert manifest.selector=='skin' and not manifest.protected
            with Image.open(BytesIO(bodies[1])) as png:
                assert np.array_equal(np.asarray(png),np.rint(skin_pixels*65535).astype(np.uint16))
    assert np.array_equal(skin_pixels,parsed_pixels==1)
    assert skin_stage['parser_outputs']==parsed_outputs
    assert len(parents)==1 and all(ref==detector for ref in face_params)
    assert db.execute('SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND id=%s',(tenant,next(iter(parents)))).fetchone()[0]==int(parent_created)


def verify_new_defect_chain(db, objects, tenant, source, parent_created=False):
    """Independently verify canonical CPU defects and registered physical protection."""
    from photo_worker.contracts.defects import DefectCandidates, DefectProposeReport
    rows = db.execute("""SELECT r.id,r.status,r.config_snapshot,t.id,t.state,t.canonical_result_id,
        t.attempt,t.fencing_token,t.execution_profile,tr.outputs
        FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
        JOIN photo.task_results tr ON tr.tenant_id=t.tenant_id AND tr.id=t.canonical_result_id
        WHERE r.tenant_id=%s AND r.config_snapshot ? 'v27_defect_native_prepare'""", (tenant,)).fetchall()
    assert len(rows)==1
    run,status,config,task,state,result,attempt,fence,profile,outputs=rows[0]
    stage=config['v27_defect_native_prepare']
    assert stage['task_id']==str(task) and stage['source']['source']==source
    assert status in ('running','completed') and state=='succeeded' and attempt==1 and fence>0
    assert profile['backend']=='cpu' and profile['resource_estimate']['vram_bytes']==0
    assert len(outputs)==5
    assert db.execute('SELECT count(*) FROM photo.task_results WHERE tenant_id=%s AND task_id=%s',(tenant,task)).fetchone()[0]==1
    assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(task,)).fetchone()[0]==0
    assert db.execute("SELECT count(*) FROM photo.resource_reservations WHERE task_id=%s AND state='reserved'",(task,)).fetchone()[0]==0
    assert db.execute('SELECT profile FROM photo.task_attempts WHERE tenant_id=%s AND task_id=%s AND attempt=%s AND fencing_token=%s AND finished_at IS NOT NULL',(tenant,task,attempt,fence)).fetchone()[0]==profile
    bodies=[]
    for ref,role in zip(outputs,('defect_labels','defect_protection','defect_labels_png','defect_candidates','defect_propose_report')):
        bucket,key,version,digest,size,stored_role,producer,a,f,verified=db.execute('SELECT bucket,object_key,object_version,sha256,bytes,output_role,created_by_task_id,produced_attempt,produced_fence,verified_at FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(tenant,ref['artifact_id'])).fetchone()
        assert version not in ('','null',None) and stored_role==role and producer==task and a==attempt and f==fence and verified is not None
        response=objects.get_object(bucket,key,version_id=version)
        try: body=response.read()
        finally: response.close();response.release_conn()
        assert len(body)==size and sha256(body).hexdigest()==digest==ref['sha256']
        bodies.append(body)
    labels=np.load(BytesIO(bodies[0]),allow_pickle=False); protected=np.load(BytesIO(bodies[1]),allow_pickle=False)
    assert labels.shape==protected.shape==(300,400) and np.any(protected>0)
    assert not np.any(labels[protected>0])
    landmark=stage['landmarks_outputs'][0]
    bucket,key,version=db.execute('SELECT bucket,object_key,object_version FROM photo.artifacts WHERE tenant_id=%s AND id=%s AND sha256=%s',(tenant,landmark['artifact_id'],landmark['sha256'])).fetchone()
    response=objects.get_object(bucket,key,version_id=version)
    try: raw=response.read()
    finally: response.close();response.release_conn()
    assert sha256(raw).hexdigest()==landmark['sha256']
    faces=FaceLandmarks.model_validate_json(raw).landmarks
    assert len(faces)==1
    # Centroids of the two eyes, nose and mouth must remain fully protected.
    for start,end in ((36,42),(42,48),(27,36),(48,68)):
        points=faces[0].points[start:end]
        x=round(sum(p.x*400-.5 for p in points)/len(points))
        y=round(sum(p.y*300-.5 for p in points)/len(points))
        assert protected[y,x]==1 and labels[y,x]==0
    with Image.open(BytesIO(bodies[2])) as png: assert np.array_equal(np.asarray(png),labels)
    candidates=DefectCandidates.model_validate_json(bodies[3]);report=DefectProposeReport.model_validate_json(bodies[4])
    assert candidates.label_map.model_dump(mode='json')==outputs[0]
    assert report.protection.artifact.model_dump(mode='json')==outputs[1] and report.protection.protected
    roots=db.execute('SELECT root_id,root_sha,pixels_id,pixels_sha,report_id,report_sha,parent_run_id,source_id,source_sha FROM photo.semantic_native_protections WHERE tenant_id=%s AND task_id=%s AND result_id=%s',(tenant,task,result)).fetchall()
    assert len(roots)==1
    root,digest,pixels,pixelsha,reportid,reportsha,parent,sourceid,sourcesha=roots[0]
    assert str(pixels)==outputs[1]['artifact_id'] and pixelsha==outputs[1]['sha256']
    assert str(reportid)==outputs[4]['artifact_id'] and reportsha==outputs[4]['sha256']
    assert str(parent)==stage['parent_run_id'] and str(sourceid)==source['artifact_id'] and sourcesha==source['sha256']
    bucket,key,version,size=db.execute("SELECT bucket,object_key,object_version,bytes FROM photo.artifacts WHERE tenant_id=%s AND id=%s AND sha256=%s AND state='ready' AND verified_at IS NOT NULL",(tenant,root,digest)).fetchone()
    response=objects.get_object(bucket,key,version_id=version)
    try: body=response.read()
    finally: response.close();response.release_conn()
    assert len(body)==size and sha256(body).hexdigest()==digest
    assert MaskRef.model_validate_json(body)==report.protection
    assert db.execute('SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND id=%s',(tenant,parent)).fetchone()[0]==int(parent_created)


def verify_saved_person_defect(db, objects, tenant, source, completed=False, parent_created=False):
    """Independent storage/DB check of original person-bearing blocked recovery.

    No worker result, coverage or parent-admission acceptance is asserted here.
    """
    rows=db.execute("""SELECT r.status,r.graph_sealed,r.config_snapshot,t.id,t.state,t.canonical_result_id
        FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
        WHERE r.tenant_id=%s AND r.config_snapshot ? 'v27_defect_native_prepare'""",(tenant,)).fetchall()
    assert len(rows)==1
    status,sealed,config,task,state,result=rows[0]
    stage=config['v27_defect_native_prepare'];person=stage['person_input']
    if completed:
        assert status in ('running','completed') and sealed and state=='succeeded' and result is not None
    else:
        assert status=='draft' and not sealed and state=='blocked' and result is None
    assert stage['task_id']==str(task) and stage['source']['source']==source
    assert person['schema_version']==1 and len(person['authority_sha'])==64
    assert len(person['files'])==2 and person['files'][0]['role']=='person_root' and person['files'][1]['role']=='person_pixels'
    assert person['files'][0]['ref']==person['root'] and person['files'][1]['ref']==person['pixels']

    def read_original(ref, pin=None):
        row=db.execute("""SELECT bucket,object_key,object_version,sha256,bytes,media_type
            FROM photo.artifacts WHERE tenant_id=%s AND id=%s AND sha256=%s
            AND state='ready' AND verified_at IS NOT NULL""",(tenant,ref['artifact_id'],ref['sha256'])).fetchone()
        assert row is not None
        bucket,key,version,digest,size,media=row
        assert version not in ('',None,'null')
        if pin is not None:
            assert (bucket,key,version,size,media)==(pin['bucket'],pin['key'],pin['version'],pin['bytes'],pin['media'])
        response=objects.get_object(bucket,key,version_id=version)
        try: body=response.read()
        finally: response.close();response.release_conn()
        assert len(body)==size and sha256(body).hexdigest()==digest==ref['sha256']
        return body

    original=json.loads(read_original(person['root'],person['files'][0]))
    pixels=np.load(BytesIO(read_original(person['pixels'],person['files'][1])),allow_pickle=False)
    assert pixels.shape==(300,400) and pixels.dtype==np.dtype('<f4')
    assert np.all(np.isfinite(pixels)) and np.all((pixels>=0)&(pixels<=1)) and np.any(pixels>0)
    assert original['artifact']==person['pixels'] and original['selector']=='person' and original['protected'] is False
    assert original['object_id'] is None and original['model_lock_id']
    read_original(person['qualification_report'])
    params=json.loads(read_original(stage['params']))
    expected=dict(original,protected=True)
    assert params['permanent']==expected and params['protections']==[]
    assert db.execute("""SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s
        AND artifact_id=%s AND sha256=%s AND contract='NpyArray'""",(tenant,task,person['pixels']['artifact_id'],person['pixels']['sha256'])).fetchone()[0]==1
    for table in ('task_attempts','task_results'):
        assert db.execute(f'SELECT count(*) FROM photo.{table} WHERE tenant_id=%s AND task_id=%s',(tenant,task)).fetchone()[0]==int(completed)
    if completed:
        outputs=db.execute('SELECT outputs FROM photo.task_results WHERE tenant_id=%s AND id=%s',(tenant,result)).fetchone()[0]
        protected=np.load(BytesIO(read_original(outputs[1])),allow_pickle=False)
        labels=np.load(BytesIO(read_original(outputs[0])),allow_pickle=False)
        assert protected.shape==pixels.shape and np.all(protected>=pixels)
        if os.environ.get('PHOTO_TEST_V27_ORIGINAL_PERSON') == '1':
            assert np.all(pixels[:10,:10]==0) and pixels[250,200]>.99
        else:
            assert 0<pixels[41,17]<.5 and protected[41,17]==pixels[41,17]
        assert not np.any(labels[protected>0])
    assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(task,)).fetchone()[0]==0
    assert db.execute('SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND id=%s',(tenant,stage['parent_run_id'])).fetchone()[0]==int(parent_created)


from v27_target_wire_verification import (
    verify_target_person_draft, verify_target_detector_draft,
    verify_target_face_blocked_drafts, verify_target_skin_executed_draft,
    verify_target_defect_prepared_params, verify_target_defect_transport,
    verify_native_parent_admission,
)

if __name__ == '__main__':
    from wire_runtime_login import run_worker_probe
    run_worker_probe(main, os.environ['PHOTO_TEST_DATABASE_URL'])
