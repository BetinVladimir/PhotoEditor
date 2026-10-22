"""Own exposure receipt/selection: real SQL, synthetic canonical result metadata.

Does not execute worker, physical verifier or photographer quality acceptance.
"""
import hashlib
import json
from uuid import uuid4
from psycopg.types.json import Jsonb
from exposure_preview_activation_fixture import published_transport_fixture, commit_preview

ROLES = (('working_image','application/json'),('pixels','application/x-npy'),('preview_evidence','application/json'),('before','image/png'),('after','image/png'),('affected','image/png'),('protected','image/png'))

def presentation_source_fixture(db, *, review_documents=True, review=None):
    args,run,scope = published_transport_fixture(db,review_documents=review_documents,review=review)
    task = commit_preview(db,args,run,scope)
    db.execute('UPDATE photo.tasks SET attempt=1,fencing_token=1 WHERE id=%s',(task,))
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')",(args[0],task,uuid4()))
    # The source API authorizes metadata; JSON schema is checked by the reader.
    raw = b'{"schema_version":1}'
    sha = hashlib.sha256(raw).hexdigest()
    root = uuid4()
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,%s,%s,'application/json','metadata-only-v1',clock_timestamp(),'preview_presentation',%s,1,1)",(root,args[0],str(root),sha,len(raw),task))
    result = db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(args[0],task,Jsonb([dict(artifact_id=str(root),sha256=sha)]),sha)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
    db.execute("UPDATE photo.runs SET status='completed' WHERE id=%s",(run,))
    return args,run,scope,task,result,root,sha

def authorized_sidecars_fixture(db, *, review_documents=True, review=None):
    args,run,scope,task,result,root,_ = presentation_source_fixture(db,review_documents=review_documents,review=review)
    presentation = dict(schema_version=1,params=json.loads(args[5]),width=16,height=16,
        resize='area_linear_premult_v1',overlay='max_coverage_ceil_alpha_v1',icc_lock_sha='b'*64,srgb_sha='c'*64)
    refs = {}
    for role,media in ROLES:
        aid = uuid4()
        # Metadata-only stage: actual body validation belongs to physical reader.
        digest = hashlib.sha256(role.encode()).hexdigest()
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,%s,128,%s,'metadata-only-v1',clock_timestamp(),%s,%s,1,1)",
            (aid,args[0],str(aid),digest,media,role,task))
        refs[role] = dict(artifact_id=str(aid),sha256=digest)
        presentation['numerical_evidence' if role == 'preview_evidence' else role] = refs[role]
    raw = json.dumps(presentation,sort_keys=True,separators=(',', ':'))
    digest = hashlib.sha256(raw.encode()).hexdigest()
    db.execute('UPDATE photo.artifacts SET sha256=%s,bytes=%s WHERE id=%s',(digest,len(raw.encode()),root))
    db.execute('UPDATE photo.task_results SET outputs=%s WHERE id=%s',(Jsonb([dict(artifact_id=str(root),sha256=digest)]),result))
    return args,run,scope,refs,raw

def receipt_fixture(db, *, review_documents=True, review=None):
    args,run,scope,refs,presentation = authorized_sidecars_fixture(db,review_documents=review_documents,review=review)
    def bind(role,body):
        raw = json.dumps(body,sort_keys=True,separators=(',', ':'))
        digest = hashlib.sha256(raw.encode()).hexdigest()
        db.execute('UPDATE photo.artifacts SET sha256=%s,bytes=%s WHERE id=%s',(digest,len(raw.encode()),refs[role]['artifact_id']))
        refs[role]['sha256'] = digest
        return raw
    # SQL receipt checks the pixels projection. Full WorkingImage/color/NPY
    # validation is mandatory in the backend before invoking this API.
    image_body = json.loads(scope['source_body']) if review_documents else {}
    image_body['pixels'] = refs['pixels']
    image = bind('working_image',image_body)
    evidence = bind('preview_evidence',dict(schema_version=1,params=json.loads(args[5]),
        working_image=refs['working_image'],pixels=refs['pixels'],
        algorithm='local_exposure_exact_replay_v1',protected_pixels_unchanged=True,
        outside_mask_max_error=0))
    body = json.loads(presentation)
    body.update(working_image=refs['working_image'],numerical_evidence=refs['preview_evidence'])
    presentation = json.dumps(body,sort_keys=True,separators=(',', ':'))
    digest = hashlib.sha256(presentation.encode()).hexdigest()
    task,result,root = db.execute('SELECT t.id,t.canonical_result_id,(r.outputs->0->>\'artifact_id\')::uuid FROM photo.tasks t JOIN photo.task_results r ON r.id=t.canonical_result_id WHERE t.run_id=%s',(run,)).fetchone()
    db.execute('UPDATE photo.artifacts SET sha256=%s,bytes=%s WHERE id=%s',(digest,len(presentation.encode()),root))
    db.execute('UPDATE photo.task_results SET outputs=%s WHERE id=%s',(Jsonb([dict(artifact_id=str(root),sha256=digest)]),result))
    return args,run,scope,body,presentation,evidence,image,task,result,root,digest

def seal(db,fixture):
    args,run,_,_,presentation,evidence,image,*_ = fixture
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        value = db.execute('SELECT photo.seal_semantic_preapply_preview_receipt(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (args[0],run,args[2],args[3],args[5],args[6],presentation,evidence,image)).fetchone()[0]
        db.execute('RESET ROLE')
        return value
