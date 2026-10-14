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


def main():
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
    database = TaskDatabase(dsn)
    payload = canonical_bytes(dict(outputs=[ref.model_dump(mode='json') for ref in result.outputs], metrics=dict(duration_ms=0, peak_memory_bytes=0, bytes_written=result.bytes_written), review_required=result.review_required))
    committed = database.commit_success(lease, payload)
    assert committed[0] == 'committed'
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


if __name__ == '__main__':
    main()


def verify_native_parent_admission(db, objects, tenant, required_target):
    """Independent queued-parent graph, immutable closure and exactly-once receipt."""
    rows=db.execute("SELECT id,status,graph_sealed,config_snapshot FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v27_semantic'",(tenant,)).fetchall()
    assert len(rows)==1
    parent,status,sealed,config=rows[0]
    assert status=='queued' and sealed
    nodes=config['v27_semantic']['nodes']
    assert len(nodes)==db.execute('SELECT count(*) FROM photo.snapshot_items WHERE tenant_id=%s AND snapshot_id=%s',(tenant,config['input_snapshot_id'])).fetchone()[0]>0
    source_roots=db.execute('SELECT root_id,root_sha FROM photo.semantic_native_protections WHERE tenant_id=%s AND parent_run_id=%s',(tenant,parent)).fetchall()
    assert len(source_roots)==1
    source_root=dict(artifact_id=str(source_roots[0][0]),sha256=source_roots[0][1])
    # Row locking is available solely to the function owner; actual mutation is
    # denied even to that role, and no application role obtains UPDATE.
    for role in ('photo_api','photo_scheduler','photo_worker','photo_publisher','photo_portal','photo_auth','photo_gateway','photo_maintenance'):
        assert not db.execute("SELECT has_column_privilege(%s,'photo.semantic_native_protections','document_id','UPDATE')",(role,)).fetchone()[0]
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        assert db.execute('SELECT document_id FROM photo.semantic_native_protections WHERE tenant_id=%s AND root_id=%s FOR SHARE',(tenant,source_root['artifact_id'])).fetchone()
    import pytest
    from psycopg.errors import InsufficientPrivilege
    with pytest.raises(InsufficientPrivilege,match='SEMANTIC_NATIVE_PROTECTION_IMMUTABLE'):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_fn_owner')
            db.execute('UPDATE photo.semantic_native_protections SET document_id=document_id WHERE tenant_id=%s AND root_id=%s',(tenant,source_root['artifact_id']))

    tasks=db.execute('SELECT id,operation,state,canonical_result_id FROM photo.tasks WHERE tenant_id=%s AND run_id=%s',(tenant,parent)).fetchall()
    assert len(tasks)==len(nodes) and {str(t[0]) for t in tasks}=={n['task_id'] for n in nodes}
    for node in nodes:
        assert node['source_protection']==source_root and node['target_protection']==required_target
    for task,operation,state,result in tasks:
        assert (operation,state,result)==('semantic.transfer','ready',None)
        pins=db.execute('SELECT i.contract,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256 WHERE i.tenant_id=%s AND i.task_id=%s',(tenant,task)).fetchall()
        documents={}
        for contract,aid,digest,bucket,key,version,size in pins:
            if contract not in ('InputClosureChunk','InputClosureManifest'):
                continue
            assert version not in (None,'','null')
            response=objects.get_object(bucket,key,version_id=version)
            try: raw=response.read()
            finally:
                response.close();response.release_conn()
            assert len(raw)==size and sha256(raw).hexdigest()==digest
            documents[(str(aid),digest)]=(contract,json.loads(raw))
        manifests=[value for contract,value in documents.values() if contract=='InputClosureManifest']
        assert len(manifests)==1
        manifest=manifests[0]
        entries=[]
        for ref in manifest['chunks']:
            contract,chunk=documents[(ref['artifact_id'],ref['sha256'])]
            assert contract=='InputClosureChunk'
            entries.extend(chunk['entries'])
        assert len(entries)==manifest['entry_count']
        assert sha256(json.dumps(entries,sort_keys=True,separators=(',',':')).encode()).hexdigest()==manifest['entries_sha']
        registered={(str(aid),digest,contract) for contract,aid,digest,*_ in pins if contract not in ('InputClosureChunk','InputClosureManifest')}
        assert registered=={(e['ref']['artifact_id'],e['ref']['sha256'],e['contract']) for e in entries}
        for root in (source_root,required_target):
            assert (root['artifact_id'],root['sha256'],'MaskRef') in registered
    assert db.execute("SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=%s AND resource_id=%s AND route='V2.Admit' AND response_status=202",(tenant,parent)).fetchone()==(1,)
    assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.run.queued'",(tenant,parent)).fetchone()==(1,)
    assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND payload->>'run_id'=%s AND routing_key='v2.run.queued'",(tenant,str(parent))).fetchone()==(1,)
    assert db.execute("SELECT bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s AND state='reserved'",(tenant,parent)).fetchall()==[(128<<20,)]
    if os.environ.get('PHOTO_TEST_V27_ORIGINAL_PERSON') == '1':
        verify_original_person_handoff(db,tenant)


