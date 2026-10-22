"""Relational publication fixture, not successful physical adoption/quality proof.

Owner seeds only the adopted links/inbox expected by the actual private
publication function. The production publication function is never replaced.
"""
from uuid import uuid4
from photo_worker.contracts.base import canonical_bytes
from test_v28_relight_lease import relight_lease_fixture
from test_v28_relight_adoption_source import complete_relight_task


def relight_publication_fixture(db,*,params_declared_bytes_delta=0,params_declared_version=None):
    def params(_pin, scope):
        return canonical_bytes({'base': {'asset_version_id': str(scope['version']),
            'source': {'artifact_id': str(scope['source']), 'sha256': scope['source_sha']}}})
    args, scope, _ = relight_lease_fixture(db, params_builder=params, workflow_type='preview',
        params_declared_bytes_delta=params_declared_bytes_delta,params_declared_version=params_declared_version)
    tenant, run, task = args[:3]
    result, root = complete_relight_task(db, args, review_required=True)
    candidate = uuid4()
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,'v1',%s,2,'application/json','working_image',clock_timestamp(),%s,1,1)",
               (candidate, tenant, str(candidate), 'c' * 64, task))
    workflow = db.execute('SELECT id FROM photo.v2_workflows WHERE tenant_id=%s AND run_id=%s', (tenant, run)).fetchone()[0]
    root_sha = db.execute('SELECT sha256 FROM photo.artifacts WHERE tenant_id=%s AND id=%s', (tenant, root)).fetchone()[0]
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        for aid, digest, path in [(root, root_sha, '$'), (candidate, 'c' * 64, '$/working_image')]:
            db.execute("INSERT INTO photo.v2_artifact_links(tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal) VALUES(%s,%s,%s,%s,%s,encode(sha256(convert_to(%s,'UTF8')),'hex'),0)",
                       (tenant, scope['project'], workflow, aid, digest, 'tasks/' + str(task) + '/' + path))
        db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',photo.v2_adoption_event_id(%s,%s),clock_timestamp())", (tenant, task, result))
    return dict(tenant=tenant,run=run,task=task,result=result,body=args[8],candidate=candidate,base=scope['source'],params=args[6])


def publish_relight_review(db, fixture, body=None):
    with db.transaction():
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(fixture['tenant']),))
        db.execute('SET LOCAL ROLE photo_fn_owner')
        return db.execute('SELECT photo.create_v28_relight_review(%s,%s,%s,%s)',
            (fixture['tenant'],fixture['task'],fixture['result'],fixture['body'] if body is None else body)).fetchone()[0]
