"""Owner-seeded relational pair fixture, not adoption/quality qualification.

Uses the production pair-ready function without stubbing it. The private
publication and complete decision authority are deliberately outside its scope.
"""
from hashlib import sha256
from uuid import uuid4
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.media.derivative import media_profile_hash
from test_v28_relight_lease import relight_lease_fixture
from test_v28_relight_adoption_source import complete_relight_task


def relight_viewed_pair_fixture(db):
    def params(_pin, scope):
        return canonical_bytes({'base': {'asset_version_id': str(scope['version']),
            'source': {'artifact_id': str(scope['source']), 'sha256': scope['source_sha']}}})
    args, scope, _ = relight_lease_fixture(db, params_builder=params)
    tenant, origin_run, origin = args[:3]
    result, _ = complete_relight_task(db, args, review_required=True)
    candidate, review, presentation = uuid4(), uuid4(), uuid4()
    candidate_sha, srgb = 'c' * 64, 'b' * 64
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,'v1',%s,2,'application/json','working_image',clock_timestamp(),%s,1,1)",
               (candidate, tenant, str(candidate), candidate_sha, origin))
    db.execute("UPDATE photo.runs SET status='waiting_review' WHERE id=%s", (origin_run,))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        db.execute("INSERT INTO photo.reviews(id,tenant_id,run_id,task_id,candidate_artifact_id,base_artifact_id,candidate_hash,kind,reasons,relight_params_jcs) VALUES(%s,%s,%s,%s,%s,%s,%s,'image.relight','[]',%s)",
                   (review, tenant, origin_run, origin, candidate, scope['source'], candidate_sha, args[8]))
        db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed,v26_derivative_task_id) VALUES(%s,%s,'preview','draft',%s,%s,%s,%s,false,%s)",
                   (presentation, tenant, scope['snapshot'], Jsonb({'srgb_sha256': srgb, 'source_result_id': str(result)}), 'd' * 64, scope['actor'], origin))
    profile = media_profile_hash('preview1600', srgb)
    expiry = db.execute("SELECT floor(extract(epoch FROM clock_timestamp()))::bigint+120").fetchone()[0]
    grant = {'version': 1, 'kind': 'image.relight', 'revision': 1, 'expires_unix': expiry}
    images, manifests, tasks = {}, {}, {}
    for side, source, source_sha in [('before', scope['source'], scope['source_sha']), ('after', candidate, candidate_sha)]:
        task, param, image, manifest = [uuid4() for _ in range(4)]
        tasks[side], images[side], manifests[side] = task, image, manifest
        body = '{}'; digest = sha256(body.encode()).hexdigest()
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,%s,'fixture',%s,'v1',%s,2,'application/json',clock_timestamp())", (param, tenant, str(param), digest))
        db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,deadline) VALUES(%s,%s,%s,%s,'media.preview','1','media','queued',%s,%s,%s,'{}',1,1,clock_timestamp()+interval '1 hour')",
                   (task, tenant, presentation, 'render.derivative:preview1600' if side == 'after' else 'fixture.before', Jsonb([{'artifact_id': str(source), 'sha256': source_sha}]), param, 'd' * 64))
        manifest_body = {'source': {'artifact_id': str(source), 'sha256': source_sha}, 'variant': 'preview1600', 'profile_hash': profile, 'derivative': {'artifact_id': str(image), 'sha256': 'e' * 64}}
        manifest_raw = canonical_bytes(manifest_body).decode()
        manifest_sha = sha256(manifest_raw.encode()).hexdigest()
        outputs, receipt_objects = [], []
        for aid, role, mime, artifact_sha in [(image, 'derivative', 'image/jpeg', 'e' * 64), (manifest, 'derivative_manifest', 'application/json', manifest_sha)]:
            key = 'pair/' + str(aid)
            db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,'v1',%s,10,%s,%s,clock_timestamp(),%s,1,1)",
                       (aid, tenant, key, artifact_sha, mime, role, task))
            db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES(%s,%s,1,1,%s,%s,%s,%s,%s,'fixture',%s,%s,10,%s,'ready','v1')",
                       (tenant, task, args[5], 'a' * 64, uuid4(), aid, role, key, artifact_sha, mime))
            outputs.append({'artifact_id': str(aid), 'sha256': artifact_sha})
            receipt_objects.append(dict(role=role, artifact_id=str(aid), sha256=artifact_sha, bucket='fixture', key=key, version='v1', bytes=10, media_type=mime))
        db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')", (tenant, task, args[5]))
        output_result = db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id", (tenant, task, Jsonb(outputs), 'd' * 64)).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s", (output_result, task))
        manifest_body = {'source': {'artifact_id': str(source), 'sha256': source_sha}, 'variant': 'preview1600', 'profile_hash': profile, 'derivative': {'artifact_id': str(image), 'sha256': 'e' * 64}}
        receipt = {'result_id': str(output_result), 'attempt': 1, 'fence': 1, 'manifest': manifest_raw, 'manifest_id': str(manifest), 'objects': receipt_objects}
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_fn_owner')
            db.execute("INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,state,object_version,completed_at,v26_derivative_params,v26_derivative_receipt) VALUES(%s,%s,%s,'task_params',%s,%s,%s,2,'fixture','ready','v1',clock_timestamp(),%s,%s)",
                       (tenant, presentation, task, param, uuid4(), digest, body, Jsonb(receipt)))
        if side == 'before':
            db.execute("INSERT INTO photo.derivatives(tenant_id,asset_version_id,variant,profile_hash,artifact_id,status,task_id) VALUES(%s,%s,'preview1600',%s,%s,'ready',%s) ON CONFLICT(tenant_id,asset_version_id,variant,profile_hash) DO UPDATE SET artifact_id=EXCLUDED.artifact_id,status='ready',task_id=EXCLUDED.task_id", (tenant, scope['version'], profile, image, task))
        grant[side] = {'version': 1, 'tenant': str(tenant), 'review': str(review), 'user': str(scope['actor']), 'session': str(scope['session']), 'side': side, 'variant': 'preview1600', 'source': str(source), 'source_sha256': source_sha, 'role': 'image', 'artifact': str(image), 'sha256': 'e' * 64, 'object_key_sha256': sha256(('pair/' + str(image)).encode()).hexdigest(), 'object_version': 'v1', 'expires_unix': expiry}
    db.execute("UPDATE photo.runs SET status='completed',graph_sealed=true WHERE id=%s", (presentation,))
    return dict(tenant=tenant, review=review, actor=scope['actor'], session=scope['session'], grant=grant, images=images, manifests=manifests, tasks=tasks, presentation=presentation)


def pair_ready(db, fixture, grant=None):
    with db.transaction():
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(fixture['tenant']),))
        db.execute('SET LOCAL ROLE photo_fn_owner')
        return db.execute('SELECT photo.v28_relight_viewed_pair_ready(%s,%s,%s,%s,1,%s)',
            (fixture['tenant'], fixture['review'], fixture['actor'], fixture['session'], Jsonb(fixture['grant'] if grant is None else grant))).fetchone()[0]