def verify_original_person_handoff(db, tenant):
    """Original producer identity and durable lease state, independent of Go logs."""
    from uuid import UUID, uuid5
    rows=db.execute("""SELECT r.id,r.config_snapshot,t.id,t.state,t.canonical_result_id,
        t.attempt,t.fencing_token,z.outputs FROM photo.runs r
        JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
        JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id
          AND z.id=t.canonical_result_id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
        WHERE r.tenant_id=%s AND r.config_snapshot ? 'v27_person_prepare'""",(tenant,)).fetchall()
    assert len(rows)==1
    run,config,task,state,result,attempt,fence,outputs=rows[0]
    stage=config['v27_person_prepare'];parent=UUID(stage['parent_run_id'])
    assert run==uuid5(parent,'semantic.mask.person:reference') and state=='succeeded'
    assert stage['task_id']==str(task) and len(outputs)==4 and attempt==1 and fence>0
    saved=db.execute("SELECT config_snapshot->'v27_defect_native_prepare'->'person_input' FROM photo.runs WHERE tenant_id=%s AND id=%s",(tenant,uuid5(parent,'semantic.defect.prepare:reference'))).fetchone()[0]
    assert saved['root']==outputs[2] and saved['pixels']==outputs[0]
    for ref,role in zip(outputs,('person_mask','person_mask_png','person_mask_ref','person_report')):
        assert db.execute("""SELECT created_by_task_id,produced_attempt,produced_fence,output_role,
          state,verified_at IS NOT NULL,object_version NOT IN('','null') FROM photo.artifacts
          WHERE tenant_id=%s AND id=%s AND sha256=%s""",(tenant,ref['artifact_id'],ref['sha256'])).fetchone()==(task,attempt,fence,role,'ready',True,True)
    assert db.execute("""SELECT v27_initial_recovery_done,v27_initial_recovery_token,
      v27_initial_recovery_until,v27_initial_recovery_after FROM photo.runs
      WHERE tenant_id=%s AND id=%s""",(tenant,uuid5(parent,'semantic.face.detect:reference'))).fetchone()==(True,None,None,None)
    assert db.execute('SELECT count(*) FROM photo.task_results WHERE tenant_id=%s AND task_id=%s',(tenant,task)).fetchone()==(1,)
    assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(task,)).fetchone()==(0,)


