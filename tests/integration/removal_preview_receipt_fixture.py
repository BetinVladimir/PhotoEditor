"""Own removal receipt/selection: real SQL, synthetic canonical result metadata.

Does not execute worker, physical verifier or photographer quality acceptance.
"""
import hashlib
import json
from uuid import uuid4
from psycopg.types.json import Jsonb
from removal_preview_activation_fixture import published_transport_fixture, commit_preview

def presentation_source_fixture(db, *, review_documents=True, review=None, _initial_root_builder=None, _complete=True, _initial_versions=None):
    if not isinstance(_complete,bool):
        raise ValueError('metadata completion must be boolean')
    versions = {} if _initial_versions is None else dict(_initial_versions)
    if set(versions) - {'preview_presentation','working_image','pixels','preview_evidence','before','after','affected','protected'} or any(v not in ('','null') for v in versions.values()):
        raise ValueError('closed metadata-only missing-version overrides')
    args,run,scope = published_transport_fixture(db,review_documents=review_documents,review=review)
    task = commit_preview(db,args,run,scope)
    db.execute('UPDATE photo.tasks SET attempt=1,fencing_token=1 WHERE id=%s',(task,))
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')",(args[0],task,uuid4()))
    # The source API authorizes metadata; JSON schema is checked by the reader.
    raw = (b'{"schema_version":1}' if _initial_root_builder is None
           else _initial_root_builder(args,scope,task))
    assert isinstance(raw,bytes) and raw
    sha = hashlib.sha256(raw).hexdigest()
    root = uuid4()
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,%s,%s,'application/json',%s,clock_timestamp(),'preview_presentation',%s,1,1)",(root,args[0],str(root),sha,len(raw),versions.get('preview_presentation','metadata-only-v1'),task))
    result = db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(args[0],task,Jsonb([dict(artifact_id=str(root),sha256=sha)]),sha)).fetchone()[0]
    if _complete:
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
        db.execute("UPDATE photo.runs SET status='completed' WHERE id=%s",(run,))
    return args,run,scope,task,result,root,sha

def authorized_sidecars_fixture(db, *, review_documents=True, review=None, _initial_documents=None, _initial_versions=None):
    from test_v27_removal_preview_bundle import ROLES
    versions = {} if _initial_versions is None else dict(_initial_versions)
    captured = {}
    def initial_root(args,scope,task):
        presentation = dict(schema_version=1,params=json.loads(args[5]),width=16,height=16,
            resize='area_linear_premult_v1',overlay='max_coverage_ceil_alpha_v1',icc_lock_sha='b'*64,srgb_sha='c'*64)
        refs = {role:dict(artifact_id=str(uuid4()),sha256=hashlib.sha256(role.encode()).hexdigest())
                for role,_ in ROLES}
        # Compute document dependencies before any immutable artifact/result INSERT.
        documents = {} if _initial_documents is None else _initial_documents(args,scope,refs)
        assert set(documents) <= {'working_image','preview_evidence'}
        for role in ('working_image','preview_evidence'):
            if role in documents:
                raw = documents[role].encode()
                refs[role]['sha256'] = hashlib.sha256(raw).hexdigest()
        for role,media in ROLES:
            ref = refs[role]
            size = len(documents[role].encode()) if role in documents else 128
            db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,%s,%s,%s,%s,clock_timestamp(),%s,%s,1,1)",
                (ref['artifact_id'],args[0],ref['artifact_id'],ref['sha256'],size,media,versions.get(role,'metadata-only-v1'),role,task))
            presentation['numerical_evidence' if role == 'preview_evidence' else role] = ref
        raw = json.dumps(presentation,sort_keys=True,separators=(',', ':'))
        captured.update(refs=refs,raw=raw)
        return raw.encode()
    args,run,scope,*_ = presentation_source_fixture(db,review_documents=review_documents,review=review,_initial_root_builder=initial_root,_initial_versions=versions)
    return args,run,scope,captured['refs'],captured['raw']


def receipt_fixture(db, *, review_documents=True, review=None, _initial_versions=None):
    documents = {}
    def initial_documents(args,scope,refs):
        # Metadata-only SQL fixture, not a physical WorkingImage verifier.
        image_body = json.loads(scope['source_body']) if review_documents else {}
        image_body['pixels'] = refs['pixels']
        image = json.dumps(image_body,sort_keys=True,separators=(',', ':'))
        refs['working_image']['sha256'] = hashlib.sha256(image.encode()).hexdigest()
        evidence = json.dumps(dict(schema_version=1,params=json.loads(args[5]),
            working_image=refs['working_image'],pixels=refs['pixels'],
            algorithm='big_lama_work_srgb_delta_v1',protected_pixels_unchanged=True,
            outside_mask_unchanged=True,changed_pixels=0,global_pass=True,roi=[0,0,16,16]),
            sort_keys=True,separators=(',', ':'))
        documents.update(working_image=image,preview_evidence=evidence)
        return documents
    args,run,scope,refs,presentation = authorized_sidecars_fixture(db,review_documents=review_documents,review=review,_initial_documents=initial_documents,_initial_versions=_initial_versions)
    body = json.loads(presentation)
    digest = hashlib.sha256(presentation.encode()).hexdigest()
    task,result,root = db.execute("SELECT t.id,t.canonical_result_id,(r.outputs->0->>'artifact_id')::uuid FROM photo.tasks t JOIN photo.task_results r ON r.id=t.canonical_result_id WHERE t.run_id=%s",(run,)).fetchone()
    return args,run,scope,body,presentation,documents['preview_evidence'],documents['working_image'],task,result,root,digest


def seal(db,fixture):
    args,run,_,_,presentation,evidence,image,*_ = fixture
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        value = db.execute('SELECT photo.seal_semantic_removal_preview_receipt(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (args[0],run,args[2],args[3],args[5],args[6],presentation,evidence,image)).fetchone()[0]
        db.execute('RESET ROLE')
        return value

def lock_selection(db,data,receipt, *, overrides=None):
    args = data[0]
    params = json.loads(args[5])
    render,presentation = receipt['working_image'],receipt['presentation']
    values = (args[0],args[1],args[2],args[3],int(params['workflow_revision']),args[4],int(params['target_revision']),
        params['plan']['sha256'],params['candidate']['sha256'],render['artifact_id'],render['sha256'],presentation['artifact_id'],presentation['sha256'])
    if overrides:
        fields = ('tenant', 'intent', 'actor', 'session', 'revision', 'target', 'target_revision',
                  'plan_sha', 'candidate_sha', 'render', 'render_sha', 'evidence', 'evidence_sha')
        assert set(overrides) <= set(fields)
        values = tuple(overrides.get(field, value) for field, value in zip(fields, values))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_semantic_admission')
        rows = db.execute('SELECT * FROM photo.lock_semantic_removal_preview_selection(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',values).fetchall()
        db.execute('RESET ROLE')
        return rows