def verify_target_person_draft(db,objects,tenant):
    """Independent target draft/lineage/Params/quota proof; no executable claim."""
    from uuid import UUID,uuid5
    from hashlib import sha256
    rows=db.execute("""SELECT r.id,r.config_snapshot,r.status,r.graph_sealed,t.id,t.state,t.canonical_result_id,t.params_artifact_id,t.execution_profile
      FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
      WHERE r.tenant_id=%s AND r.config_snapshot ? 'v27_target_person_prepare'""",(tenant,)).fetchall()
    assert len(rows)==1
    run,config,status,sealed,task,state,canonical,params,profile=rows[0]
    stage=config['v27_target_person_prepare'];namespace=UUID(stage['target_namespace']);parent=UUID(stage['parent_run_id'])
    assert run==uuid5(namespace,'semantic.mask.person:reference')
    import os
    advance_mode=os.environ.get('PHOTO_TEST_V27_TARGET_PERSON_ADVANCE')=='1'
    seal_mode=os.environ.get('PHOTO_TEST_V27_TARGET_PERSON_SEAL')=='1' or advance_mode
    execute_mode=os.environ.get('PHOTO_TEST_V27_TARGET_PERSON_EXECUTE')=='1' or advance_mode
    finish_mode=os.environ.get('PHOTO_TEST_V27_TARGET_PERSON_FINISH')=='1' or advance_mode
    if finish_mode: assert status=='completed' and sealed and state=='succeeded' and canonical is not None
    elif execute_mode: assert status=='running' and sealed and state=='succeeded' and canonical is not None
    elif seal_mode: assert status=='queued' and sealed and state=='ready' and canonical is None
    else: assert status=='draft' and not sealed and state=='blocked' and canonical is None
    assert stage['task_id']==str(task) and stage['target_stage_id']==str(uuid5(namespace,'stage:mask.person'))
    saved,manifest_sha=db.execute('SELECT v27_target_manifest,v27_target_manifest_sha FROM photo.runs WHERE tenant_id=%s AND id=%s',(tenant,uuid5(parent,'semantic.face.detect:reference'))).fetchone()
    manifest=json.loads(saved);entry=manifest['entries'][stage['target_ordinal']]
    assert sha256(saved.encode()).hexdigest()==manifest_sha==stage['manifest_sha']
    assert entry['namespace']==str(namespace) and entry['binding']==stage['source'] and not entry['reuse_reference']
    assert manifest['parent_run_id']==str(parent) and manifest['request_sha']==stage['request_sha']
    assert manifest['intent']==stage['intent'] and manifest['snapshot']==stage['snapshot']
    bucket,key,version,digest,size=db.execute('SELECT bucket,object_key,object_version,sha256,bytes FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(tenant,params)).fetchone()
    response=objects.get_object(bucket,key,version_id=version)
    try: payload=response.read()
    finally: response.close();response.release_conn()
    assert len(payload)==size and sha256(payload).hexdigest()==digest==stage['params']['sha256']
    assert json.loads(payload)['image']==entry['binding']['source']
    assert profile['model_lock_sha'] and profile['runtime_lock_sha'] and profile['device_profile_sha']
    if finish_mode: assert db.execute("SELECT state,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(tenant,run)).fetchone()==('released',0)
    else: assert db.execute("SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s AND state='reserved'",(tenant,run)).fetchone()[0]==1
    receipt=db.execute("SELECT response_status,response_body FROM photo.idempotency_records WHERE tenant_id=%s AND resource_id=%s AND route='V2.Admit'",(tenant,run)).fetchone()
    if seal_mode: assert receipt==(202,dict(run_id=str(run),workflow_id=str(uuid5(run,'workflow')),state='queued'))
    else: assert receipt==(None,None)
    # Transport registration is complete, exact, replay-safe and still blocked.
    pins=db.execute('SELECT i.ordinal,i.contract,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256 WHERE i.tenant_id=%s AND i.task_id=%s ORDER BY i.ordinal',(tenant,task)).fetchall()
    docs={}
    for ordinal,contract,aid,digest,bucket,key,version,size in pins:
        if ordinal<1000000:continue
        response=objects.get_object(bucket,key,version_id=version)
        try: payload=response.read()
        finally: response.close();response.release_conn()
        assert len(payload)==size and sha256(payload).hexdigest()==digest
        docs[(str(aid),digest)]=(contract,json.loads(payload))
        assert db.execute("SELECT count(*) FROM photo.backend_documents WHERE tenant_id=%s AND run_id=%s AND owner_id=%s AND artifact_id=%s AND sha256=%s AND state='ready'",(tenant,run,task,aid,digest)).fetchone()[0]==1
    transport=profile['v2_input_manifest']
    contract,transport_manifest=docs[(transport['artifact_id'],transport['sha256'])]
    assert contract=='InputClosureManifest' and transport_manifest['task_id']==str(task)
    entries=[]
    for ref in transport_manifest['chunks']:
        contract,chunk=docs[(ref['artifact_id'],ref['sha256'])]
        assert contract=='InputClosureChunk'
        entries.extend(chunk['entries'])
    assert len(entries)==transport_manifest['entry_count']==profile['v2_entry_count']
    assert sha256(canonical_bytes(entries)).hexdigest()==transport_manifest['entries_sha']==profile['v2_entries_sha']
    assert {(str(aid),digest,contract) for ordinal,contract,aid,digest,*_ in pins if ordinal<1000000}=={(e['ref']['artifact_id'],e['ref']['sha256'],e['contract']) for e in entries}
    assert any(e['ref']==stage['source']['source'] and e['contract']=='WorkingImage' for e in entries)
    assert any(e['ref']==stage['params'] and e['contract']=='Params' for e in entries)
    assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.run.queued'",(tenant,run)).fetchone()[0]==(1 if seal_mode else 0)

    assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND payload->>'run_id'=%s AND routing_key='v2.run.queued'",(tenant,str(run))).fetchone()[0]==(1 if seal_mode else 0)

    assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.run.completed'",(tenant,run)).fetchone()[0]==(1 if finish_mode else 0)
    assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND payload->>'run_id'=%s AND routing_key='v2.run.completed'",(tenant,str(run))).fetchone()[0]==(1 if finish_mode else 0)


def verify_target_detector_draft(db, objects, tenant):
    """Independent physical target detector blocked-draft proof; no execution."""
    from uuid import UUID, uuid5
    from hashlib import sha256
    rows = db.execute("""SELECT r.id,r.config_snapshot,r.status,r.graph_sealed,t.id,t.state,
      t.canonical_result_id,t.params_artifact_id,t.execution_profile
      FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
      WHERE r.tenant_id=%s AND r.config_snapshot ? 'v27_target_detector_prepare'""", (tenant,)).fetchall()
    assert len(rows) == 1
    run, config, status, sealed, task, state, result, params, profile = rows[0]
    stage = config['v27_target_detector_prepare']
    namespace, parent = UUID(stage['target_namespace']), UUID(stage['parent_run_id'])
    assert run == uuid5(namespace, 'semantic.face.detect:reference')
    assert task == uuid5(uuid5(run, 'workflow'), 'face.detect:0')
    assert stage['task_id'] == str(task)
    assert stage['target_stage_id'] == str(uuid5(namespace, 'stage:face.detect'))
    seal_mode=os.environ.get('PHOTO_TEST_V27_TARGET_DETECTOR_SEAL')=='1'
    execute_mode=os.environ.get('PHOTO_TEST_V27_TARGET_DETECTOR_EXECUTE')=='1'
    finish_mode=os.environ.get('PHOTO_TEST_V27_TARGET_DETECTOR_FINISH')=='1'
    if finish_mode: assert status=='completed' and sealed and state=='succeeded' and result is not None
    elif execute_mode: assert status=='running' and sealed and state=='succeeded' and result is not None
    elif seal_mode: assert status=='queued' and sealed and state=='ready' and result is None
    else: assert status == 'completed' and sealed and state == 'succeeded' and result is not None
    saved, manifest_sha = db.execute('SELECT v27_target_manifest,v27_target_manifest_sha FROM photo.runs WHERE tenant_id=%s AND id=%s', (tenant, uuid5(parent, 'semantic.face.detect:reference'))).fetchone()
    manifest = json.loads(saved)
    entry = manifest['entries'][stage['target_ordinal']]
    assert sha256(saved.encode()).hexdigest() == manifest_sha == stage['manifest_sha']
    assert entry['namespace'] == str(namespace) and entry['binding'] == stage['source'] and not entry['reuse_reference']
    assert manifest['parent_run_id'] == str(parent) and manifest['request_sha'] == stage['request_sha']
    assert manifest['intent'] == stage['intent'] and manifest['snapshot'] == stage['snapshot']
    bucket, key, version, digest, size = db.execute('SELECT bucket,object_key,object_version,sha256,bytes FROM photo.artifacts WHERE tenant_id=%s AND id=%s', (tenant, params)).fetchone()
    response = objects.get_object(bucket, key, version_id=version)
    try:
        payload = response.read()
    finally:
        response.close()
        response.release_conn()
    assert len(payload) == size and sha256(payload).hexdigest() == digest == stage['params']['sha256']
    assert json.loads(payload)['image'] == entry['binding']['source']
    assert all(profile[k] for k in ('model_lock_sha', 'runtime_lock_sha', 'device_profile_sha'))
    if finish_mode:
        assert db.execute("SELECT state,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(tenant,run)).fetchone()==('released',0)
        assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.run.completed'",(tenant,run)).fetchone()[0]==1
        assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND payload->>'run_id'=%s AND routing_key='v2.run.completed'",(tenant,str(run))).fetchone()[0]==1
    else:
        assert db.execute("SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s AND state='reserved'", (tenant, run)).fetchone()[0] == 1
    receipt=db.execute("SELECT response_status,response_body FROM photo.idempotency_records WHERE tenant_id=%s AND resource_id=%s AND route='V2.Admit'", (tenant, run)).fetchone()
    if seal_mode:
        assert receipt==(202,dict(run_id=str(run),workflow_id=str(uuid5(run,'workflow')),state='queued'))
        assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.run.queued'",(tenant,run)).fetchone()[0]==1
        assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND payload->>'run_id'=%s AND routing_key='v2.run.queued'",(tenant,str(run))).fetchone()[0]==1
    else: assert receipt==(None,None)
    if os.environ.get('PHOTO_TEST_V27_TARGET_DETECTOR_REGISTER') == '1':
        pins = db.execute('SELECT i.ordinal,i.contract,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256 WHERE i.tenant_id=%s AND i.task_id=%s ORDER BY i.ordinal', (tenant,task)).fetchall()
        assert len([p for p in pins if p[0]<1000000]) == profile['v2_entry_count']
        for ordinal, contract, aid, digest, bucket, key, version, size in pins:
            if ordinal<1000000: continue
            response=objects.get_object(bucket,key,version_id=version)
            try: payload=response.read()
            finally: response.close();response.release_conn()
            assert len(payload)==size and sha256(payload).hexdigest()==digest
            purpose='v2_input_manifest' if contract=='InputClosureManifest' else 'v2_input_chunk_'+str(ordinal-1000000)
            assert db.execute('SELECT count(*) FROM photo.backend_documents WHERE tenant_id=%s AND run_id=%s AND owner_id=%s AND artifact_id=%s AND sha256=%s AND purpose=%s AND state=\'ready\'',(tenant,run,task,aid,digest,purpose)).fetchone()[0]==1
        assert db.execute('SELECT photo.v2_input_closure_ready(%s)',(task,)).fetchone()==(True,)
    else:
        assert db.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s', (tenant, task)).fetchone()[0] >= 6
        assert db.execute('SELECT photo.v2_input_closure_ready(%s)', (task,)).fetchone()[0] is True
    assert db.execute('SELECT count(*) FROM photo.task_results WHERE tenant_id=%s AND task_id=%s', (tenant, task)).fetchone()[0] == (1 if execute_mode else 0)


def verify_target_face_blocked_drafts(db, objects, tenant):
    """Independent persisted state and physical Params checks for both targets."""
    from uuid import UUID, uuid5
    from hashlib import sha256
    rows = db.execute("""SELECT r.id,r.config_snapshot,r.status,r.graph_sealed,
      t.id,t.operation,t.state,t.canonical_result_id,t.params_artifact_id
      FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
      WHERE r.tenant_id=%s AND r.config_snapshot ? 'v27_target_face_prepare'""", (tenant,)).fetchall()
    assert len(rows) == 2
    assert {row[5] for row in rows} == {'face.landmarks', 'face.parse'}
    for run, config, status, sealed, task, operation, state, result, params in rows:
        stage = config['v27_target_face_prepare']
        namespace = UUID(stage['target_namespace'])
        if os.environ.get('PHOTO_TEST_V27_TARGET_FACE_EXECUTE')=='1':
            expected_status='completed' if os.environ.get('PHOTO_TEST_V27_TARGET_FACE_FINISH')=='1' else 'running'
            assert status==expected_status and sealed and state=='succeeded' and result is not None
            assert db.execute('SELECT count(*) FROM photo.task_results WHERE tenant_id=%s AND task_id=%s',(tenant,task)).fetchone()[0]==1
        else:
            assert status == 'completed' and sealed and state == 'succeeded' and result is not None
        assert run == uuid5(namespace, 'semantic.' + operation + ':reference')
        assert task == uuid5(uuid5(run, 'workflow'), operation + ':0')
        assert stage['task_id'] == str(task) and stage['operation'] == operation
        assert stage['target_stage_id'] == str(uuid5(namespace, 'stage:' + operation))
        detector = db.execute("SELECT t.canonical_result_id FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.tenant_id=%s AND t.id=%s AND r.status='completed' AND t.state='succeeded'", (tenant, UUID(stage['detector_task_id']))).fetchone()
        assert detector and str(detector[0]) == stage['detector_result_id']
        assert db.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s', (tenant, task)).fetchone()[0] >= 6
        assert db.execute('SELECT photo.v2_input_closure_ready(%s)', (task,)).fetchone()[0] is True
        bucket, key, version, digest, size = db.execute('SELECT bucket,object_key,object_version,sha256,bytes FROM photo.artifacts WHERE tenant_id=%s AND id=%s', (tenant, params)).fetchone()
        response = objects.get_object(bucket, key, version_id=version)
        try:
            payload = response.read()
        finally:
            response.close()
            response.release_conn()
        assert len(payload) == size and sha256(payload).hexdigest() == digest == stage['params']['sha256']
        document = json.loads(payload)
        assert document['image'] == stage['source']['source']


def verify_target_skin_executed_draft(db, objects, tenant):
    """Independent executed persistence and namespace Params byte verification."""
    from uuid import UUID, uuid5
    from hashlib import sha256
    rows = db.execute("""SELECT r.id,r.status,r.graph_sealed,r.config_snapshot,
      t.id,t.operation,t.state,t.canonical_result_id,t.params_artifact_id
      FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
      WHERE r.tenant_id=%s AND r.config_snapshot ? 'v27_target_skin_prepare'""", (tenant,)).fetchall()
    assert len(rows) == 1
    run, status, sealed, config, task, operation, state, result, params = rows[0]
    stage = config['v27_target_skin_prepare']
    namespace = UUID(stage['target_namespace'])
    assert status == 'completed' and sealed and state == 'succeeded' and result is not None
    assert operation == 'mask.face_skin'
    assert db.execute('SELECT photo.v2_input_closure_ready(%s)', (task,)).fetchone()[0] is True
    assert db.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s AND ordinal<1000000', (tenant,task)).fetchone()[0] == 3
    assert run == uuid5(namespace, 'semantic.mask.face_skin:reference')
    assert task == uuid5(uuid5(run, 'workflow'), 'mask.face_skin:0')
    assert stage['target_stage_id'] == str(uuid5(namespace, 'stage:mask.face_skin'))
    assert stage['params_document'] == str(uuid5(namespace, 'semantic.mask.face_skin.params:reference'))
    assert stage['task_id'] == str(task)
    parser = db.execute("""SELECT t.canonical_result_id FROM photo.tasks t
      JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
      WHERE t.tenant_id=%s AND t.id=%s AND t.state='succeeded' AND r.status='completed'""", (tenant, UUID(stage['parser_task_id']))).fetchone()
    assert parser and str(parser[0]) == stage['parser_result_id']
    bucket, key, version, digest, size = db.execute('SELECT bucket,object_key,object_version,sha256,bytes FROM photo.artifacts WHERE tenant_id=%s AND id=%s', (tenant, params)).fetchone()
    response = objects.get_object(bucket, key, version_id=version)
    try:
        payload = response.read()
    finally:
        response.close()
        response.release_conn()
    assert len(payload) == size and sha256(payload).hexdigest() == digest == stage['params']['sha256']
    document = json.loads(payload)
    manifest = stage['parser_outputs'][1]
    assert document == {'labels': manifest, 'algorithm': 'bisenet_face19_skin_v1'}
    bucket, key, version, digest, size = db.execute('SELECT bucket,object_key,object_version,sha256,bytes FROM photo.artifacts WHERE tenant_id=%s AND id=%s', (tenant, UUID(manifest['artifact_id']))).fetchone()
    response = objects.get_object(bucket, key, version_id=version)
    try:
        payload = response.read()
    finally:
        response.close()
        response.release_conn()
    assert len(payload) == size and sha256(payload).hexdigest() == digest == manifest['sha256']
    assert json.loads(payload)['image'] == stage['source']['source']


def verify_target_defect_prepared_params(db, objects, tenant):
    """Independent namespace ownership and unmodified target producer refs."""
    from uuid import uuid5
    stage = db.execute("SELECT config_snapshot->'v27_target_skin_prepare' FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v27_target_skin_prepare'", (tenant,)).fetchone()[0]
    namespace = UUID(stage['target_namespace'])
    document_id = uuid5(namespace, 'semantic.defect.params:reference')
    rows = db.execute("""SELECT d.asset_version_id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes
      FROM photo.v2_draft_uploads d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
      WHERE d.tenant_id=%s AND d.document_id=%s AND d.state='ready' AND a.state='ready'""", (tenant, document_id)).fetchall()
    assert len(rows) == 1
    version, bucket, key, obj_version, digest, size = rows[0]
    assert str(version) == stage['source']['asset_version_id']
    response = objects.get_object(bucket, key, version_id=obj_version)
    try:
        payload = response.read()
    finally:
        response.close()
        response.release_conn()
    assert len(payload) == size and sha256(payload).hexdigest() == digest
    params = json.loads(payload)
    source = stage['source']['source']
    assert params['image'] == source
    def outputs(operation):
        run = uuid5(namespace, 'semantic.' + operation + ':reference')
        row = db.execute("""SELECT tr.outputs FROM photo.tasks t
          JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
          JOIN photo.task_results tr ON tr.tenant_id=t.tenant_id AND tr.id=t.canonical_result_id
          WHERE t.tenant_id=%s AND t.run_id=%s AND t.operation=%s AND t.state='succeeded' AND r.status='completed'""", (tenant, run, operation)).fetchone()
        assert row
        return row[0]
    person = outputs('mask.person')[0]
    landmarks = outputs('face.landmarks')[0]
    skin = outputs('mask.face_skin')[0]
    assert params['permanent']['artifact'] == person
    assert params['permanent']['protected'] is True and params['permanent']['selector'] == 'person'
    assert params['protections'] == []
    assert params['diameter_face_fraction'] == [.002, .012]
    assert len(params['faces']) > 0
    for face in params['faces']:
        assert face['landmarks'] == landmarks
        assert face['skin']['artifact'] == skin
        assert face['skin']['protected'] is False
    # Preparation/publication must not activate an unqualified defect stage.
    defect_run = uuid5(namespace, 'semantic.defect.prepare:reference')
    if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_BEGIN')=='1' or os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_ADVANCE')=='1':
        row = db.execute("SELECT r.status,r.graph_sealed,t.state,t.canonical_result_id FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=%s AND r.id=%s", (tenant,defect_run)).fetchone()
        if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_EXECUTE')=='1' or os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_ADVANCE')=='1':
            assert row[:3] == (('completed' if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_FINISH')=='1' or os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_ADVANCE')=='1' else 'running'),True,'succeeded') and row[3] is not None
        else:
            assert row == (('queued',True,'ready',None) if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_SEAL')=='1' else ('draft',False,'blocked',None))
    else:
        assert db.execute('SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND id=%s', (tenant,defect_run)).fetchone()[0] == 0


def verify_target_defect_transport(db, objects, tenant):
    """Physical backend-authored closure, still unregistered and unqueued."""
    from uuid import uuid5
    row = db.execute("SELECT id,config_snapshot FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v27_target_defect_prepare'", (tenant,)).fetchone()
    assert row
    run, config = row
    stage = config['v27_target_defect_prepare']
    namespace = UUID(stage['target_namespace'])
    assert run == uuid5(namespace, 'semantic.defect.prepare:reference')
    task = uuid5(uuid5(run, 'workflow'), 'defects.propose:0')
    assert stage['task_id'] == str(task)
    rows = db.execute("""SELECT d.purpose,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes
      FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
      WHERE d.tenant_id=%s AND d.run_id=%s AND d.owner_id=%s AND d.state='ready' AND a.state='ready'""", (tenant,run,task)).fetchall()
    docs = {}
    for purpose, aid, digest, bucket, key, version, size in rows:
        assert purpose == 'v2_input_manifest' or purpose.startswith('v2_input_chunk_')
        response = objects.get_object(bucket,key,version_id=version)
        try:
            payload = response.read()
        finally:
            response.close()
            response.release_conn()
        assert len(payload) == size and sha256(payload).hexdigest() == digest
        assert purpose not in docs
        docs[purpose] = (str(aid),digest,json.loads(payload))
    manifest = docs['v2_input_manifest'][2]
    assert manifest['schema_version'] == 1 and manifest['task_id'] == str(task)
    entries = []
    for i, ref in enumerate(manifest['chunks']):
        aid,digest,chunk = docs['v2_input_chunk_'+str(i)]
        assert ref == {'artifact_id':aid,'sha256':digest} and chunk['schema_version'] == 1
        entries.extend(chunk['entries'])
    assert len(docs) == len(manifest['chunks']) + 1
    assert entries == stage['input_entries'] and len(entries) == manifest['entry_count']
    if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_ADVANCE')=='1':
        completed = db.execute("SELECT r.status,q.state,q.bytes,(SELECT count(*) FROM photo.run_events WHERE tenant_id=r.tenant_id AND run_id=r.id AND type='v2.run.completed'),(SELECT count(*) FROM photo.outbox WHERE tenant_id=r.tenant_id AND payload->>'run_id'=r.id::text AND routing_key='v2.run.completed') FROM photo.runs r JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_id=r.id AND q.holder_type='run' WHERE r.tenant_id=%s AND r.id=%s", (tenant,run)).fetchone()
        assert completed == ('completed','released',0,1,1)
        from uuid import uuid5
        protection_doc = uuid5(UUID(stage['target_namespace']), 'semantic.defect.protection:reference')
        registered_root = db.execute("SELECT s.root_id,s.root_sha,s.pixels_id,s.pixels_sha,s.anchor,a.bucket,a.object_key,a.object_version,a.bytes FROM photo.semantic_native_protections s JOIN photo.artifacts a ON a.tenant_id=s.tenant_id AND a.id=s.root_id WHERE s.tenant_id=%s AND s.document_id=%s", (tenant, protection_doc)).fetchall()
        assert len(registered_root) == 1
        root_id,root_sha,pixels_id,pixels_sha,anchor,bucket,key,version,size = registered_root[0]
        assert anchor == dict(schema_version=1,kind='target_defect',parent_run_id=stage['parent_run_id'],request_sha=stage['request_sha'],target_namespace=stage['target_namespace'],target_ordinal=stage['target_ordinal'],target_manifest_sha=stage['manifest_sha'])
        response = objects.get_object(bucket,key,version_id=version)
        try:
            body = response.read()
        finally:
            response.close()
            response.release_conn()
        assert len(body) == size and sha256(body).hexdigest() == root_sha
        root = json.loads(body)
        assert root['artifact'] == dict(artifact_id=str(pixels_id),sha256=pixels_sha) and root['protected'] is True
        report_ref = db.execute("SELECT tr.outputs->4 FROM photo.tasks t JOIN photo.task_results tr ON tr.tenant_id=t.tenant_id AND tr.id=t.canonical_result_id WHERE t.tenant_id=%s AND t.id=%s", (tenant,task)).fetchone()[0]
        report_bucket,report_key,report_version = db.execute("SELECT bucket,object_key,object_version FROM photo.artifacts WHERE tenant_id=%s AND id=%s AND sha256=%s", (tenant,report_ref['artifact_id'],report_ref['sha256'])).fetchone()
        response = objects.get_object(report_bucket,report_key,version_id=report_version)
        try:
            report = response.read()
        finally:
            response.close()
            response.release_conn()
        assert sha256(report).hexdigest() == report_ref['sha256'] and root == json.loads(report)['protection']

    registered = os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_REGISTER')=='1' or os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_ADVANCE')=='1'
    expected_count = len(entries) + len(manifest['chunks']) + 1 if registered else 0
    assert db.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s', (tenant,task)).fetchone()[0] == expected_count
    if registered:
        assert db.execute('SELECT photo.v2_input_closure_ready(%s)', (task,)).fetchone()[0] is True
    assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND payload->>'run_id'=%s AND routing_key='v2.run.queued'", (tenant,str(run))).fetchone()[0] == (1 if os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_SEAL')=='1' or os.environ.get('PHOTO_TEST_V27_TARGET_DEFECT_ADVANCE')=='1' else 0)
