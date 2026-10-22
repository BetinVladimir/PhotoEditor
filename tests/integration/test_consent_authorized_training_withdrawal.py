"""Authorized grant, training SQL adoption, withdrawal and both settlement orders.

Authored only: qualification, source objects and canonical worker receipts are
seeded relational evidence, not physical worker/model/object verification.
The positive training chain is adapted from test_v23_training_freeze, keeping
its real reserve/complete/freeze/stage/close/adopt/safety/activation SQL calls.
"""
from hashlib import sha256
import json
from uuid import uuid4, uuid5

import pytest
from psycopg import errors
from psycopg.types.json import Jsonb

from photo_worker.operations.learning.contracts import TrainingManifest
from semantic_qualification_fixture import frozen_qualification
from test_database import tenant
from test_v23_training_freeze import _artifact, _jcs_simple

def _synthetic_finished_attempt(db, tenant_id, task_id):
    """Relational receipt only: no worker execution or physical verification."""
    worker = uuid4()
    profile = db.execute('SELECT execution_profile FROM photo.tasks WHERE tenant_id=%s AND id=%s',
                         (tenant_id, task_id)).fetchone()[0]
    db.execute('INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,'
               'worker_id,started_at,finished_at,backend,profile) '
               'VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),%s,%s)',
               (tenant_id, task_id, worker, profile['backend'], Jsonb(profile)))
    return worker


def _check_constructed_receipt_constraints(db):
    # Flush deferred authority/FK checks at construction, not a later tenant seed.
    db.execute('SET CONSTRAINTS ALL IMMEDIATE')
    db.execute('SET CONSTRAINTS ALL DEFERRED')


def _fixture(db, *, owner=None, qualification):
    tid, actor = tenant(db, str(uuid4()) + '@example.test') if owner is None else owner
    auth_session = db.execute(
        "INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) "
        "VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') RETURNING id",
        (actor, uuid4().bytes)).fetchone()[0]
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) "
                         "VALUES(%s,'Correction') RETURNING id", (tid,)).fetchone()[0]
    collection = db.execute("INSERT INTO photo.collections(tenant_id,project_id,name) "
                            "VALUES(%s,%s,'Session') RETURNING id",
                            (tid, project)).fetchone()[0]
    def artifact(digest, role='source'):
        return db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,"
                          "object_version,sha256,bytes,media_type,output_role,verified_at) "
                          "VALUES(%s,'test',%s,'v1',%s,64,'application/json',%s,now()) RETURNING id",
                          (tid, str(uuid4()), digest, role)).fetchone()[0]
    original = artifact('a' * 64)
    asset = db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,"
                       "metadata,upload_state,provenance) "
                       "VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",
                       (tid, project, original)).fetchone()[0]
    version = db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,"
                         "master_artifact_id,content_hash,metadata,review_state) "
                         "VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",
                         (tid, asset, original, 'a' * 64)).fetchone()[0]
    db.execute("INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,"
               "asset_version_id,position) VALUES(%s,%s,%s,%s,1)",
               (tid, collection, asset, version))
    shoot = db.execute("INSERT INTO photo.shoot_sessions(tenant_id,project_id,"
                       "collection_id,name,state,policy,consent_epoch) "
                       "VALUES(%s,%s,%s,'Test','open','{}',1) RETURNING id",
                       (tid, project, collection)).fetchone()[0]
    stream = db.execute("INSERT INTO photo.v2_document_streams(tenant_id,project_id,"
                        "asset_version_id,stream_type) VALUES(%s,%s,%s,'draft') RETURNING id",
                        (tid, project, version)).fetchone()[0]
    base, approved = uuid4(), uuid4()
    base_artifact, approved_artifact, resolved = (
        artifact(c * 64, 'document') for c in 'bcd')
    for document, number, aid, digest in (
        (base, 1, base_artifact, 'b' * 64),
        (approved, 2, approved_artifact, 'c' * 64)):
        db.execute("INSERT INTO photo.v2_documents(id,tenant_id,project_id,stream_id,"
                   "version_no,document_type,artifact_id,sha256) "
                   "VALUES(%s,%s,%s,%s,%s,'EditingRecipeV3',%s,%s)",
                   (document, tid, project, stream, number, aid, digest))
    db.execute("UPDATE photo.v2_document_streams SET current_document_id=%s,"
               "approved_document_id=%s WHERE id=%s", (approved, approved, stream))
    db.execute("INSERT INTO photo.v2_document_approvals(tenant_id,project_id,"
               "document_id,actor_id,recipe_sha,resolved_artifact_id) "
               "VALUES(%s,%s,%s,%s,%s,%s)",
               (tid, project, approved, actor, 'c' * 64, resolved))
    # Session API creates revoked@1; qualification is synthetic relational evidence.
    calibration = artifact('6' * 64)
    calibration_ref = dict(artifact_id=str(calibration), sha256='6' * 64)
    if not qualification:
        imported = frozen_qualification(db, tid, project, actor,
            operation='session.describe', calibration_ref=calibration_ref,
            model_lock_sha='1' * 64, device_profile_sha='2' * 64)
        # Canonical report contains digest pins, not calibration artifact IDs.
        body = _jcs_simple(dict(schema_version=1, operation='session.describe',
            version='1', model_lock_sha='1' * 64, runtime_lock_sha='b' * 64,
            device_profile_sha='2' * 64, dataset_sha='d' * 64,
            input_fixture_shas=['e' * 64],
            checks=[dict(id='quality', passed=True, evidence_sha='f' * 64)],
            metrics=dict(p50_ms=10, p95_ms=20, peak_vram_bytes=0,
                         peak_rss_bytes=0, scratch_bytes=0),
            status='passed', issuer_id=str(actor), created_at='2026-09-01T00:00:00Z'))
        assert sha256(body).hexdigest() == imported['report']['sha256']
        qualification.update(body=body, sha=imported['report']['sha256'],
                             policy_id=imported['policy_id'])
    else:
        body = qualification['body']
        report = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,"
            "object_version,sha256,bytes,media_type,verified_at) VALUES(%s,'test',%s,"
            "'v1',%s,%s,'application/json',clock_timestamp()) RETURNING id",
            (tid, str(uuid4()), qualification['sha'], len(body))).fetchone()[0]
        db.execute('INSERT INTO photo.v2_qualification_operators(operator_id,enabled) '
                   'VALUES(%s,true) ON CONFLICT(operator_id) DO NOTHING', (actor,))
        db.execute('SET LOCAL ROLE photo_maintenance')
        try:
            db.execute('SELECT photo.import_v2_qualification(%s,%s,%s,%s,%s,%s,%s)',
                       (tid, project, report, qualification['sha'], actor,
                        'cuda', body.decode())).fetchone()
        finally:
            db.execute('RESET ROLE')
    assert db.execute("SELECT count(*) FROM photo.v2_qualification_policies WHERE "
        "operation='session.describe' AND admission_enabled").fetchone() == (1,)
    assert db.execute("SELECT trusted_import,status,measured_p95_ms FROM "
        "photo.model_qualifications WHERE tenant_id=%s AND project_id=%s AND "
        "operation='session.describe'", (tid, project)).fetchone() == (True, 'passed', 20)
    policy = dict(schema_version=1, time_gap_seconds=60, neighbor_count=4,
                  protect_manual_picks=True, camera_offsets=[], calibration=calibration_ref)
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tid),))
    db.execute('SET LOCAL ROLE photo_api')
    db.execute("SELECT photo.create_v22_session(%s,%s,%s,'Consent bootstrap',%s,NULL,%s,%s,%s)",
               (tid, project, collection, Jsonb(policy), '1' * 64, actor, auth_session))
    db.execute('RESET ROLE')
    consent = db.execute("SELECT id FROM photo.consent_grants WHERE tenant_id=%s "
                         "AND project_id=%s AND purpose='learning_photographer'",
                         (tid, project)).fetchone()[0]
    assert db.execute('SELECT status,epoch,revision FROM photo.consent_grants WHERE id=%s',
                      (consent,)).fetchone() == ('revoked', 1, 1)
    db.execute('SET LOCAL ROLE photo_api')
    granted = db.execute('SELECT photo.change_v2_consent(' + ','.join(['%s'] * 10) + ')',
                         (tid, project, consent, 1, 'learning_photographer', None,
                          'grant', 'v2.2', actor, auth_session)).fetchone()[0]
    db.execute('RESET ROLE')
    assert granted['status'] == 'granted' and granted['epoch'] == '2'
    request = dict(client_event_id=str(uuid4()), session_id=str(shoot),
                   asset_version_id=str(version), base_document_id=str(base),
                   approved_document_id=str(approved), reason='exposure', learn=True,
                   person_id=None, scene_group_id=None, consent_id=str(consent),
                   consent_epoch='2')
    return tid, actor, auth_session, project, request, consent


def _adopted_training(db, *, qualification, base=None, stop_before_evaluate_result=False):
    base = _fixture(db, qualification=qualification) if base is None else base
    tenant, actor, auth_session, project, request, consent = base
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING', (tenant,))
    collection = db.execute('SELECT collection_id FROM photo.shoot_sessions '
                            'WHERE id=%s', (request['session_id'],)).fetchone()[0]
    sessions = [request['session_id']]
    for index in range(5):
        sessions.append(str(db.execute(
            'INSERT INTO photo.shoot_sessions(tenant_id,project_id,collection_id,'
            "name,state,policy,consent_epoch) VALUES(%s,%s,%s,%s,'open','{}',1) "
            'RETURNING id', (tenant, project, collection, f'Session {index}')).fetchone()[0]))
    base_artifact, approved_artifact, resolved_artifact = db.execute(
        'SELECT base.artifact_id,approved.artifact_id,approval.resolved_artifact_id '
        'FROM photo.v2_documents base JOIN photo.v2_documents approved '
        'ON approved.tenant_id=base.tenant_id '
        'JOIN photo.v2_document_approvals approval '
        'ON approval.tenant_id=approved.tenant_id '
        'AND approval.document_id=approved.id '
        'WHERE base.id=%s AND approved.id=%s',
        (request['base_document_id'], request['approved_document_id'])).fetchone()
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
    db.execute('SET LOCAL ROLE photo_api')
    profile = db.execute('SELECT photo.create_v23_profile(%s,%s,%s,%s,%s,%s,%s)',
                         (tenant, project, 'Training', 'photographer', None,
                          actor, auth_session)).fetchone()[0]
    db.execute('RESET ROLE')
    key = 'freeze-' + uuid4().hex
    run = uuid5(tenant, f'V2.Admit:{actor}:{key}')
    examples = []
    frozen_documents=[]
    for index in range(40):
        source_sha = f'{index + 1:064x}'
        original = _artifact(db, tenant, source_sha)
        asset = db.execute(
            'INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,'
            "metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') "
            'RETURNING id', (tenant, project, original)).fetchone()[0]
        version = db.execute(
            'INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,'
            "content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') "
            'RETURNING id', (tenant, asset, original, source_sha)).fetchone()[0]
        db.execute('INSERT INTO photo.collection_items(tenant_id,collection_id,'
                   'asset_id,asset_version_id,position) VALUES(%s,%s,%s,%s,%s)',
                   (tenant, collection, asset, version, index + 2))
        stream = db.execute(
            'INSERT INTO photo.v2_document_streams(tenant_id,project_id,'
            "asset_version_id,stream_type) VALUES(%s,%s,%s,'draft') RETURNING id",
            (tenant, project, version)).fetchone()[0]
        base_doc, approved_doc = uuid4(), uuid4()
        for doc, number, artifact, digest in (
            (base_doc, 1, base_artifact, 'b' * 64),
            (approved_doc, 2, approved_artifact, 'c' * 64),
        ):
            db.execute('INSERT INTO photo.v2_documents(id,tenant_id,project_id,'
                       'stream_id,version_no,document_type,artifact_id,sha256) '
                       "VALUES(%s,%s,%s,%s,%s,'EditingRecipeV3',%s,%s)",
                       (doc, tenant, project, stream, number, artifact, digest))
        db.execute('UPDATE photo.v2_document_streams SET current_document_id=%s,'
                   'approved_document_id=%s WHERE id=%s',
                   (approved_doc, approved_doc, stream))
        approval = db.execute(
            'INSERT INTO photo.v2_document_approvals(tenant_id,project_id,'
            'document_id,actor_id,recipe_sha,resolved_artifact_id) '
            'VALUES(%s,%s,%s,%s,%s,%s) RETURNING id',
            (tenant, project, approved_doc, actor, 'c' * 64,
             resolved_artifact)).fetchone()[0]
        event = db.execute(
            'INSERT INTO photo.correction_events(tenant_id,project_id,session_id,'
            'asset_version_id,base_document_id,approved_document_id,changed_paths,'
            'reason,learn,consent_id,consent_epoch,approved_by,client_event_id) '
            "VALUES(%s,%s,%s,%s,%s,%s,%s,'exposure',true,%s,2,%s,%s) RETURNING id",
            (tenant, project, sessions[index % 6], version, base_doc,
             approved_doc, Jsonb(['/ev']), consent, actor, uuid4())).fetchone()[0]
        ref = lambda aid, digest: {'artifact_id': str(aid), 'sha256': digest}
        source_ref = ref(original, source_sha)
        frozen={'schema_version':1,'source':{'asset_version_id':str(version),'source':source_ref,'canvas_hash':source_sha},'base_style':ref(resolved_artifact,'d'*64),'effective_delta':{key:0 for key in ('ev','log_r','log_g','log_b','saturation_delta','retouch_delta')},'renderer_version':'v2.2','icc_lock_sha':'f'*64,'model_lock_sha':'0'*64}
        frozen_body=_jcs_simple(frozen);frozen_sha=sha256(frozen_body).hexdigest()
        frozen_refs={side:ref(uuid5(run,f'v23-frozen/{side}/{event}'),frozen_sha) for side in ('base','approved')}
        frozen_documents.append((event,frozen_body))
        examples.append({
            'event_id': str(event), 'session_id': sessions[index % 6],
            'subject_group_id': str(project),
            'split': 'train' if index % 6 < 4 else 'holdout',
            'features': [0] * 12,
            'target': {key: 0 for key in (
                'ev', 'log_r', 'log_g', 'log_b', 'saturation_delta', 'retouch_delta')},
            'render_case': {
                'binding': {'asset_version_id': str(version),
                            'source': source_ref, 'canvas_hash': source_sha},
                'working_image': source_ref,
                'base_recipe': frozen_refs['base'],
                'approved_recipe': frozen_refs['approved'],
                'base_document': ref(base_artifact,'b'*64),
                'approved_document': ref(approved_artifact,'c'*64),
                'descriptor_shard': ref(resolved_artifact,'d'*64),
                'descriptor_analysis_id': str(uuid4()),
                'descriptor_ordinal': index,
                'protected': ref(resolved_artifact, 'd' * 64),
                'faces': ref(resolved_artifact, 'd' * 64),
                'approval_id': str(approval),
                'source_lineage_sha': source_sha}})
    zero_body=_jcs_simple({'schema_version':1,'profile_id':str(profile),'training_sha':'0'*64,'feature_order':'scene12_v1','means':[0]*12,'scales':[1]*12,'coefficients':[[0]*6 for _ in range(12)],'intercept':[0]*6,'ridge_lambda':1})
    zero_sha=sha256(zero_body).hexdigest()
    policy_body=_jcs_simple({'schema_version':1})
    policy_sha=sha256(policy_body).hexdigest()
    manifest = {'schema_version': 1, 'profile_id': str(profile),
                'mode': 'photographer', 'person_id': None,
                'consents': [{'consent_id': str(consent), 'epoch': '2'}],
                'examples': examples, 'policy_sha': policy_sha,
                'baseline': {'artifact_id': str(uuid5(run,'v23-zero-profile/'+str(profile))), 'sha256': zero_sha},
                'renderer_version': 'v2.2', 'icc_lock_sha': 'f' * 64,
                'model_lock_sha': '0' * 64,
                'calibration': {'artifact_id': str(resolved_artifact), 'sha256': 'd' * 64}}
    TrainingManifest.model_validate_json(_jcs_simple(manifest))
    workflow, snapshot = uuid5(run, 'workflow'), uuid5(run, 'training.snapshot')
    manifest_sha = sha256(_jcs_simple(manifest)).hexdigest()
    manifest_id = uuid5(run, 'v23_training_manifest')
    root = {'schema_version': 1, 'snapshot_id': str(snapshot),
            'project_id': str(project), 'items': [{
                'ordinal': 0,
                'artifact': {'artifact_id': str(manifest_id), 'sha256': manifest_sha},
                'contract': 'TrainingManifest', 'owner_id': str(profile)}]}
    snapshot_digest = sha256(_jcs_simple(root)).hexdigest()
    root['snapshot_digest'] = snapshot_digest
    root_sha = sha256(_jcs_simple(root)).hexdigest()
    config = {'project_id': str(project), 'v23_training': {
        'schema_version': 1, 'profile_id': str(profile),
        'expected_revision': '1', 'manifest_sha': manifest_sha,
        'snapshot_digest': snapshot_digest, 'snapshot_body_sha': root_sha}}
    db.execute('SET LOCAL ROLE photo_api')
    admitted = db.execute('SELECT * FROM photo.begin_v23_training_draft('
                          + ','.join(['%s'] * 13) + ')',
                          (tenant, run, workflow, profile, 1, actor, auth_session,
                           policy_sha, '1' * 64, Jsonb(config), 4096,
                           key, '2' * 64)).fetchone()
    assert admitted == ('created', run, snapshot)
    recovery={'schema_version':1,'manifest_jcs':_jcs_simple(manifest).decode(),'manifest_sha':manifest_sha,'snapshot_jcs':_jcs_simple(root).decode(),'snapshot_body_sha':root_sha,'snapshot_digest':snapshot_digest,'policy_jcs':policy_body.decode(),'policy_sha':policy_sha,'reserved_bytes':4096,'expected_revision':1,'profile_id':str(profile),'project_id':str(project),'frozen_jcs':_jcs_simple([{'event_id':str(event),'base_jcs':body.decode(),'approved_jcs':body.decode()} for event,body in frozen_documents]).decode()}
    db.execute('RESET ROLE');db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.save_v23_training_recovery('+','.join(['%s']*7)+')',(tenant,actor,auth_session,run,key,'2'*64,Jsonb(recovery))).fetchone()[0]
    state,zero_id,zero_token,ready=db.execute('SELECT * FROM photo.reserve_v23_training_zero('+','.join(['%s']*6)+')',(tenant,run,profile,zero_sha,len(zero_body),'test-bucket')).fetchone()
    assert state=='created' and not ready
    assert db.execute('SELECT photo.complete_v23_training_zero('+','.join(['%s']*9)+')',(tenant,run,profile,zero_id,zero_token,zero_sha,len(zero_body),'test-bucket','v1')).fetchone()[0]
    for event,body in frozen_documents:
        digest=sha256(body).hexdigest()
        for side in ('base','approved'):
            state,aid,token,ready=db.execute('SELECT * FROM photo.reserve_v23_training_frozen('+','.join(['%s']*7)+')',(tenant,run,event,side,digest,len(body),'test-bucket')).fetchone()
            assert state=='created' and not ready
            assert db.execute('SELECT photo.complete_v23_training_frozen('+','.join(['%s']*10)+')',(tenant,run,event,side,aid,token,digest,len(body),'test-bucket','v1')).fetchone()[0]
    for owner, purpose, digest, size in (
        (profile, 'v23_training_manifest', manifest_sha, len(_jcs_simple(manifest))),
        (run, 'v23_training_snapshot', root_sha, len(_jcs_simple(root))),
    ):
        state, aid, token, ready = db.execute(
            'SELECT * FROM photo.reserve_v23_training_document('
            + ','.join(['%s'] * 7) + ')',
            (tenant, run, owner, purpose, digest, size, 'test-bucket')).fetchone()
        assert state == 'created' and aid == uuid5(run, purpose) and ready is False
        assert db.execute('SELECT photo.complete_v23_training_document('
                          + ','.join(['%s'] * 10) + ')',
                          (tenant, run, owner, purpose, aid, token, digest,
                           size, 'test-bucket', 'v1')).fetchone()[0] is True
    assert db.execute('SELECT photo.freeze_v23_training_snapshot('
                      + ','.join(['%s'] * 9) + ')',
                      (tenant, run, profile, actor, auth_session, 1,
                       Jsonb(manifest), manifest_sha, root_sha)).fetchone()[0] is True
    db.execute('RESET ROLE')
    assert db.execute('SELECT artifact_id,digest,expires_at FROM photo.snapshots '
                      'WHERE id=%s', (snapshot,)).fetchone() == (
                          uuid5(run, 'v23_training_snapshot'), snapshot_digest, None)
    assert db.execute('SELECT artifact_id,sha256,payload FROM photo.snapshot_items '
                      'WHERE snapshot_id=%s', (snapshot,)).fetchone() == (
                          manifest_id, manifest_sha,
                          {'contract': 'TrainingManifest', 'owner_id': str(profile)})
    assert db.execute('SELECT consent_id,consent_epoch FROM '
                      'photo.workflow_consent_links WHERE workflow_id=%s',
                      (workflow,)).fetchone() == (consent, 2)
    fit_task = uuid5(workflow, 'learning.fit:0')
    params = {'schema_version': 1,
              'manifest': {'artifact_id': str(manifest_id), 'sha256': manifest_sha},
              'base_profile': manifest['baseline'],
              'policy': {'schema_version': 1, 'mode': 'photographer',
                         'min_examples': 40, 'min_sessions': 6,
                         'holdout_fraction': 0.2, 'seed': 2026,
                         'calibration': manifest['calibration']}}
    params_sha = sha256(_jcs_simple(params)).hexdigest()
    profile_json = {'backend': 'cpu', 'renderer_version': 'v2.2',
                    'resource_estimate': {'ram_bytes': 2147483648,
                                          'vram_bytes': 0,
                                          'scratch_bytes': 536870912,
                                          'output_bytes': 8388608,
                                          'pixel_count': 0}}
    db.execute('SET LOCAL ROLE photo_scheduler')
    state, params_id, token, ready = db.execute(
        'SELECT * FROM photo.reserve_v23_training_document('
        + ','.join(['%s'] * 7) + ')',
        (tenant, run, fit_task, 'v23_fit_params', params_sha,
         len(_jcs_simple(params)), 'test-bucket')).fetchone()
    assert state == 'created' and params_id == uuid5(run, 'v23_fit_params')
    assert ready is False
    assert db.execute('SELECT photo.complete_v23_training_document('
                      + ','.join(['%s'] * 10) + ')',
                      (tenant, run, fit_task, 'v23_fit_params', params_id,
                       token, params_sha, len(_jcs_simple(params)),
                       'test-bucket', 'v1')).fetchone()[0] is True
    stage = ('SELECT photo.stage_v23_learning_fit('
             + ','.join(['%s'] * 7) + ')')
    args = (tenant, run, fit_task, params_id, params_sha,
            '9' * 64, Jsonb(profile_json))
    assert db.execute(stage, args).fetchone()[0] is True
    assert db.execute(stage, args).fetchone()[0] is True
    assert db.execute(stage, (tenant, run, uuid4(), *args[3:])).fetchone()[0] is False
    db.execute('RESET ROLE')
    assert db.execute('SELECT node_key,operation,state,params_artifact_id '
                      'FROM photo.tasks WHERE id=%s', (fit_task,)).fetchone() == (
                          'learning.fit:0', 'learning.fit', 'blocked', params_id)
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.freeze_v23_training_snapshot('
                      + ','.join(['%s'] * 9) + ')',
                      (tenant, run, profile, actor, auth_session, 1,
                       Jsonb(manifest), manifest_sha, root_sha)).fetchone()[0] is True
    db.execute('RESET ROLE')
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute('UPDATE photo.snapshots SET artifact_id=%s WHERE id=%s',
                   (examples[0]['render_case']['working_image']['artifact_id'], snapshot))
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute('UPDATE photo.snapshot_items SET sha256=%s WHERE snapshot_id=%s',
                   ('3' * 64, snapshot))
    fit_worker = _synthetic_finished_attempt(db, tenant, fit_task)
    candidate = db.execute(
        'INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,'
        'sha256,bytes,media_type,output_role,verified_at,created_by_task_id,'
        "produced_attempt,produced_fence) VALUES(%s,'test',%s,'v1',%s,64,"
        "'application/json','document',now(),%s,1,1) RETURNING id",
        (tenant, str(uuid4()), '8' * 64, fit_task)).fetchone()[0]
    candidate_key = db.execute('SELECT object_key FROM photo.artifacts WHERE id=%s',
                               (candidate,)).fetchone()[0]
    db.execute(
        'INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,'
        'worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,'
        'object_key,sha256,bytes,media_type,state,object_version,completed_at) VALUES'
        "(%s,%s,1,1,%s,%s,%s,%s,'document','test',%s,%s,64,"
        "'application/json','ready','v1',clock_timestamp())",
        (tenant, fit_task, fit_worker, '8' * 64, uuid4(), candidate,
         candidate_key, '8' * 64))
    result = db.execute(
        'INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,'
        "outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) "
        'RETURNING id', (tenant, fit_task, Jsonb([{
            'artifact_id': str(candidate), 'sha256': '8' * 64}]),
            '7' * 64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',attempt=1,"
               'fencing_token=1,canonical_result_id=%s WHERE id=%s',
               (result, fit_task))
    _check_constructed_receipt_constraints(db)
    db.execute("UPDATE photo.runs SET status='running',graph_sealed=true "
               'WHERE id=%s', (run,))
    assert db.execute('SELECT photo.open_v23_learning_evaluate_barrier(%s,%s)',
                      (tenant, fit_task)).fetchone()[0] is False
    marker = db.execute('SELECT photo.v2_adoption_event_id(%s,%s)',
                        (fit_task, result)).fetchone()[0]
    db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) "
               "VALUES(%s,'v2-adopt',%s,now())", (tenant, marker))
    assert db.execute('SELECT photo.open_v23_learning_evaluate_barrier(%s,%s)',
                      (tenant, fit_task)).fetchone()[0] is True
    assert db.execute('SELECT photo.open_v23_learning_evaluate_barrier(%s,%s)',
                      (tenant, fit_task)).fetchone()[0] is False
    assert db.execute('SELECT graph_sealed FROM photo.runs WHERE id=%s',
                      (run,)).fetchone()[0] is False
    assert db.execute('SELECT state,parent_task_id FROM photo.graph_expansions '
                      "WHERE run_id=%s AND kind='v2.learning.evaluate/1'",
                      (run,)).fetchone() == ('open', fit_task)
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT * FROM photo.v23_learning_candidate_input(%s,%s)',
                      (tenant, run)).fetchone() == (fit_task, candidate, '8' * 64)
    claim = db.execute('SELECT * FROM photo.claim_v23_learning_evaluate()').fetchone()
    assert claim is not None and claim[3:] == (
        tenant, run, workflow, candidate, '8' * 64)
    assert db.execute('SELECT * FROM photo.claim_v23_learning_evaluate()').fetchone() is None
    db.execute('RESET ROLE')
    person = db.execute('INSERT INTO photo.project_persons(tenant_id,project_id,'
                        "label,state,consent_epoch) VALUES(%s,%s,'Holdout',"
                        "'active',1) RETURNING id", (tenant, project)).fetchone()[0]
    matching = db.execute('INSERT INTO photo.consent_grants(tenant_id,project_id,'
                          'purpose,person_id,status,epoch,policy_version,actor_id) '
                          "VALUES(%s,%s,'identity_matching',%s,'granted',1,'v1',%s) "
                          'RETURNING id', (tenant, project, person, actor)).fetchone()[0]
    identity_body = {'schema_version': 1, 'confirmed_faces': [],
                     'grants': [{'consent_id': str(matching), 'epoch': '1',
                                 'person_id': str(person),
                                 'purpose': 'identity_matching'}]}
    db.execute('SET LOCAL ROLE photo_scheduler')
    pin = ('SELECT photo.v23_pin_evaluate_identity_consents(%s,%s,%s,%s,%s)')
    stale = {**identity_body, 'grants': [
        {**identity_body['grants'][0], 'epoch': '2'}]}
    assert db.execute(pin, (tenant, run, claim[0], claim[1],
                            Jsonb(stale))).fetchone()[0] is False
    assert db.execute(pin, (tenant, run, claim[0], uuid4(),
                            Jsonb(identity_body))).fetchone()[0] is False
    assert db.execute(pin, (tenant, run, claim[0], claim[1],
                            Jsonb(identity_body))).fetchone()[0] is True
    db.execute('RESET ROLE')
    assert db.execute('SELECT consent_epoch FROM photo.workflow_consent_links '
                      'WHERE workflow_id=%s AND consent_id=%s',
                      (workflow, matching)).fetchone() == (1,)
    db.execute('SET LOCAL ROLE photo_scheduler')
    eval_params = {'schema_version': 1,
                   'candidate': {'artifact_id': str(candidate), 'sha256': '8' * 64},
                   'manifest': {'artifact_id': str(manifest_id), 'sha256': manifest_sha},
                   'baseline': manifest['baseline'],
                   'calibration': manifest['calibration'],
                   'model_lock_sha': manifest['model_lock_sha'],
                   'icc_lock_sha': manifest['icc_lock_sha'],
                   'renderer_version': 'v2.2'}
    eval_sha = sha256(_jcs_simple(eval_params)).hexdigest()
    eval_size = len(_jcs_simple(eval_params))
    reserve = ('SELECT * FROM photo.reserve_v23_evaluate_document('
               + ','.join(['%s'] * 7) + ')')
    assert db.execute(reserve, (tenant, run, uuid4(), 'v23_evaluate_params',
                                eval_sha, eval_size, 'test-bucket')).fetchone()[0] == 'forbidden'
    eval_state, eval_id, eval_token, eval_ready = db.execute(reserve, (
        tenant, run, claim[0], 'v23_evaluate_params', eval_sha,
        eval_size, 'test-bucket')).fetchone()
    assert eval_state == 'created' and not eval_ready
    assert db.execute('SELECT photo.complete_v23_evaluate_document('
                      + ','.join(['%s'] * 10) + ')',
                      (tenant, run, claim[0], 'v23_evaluate_params',
                       eval_id, eval_token, eval_sha, eval_size,
                       'test-bucket', 'v1')).fetchone()[0] is True
    assert db.execute(reserve, (tenant, run, claim[0], 'v23_evaluate_params',
                                eval_sha, eval_size, 'test-bucket')).fetchone()[0] == 'replayed'
    assert db.execute(reserve, (tenant, run, claim[0], 'v23_evaluate_params',
                                '6' * 64, eval_size,
                                'test-bucket')).fetchone()[0] == 'conflict'
    eval_task = uuid5(workflow, 'learning.evaluate:0')
    eval_profile = {'backend': 'cuda', 'renderer_version': 'v2.2',
                    'resource_estimate': {'ram_bytes': 8589934592,
                                          'vram_bytes': 8589934592,
                                          'scratch_bytes': 1073741824,
                                          'output_bytes': 8388608,
                                          'pixel_count': 40000000}}
    stage_eval = ('SELECT photo.stage_v23_learning_evaluate('
                  + ','.join(['%s'] * 11) + ')')
    stage_args = (tenant, run, claim[0], claim[1], eval_task,
                  eval_id, eval_sha, candidate, '8' * 64,
                  '5' * 64, Jsonb(eval_profile))
    assert db.execute(stage_eval, (*stage_args[:8], '6' * 64,
                                   *stage_args[9:])).fetchone()[0] is False
    assert db.execute(stage_eval, stage_args).fetchone()[0] is True
    assert db.execute(stage_eval, stage_args).fetchone()[0] is True
    assert db.execute('SELECT photo.close_v23_learning_evaluate_guarded(%s,%s,%s,%s,%s)',
                      (tenant, run, claim[0], claim[1], eval_task)).fetchone()[0] is False
    assert db.execute('SELECT photo.close_v23_learning_evaluate_guarded(%s,%s,%s,%s,%s)',
                      (tenant, run, claim[0], uuid4(), eval_task)).fetchone()[0] is False
    db.execute('RESET ROLE')
    if stop_before_evaluate_result:
        assert db.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s',
                          (run,)).fetchone() == ('running', False)
        return dict(run=run, task=eval_task, sources=[item['render_case']['working_image']['artifact_id'] for item in examples])
    db.execute("UPDATE photo.operation_registry SET v2_enabled=true "
               "WHERE operation='learning.evaluate' AND version='1'")
    calibration = _artifact(db, tenant, '1' * 64)
    protected = _artifact(db, tenant, '2' * 64)
    faces = _artifact(db, tenant, '3' * 64)
    inputs = [(eval_id, eval_sha, 'Params'),
              (manifest_id, manifest_sha, 'TrainingManifest'),
              (candidate, '8' * 64, 'CandidateProfile'),
              (resolved_artifact, 'd' * 64, 'CandidateProfile'),
              (calibration, '1' * 64, 'Calibration'),
              (base_artifact, 'b' * 64, 'FrozenRecipe'),
              (approved_artifact, 'c' * 64, 'FrozenRecipe'),
              (protected, '2' * 64, 'MaskRef'),
              (faces, '3' * 64, 'FaceObservations')]
    inputs.extend((item['render_case']['working_image']['artifact_id'],
                   item['render_case']['working_image']['sha256'],
                   'WorkingImage') for item in examples
                  if item['split'] == 'holdout')
    for index in range(sum(item['split'] == 'holdout' for item in examples)):
        digest = f'{index + 100:064x}'
        inputs.append((_artifact(db, tenant, digest), digest, 'NpyArray'))
    inputs.append((_artifact(db, tenant, 'a' * 64), 'a' * 64, 'MaskArray'))
    inputs.append((_artifact(db, tenant, '9' * 64), '9' * 64, 'StyleV2'))
    inputs.sort(key=lambda item: str(item[0]))
    for ordinal, (artifact_id, digest, contract) in enumerate(inputs):
        db.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,'
                   'task_id,artifact_id,sha256,contract,ordinal) '
                   'VALUES(%s,%s,%s,%s,%s,%s,%s)',
                   (tenant, project, eval_task, artifact_id, digest,
                    contract, ordinal))
    chunk = _artifact(db, tenant, '4' * 64)
    transport = _artifact(db, tenant, '5' * 64)
    for offset, artifact_id, digest, contract in (
        (0, chunk, '4' * 64, 'InputClosureChunk'),
        (1, transport, '5' * 64, 'InputClosureManifest'),
    ):
        db.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,'
                   'task_id,artifact_id,sha256,contract,ordinal) '
                   'VALUES(%s,%s,%s,%s,%s,%s,%s)',
                   (tenant, project, eval_task, artifact_id, digest,
                    contract, 1000000 + offset))
    ref = lambda artifact_id, digest: {
        'artifact_id': str(artifact_id), 'sha256': digest}
    sealed_profile = dict(eval_profile)
    sealed_profile.update({'contracts_sha':
        '341984c53d787175fb5a9eec4e5d55ab74e9cd293075add8965d6064aba2e815',
        'v2_entry_count': len(inputs), 'v2_entries_sha': '6' * 64,
        'v2_input_manifest': ref(transport, '5' * 64)})
    db.execute('UPDATE photo.tasks SET execution_profile=%s,inputs=%s '
               'WHERE id=%s', (Jsonb(sealed_profile),
                               Jsonb([ref(transport, '5' * 64),
                                      ref(eval_id, eval_sha),
                                      ref(candidate, '8' * 64)]), eval_task))
    assert db.execute('SELECT photo.v2_input_closure_ready(%s)',
                      (eval_task,)).fetchone()[0] is True
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.close_v23_learning_evaluate_guarded(%s,%s,%s,%s,%s)',
                      (tenant, run, claim[0], claim[1], eval_task)).fetchone()[0] is True
    db.execute('RESET ROLE')
    assert db.execute('SELECT graph_sealed FROM photo.runs WHERE id=%s',
                      (run,)).fetchone()[0] is True
    assert db.execute('SELECT state FROM photo.tasks WHERE id=%s',
                      (eval_task,)).fetchone()[0] == 'ready'
    eval_worker = _synthetic_finished_attempt(db, tenant, eval_task)
    evaluation_artifact = db.execute(
        'INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,'
        'sha256,bytes,media_type,output_role,verified_at,created_by_task_id,'
        "produced_attempt,produced_fence) VALUES(%s,'test',%s,'v1',%s,64,"
        "'application/json','document',now(),%s,1,1) RETURNING id",
        (tenant, str(uuid4()), '0' * 64, eval_task)).fetchone()[0]
    output_key = db.execute('SELECT object_key FROM photo.artifacts WHERE id=%s',
                            (evaluation_artifact,)).fetchone()[0]
    db.execute(
        'INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,'
        'worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,'
        'object_key,sha256,bytes,media_type,state,object_version,completed_at) VALUES'
        "(%s,%s,1,1,%s,%s,%s,%s,'document','test',%s,%s,64,"
        "'application/json','ready','v1',clock_timestamp())",
        (tenant, eval_task, eval_worker, '0' * 64, uuid4(),
         evaluation_artifact, output_key, '0' * 64))
    eval_result = db.execute(
        'INSERT INTO photo.task_results(tenant_id,task_id,attempt,'
        'fencing_token,outputs,metrics,result_digest) '
        "VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",
        (tenant, eval_task, Jsonb([{'artifact_id': str(evaluation_artifact),
                                   'sha256': '0' * 64}]),
         '1' * 64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',attempt=1,"
               'fencing_token=1,canonical_result_id=%s WHERE id=%s',
               (eval_result, eval_task))
    _check_constructed_receipt_constraints(db)
    eval_marker = db.execute('SELECT photo.v2_adoption_event_id(%s,%s)',
                             (eval_task, eval_result)).fetchone()[0]
    db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) "
               "VALUES(%s,'v2-adopt',%s,now())", (tenant, eval_marker))
    eval_body = {'schema_version': 1, 'candidate_sha': '8' * 64,
                 'manifest_sha': manifest_sha, 'metric_eligible': True,
                 'safety_status': 'not_measurable', 'eligible': False,
                 'safety_regression': False}
    db.execute('SET LOCAL ROLE photo_scheduler')
    adopt = ('SELECT photo.adopt_v23_learning_version('
             + ','.join(['%s'] * 6) + ')')
    version = db.execute(adopt, (tenant, run, Jsonb(manifest),
                                 Jsonb(eval_body), evaluation_artifact,
                                 '0' * 64)).fetchone()[0]
    assert version == uuid5(workflow, 'learning.version:0')
    assert db.execute(adopt, (tenant, run, Jsonb(manifest),
                              Jsonb(eval_body), evaluation_artifact,
                              '0' * 64)).fetchone()[0] == version
    db.execute('RESET ROLE')
    assert db.execute('SELECT status FROM photo.version_eligibility '
                      'WHERE version_id=%s', (version,)).fetchone()[0] == 'needs_review'
    assert db.execute('SELECT count(*) FROM photo.learning_example_links '
                      'WHERE version_id=%s', (version,)).fetchone()[0] == 40
    assert db.execute('SELECT active_version_id FROM photo.learning_profiles '
                      'WHERE id=%s', (profile,)).fetchone()[0] is None
    db.execute('SET LOCAL ROLE photo_api')
    activate = ('SELECT photo.activate_v23_learning_profile('
                + ','.join(['%s'] * 7) + ')')
    assert db.execute(activate, (tenant, profile, 1, version, 'promote',
                                 actor, auth_session)).fetchone()[0] is False
    review = ('SELECT photo.review_v23_learning_safety('
              + ','.join(['%s'] * 8) + ')')
    assert db.execute(review, (tenant, version, 1, '9' * 64, True,
                               'Manual holdout review', actor,
                               auth_session)).fetchone()[0] is None
    review_id = db.execute(review, (tenant, version, 1, '0' * 64, True,
                                    'Manual holdout review', actor,
                                    auth_session)).fetchone()[0]
    assert review_id is not None
    assert db.execute(review, (tenant, version, 1, '0' * 64, False,
                               'Second decision', actor,
                               auth_session)).fetchone()[0] is None
    assert db.execute(activate, (tenant, profile, 1, version, 'rollback',
                                 actor, auth_session)).fetchone()[0] is False
    assert db.execute(activate, (tenant, profile, 1, version, 'promote',
                                 actor, auth_session)).fetchone()[0] is True
    db.execute('RESET ROLE')
    assert db.execute('SELECT active_version_id,revision '
                      'FROM photo.learning_profiles WHERE id=%s',
                      (profile,)).fetchone() == (version, 2)
    assert db.execute('SELECT status,revision FROM photo.version_eligibility '
                      'WHERE version_id=%s', (version,)).fetchone() == (
                          'eligible', 2)
    return dict(base=base, tenant=tenant, actor=actor, session=auth_session, project=project,
                consent=consent, run=run, workflow=workflow, profile=profile,
                version=version, manifest=manifest, request=request,
                sources=[item['render_case']['working_image']['artifact_id'] for item in examples])


def _consent_state(db, consent):
    return db.execute('SELECT status,epoch,revision FROM photo.consent_grants WHERE id=%s',
                      (consent,)).fetchone()


@pytest.mark.parametrize('consumer_order', ['v2-first', 'generic-first', 'pre-settled'])
def test_authorized_training_withdrawal_full_lifecycle(db, consumer_order):
    """No direct consent UPDATE; both scheduler orders must settle exactly once."""
    qualification = {}
    state = _adopted_training(db, qualification=qualification)
    tid, actor, session = state['tenant'], state['actor'], state['session']
    run, consent = state['run'], state['consent']
    assert _consent_state(db, consent) == ('granted', 2, 2)
    pending = _adopted_training(db, qualification=qualification, base=state['base'], stop_before_evaluate_result=True)
    # Another project in this tenant and a foreign tenant have independent grants.
    # A broad tenant/consent update or cancellation must not touch that neighbor.
    neighbor = _fixture(db, owner=(tid, actor), qualification=qualification)
    foreign = _fixture(db, qualification=qualification)
    foreign_before = _consent_state(db, foreign[5])
    neighbor_before = _consent_state(db, neighbor[5])
    neighbor_events = db.execute('SELECT count(*) FROM photo.consent_events WHERE consent_id=%s',
                                 (neighbor[5],)).fetchone()[0]
    source_before = db.execute('SELECT id,state,object_version,sha256 FROM photo.artifacts '
                               'WHERE id=ANY(%s::uuid[]) ORDER BY id',
                               (state['sources'],)).fetchall()
    qid, reserved = db.execute("SELECT id,bytes FROM photo.quota_reservations "
                               "WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s "
                               "AND state='reserved'", (tid, run)).fetchone()
    assert reserved > 0
    pending_qid, pending_reserved = db.execute(
        "SELECT id,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' "
        "AND holder_id=%s AND state='reserved'", (tid, pending['run'])).fetchone()
    assert pending_reserved > 0
    assert pending['run'] != run
    assert db.execute('SELECT count(*) FROM photo.consent_grants WHERE tenant_id=%s AND project_id=%s '
                      "AND purpose='learning_photographer'", (tid, state['project'])).fetchone()[0] == 1
    outputs_before = db.execute('SELECT to_jsonb(a) FROM photo.artifacts a WHERE a.tenant_id=%s '
        'AND a.created_by_task_id IN (SELECT id FROM photo.tasks WHERE run_id=ANY(%s::uuid[])) ORDER BY a.id',
        (tid, [run, pending['run']])).fetchall()
    documents_before = db.execute('SELECT to_jsonb(d) FROM photo.backend_documents d WHERE d.tenant_id=%s '
        'AND d.run_id=ANY(%s::uuid[]) ORDER BY d.id', (tid, [run, pending['run']])).fetchall()
    usage_before = db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage '
                              'WHERE tenant_id=%s', (tid,)).fetchone()
    assert usage_before[0] >= reserved + pending_reserved
    if consumer_order == 'pre-settled':
        # Synthetic prior settlement order: owned remaining reservation already released.
        db.execute('UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-%s WHERE tenant_id=%s',
                   (pending_reserved, tid))
        db.execute("UPDATE photo.quota_reservations SET state='released',bytes=0 WHERE id=%s", (pending_qid,))
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tid),))
    revoke = 'SELECT photo.change_v2_consent(' + ','.join(['%s'] * 10) + ')'
    args = (tid, state['project'], consent, 2, 'learning_photographer', None,
            'revoke', 'v2.2', actor, session)
    db.execute('SET LOCAL ROLE photo_api')
    changed = db.execute(revoke, args).fetchone()[0]
    assert changed['status'] == 'revoked' and changed['epoch'] == '3'
    # The SQL writer is CAS, not the HTTP idempotency wrapper. Exact duplicate
    # writer invocation must fail without another epoch/event/accounting change.
    with pytest.raises(errors.SerializationFailure), db.transaction():
        db.execute(revoke, args)
    db.execute('RESET ROLE')
    assert _consent_state(db, consent) == ('revoked', 3, 3)
    assert db.execute('SELECT epoch,action FROM photo.consent_events WHERE consent_id=%s ORDER BY epoch',
                      (consent,)).fetchall() == [(2, 'grant'), (3, 'revoke')]
    assert db.execute('SELECT active_version_id FROM photo.learning_profiles WHERE id=%s',
                      (state['profile'],)).fetchone()[0] is None
    assert db.execute('SELECT status,reason FROM photo.version_eligibility WHERE version_id=%s',
                      (state['version'],)).fetchone() == ('disabled', 'CONSENT_REVOKED')
    assert db.execute('SELECT photo.v23_learning_version_consents_live(%s,%s)',
                      (tid, state['version'])).fetchone()[0] is False
    db.execute('SET LOCAL ROLE photo_api')
    assert db.execute('SELECT photo.activate_v23_learning_profile(' + ','.join(['%s'] * 7) + ')',
                      (tid, state['profile'], 3, state['version'], 'promote', actor,
                       session)).fetchone()[0] is False
    from test_v23_record_correction import _record
    fresh = dict(state['request'], client_event_id=str(uuid4()))
    with pytest.raises(errors.CheckViolation), db.transaction():
        _record(db, tid, actor, session, fresh)
    db.execute('RESET ROLE')
    assert db.execute('SELECT status,cancel_requested,graph_sealed FROM photo.runs WHERE id=%s',
                      (run,)).fetchone() == ('cancelling', True, True)
    assert db.execute("SELECT count(*) FROM photo.consent_cleanup WHERE tenant_id=%s "
                      "AND consent_id=%s AND state='pending'", (tid, consent)).fetchone()[0] == 3
    assert db.execute('SELECT photo.v23_training_cancellation_scope(%s,%s)',
                      (tid, pending['run'])).fetchone() == (True,)
    assert db.execute('SELECT photo.v23_training_cancellation_scope(%s,%s)',
                      (foreign[0], pending['run'])).fetchone() == (False,)
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.quota_reservations SET principal_id=%s WHERE id=%s',
                   (foreign[1], pending_qid))
        assert db.execute('SELECT photo.v23_training_cancellation_scope(%s,%s)',
                          (tid, pending['run'])).fetchone() == (False,)
        # Give generic recovery only this invalid pending run; failure must be atomic.
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.finalize_v2_run(%s,%s)', (tid, run)).fetchone() == ('cancelled',)
        assert db.execute('SELECT photo.cancel_idle_task()').fetchone() == (1,)
        db.execute('RESET ROLE')
        before_expansions = db.execute('SELECT to_jsonb(e) FROM photo.graph_expansions e WHERE run_id=%s ORDER BY id',
                                       (pending['run'],)).fetchall()
        before_run = db.execute('SELECT to_jsonb(r) FROM photo.runs r WHERE id=%s', (pending['run'],)).fetchone()
        with pytest.raises(errors.CheckViolation, match='V23_TRAINING_CANCEL_SCOPE_INVALID'):
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_scheduler')
                db.execute('SELECT photo.finalize_run_cancellation()')
        assert db.execute('SELECT to_jsonb(e) FROM photo.graph_expansions e WHERE run_id=%s ORDER BY id',
                          (pending['run'],)).fetchall() == before_expansions
        assert db.execute('SELECT to_jsonb(r) FROM photo.runs r WHERE id=%s', (pending['run'],)).fetchone() == before_run
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_maintenance'):
        assert db.execute("SELECT has_function_privilege(%s,'photo.v23_training_cancellation_scope(uuid,uuid)','EXECUTE')",
                          (role,)).fetchone() == (False,)
    db.execute('SET LOCAL ROLE photo_scheduler')
    # Only the second, genuinely staged blocked evaluation is cancellable; canonical
    # results of the adopted run must remain untouched.
    assert db.execute('SELECT photo.cancel_idle_task()').fetchone()[0] == 1
    assert db.execute('SELECT photo.cancel_idle_task()').fetchone()[0] == 0
    if consumer_order == 'generic-first':
        assert db.execute('SELECT photo.finalize_run_cancellation()').fetchone()[0] == 1
    assert db.execute('SELECT photo.finalize_v2_run(%s,%s)', (tid, run)).fetchone()[0] == 'cancelled'
    # Generic recovery also handles the legitimately unsealed evaluation expansion.
    for _ in range(3):
        if db.execute('SELECT photo.finalize_run_cancellation()').fetchone()[0] == 0:
            break
    assert db.execute('SELECT photo.finalize_run_cancellation()').fetchone()[0] == 0
    assert db.execute('SELECT photo.finalize_v2_run(%s,%s)', (tid, run)).fetchone()[0] == 'cancelled'
    assert db.execute('SELECT photo.finalize_v2_run(%s,%s)', (tid, pending['run'])).fetchone()[0] == 'cancelled'
    db.execute('RESET ROLE')
    assert db.execute('SELECT status,cancel_requested FROM photo.runs WHERE id=%s',
                      (run,)).fetchone() == ('cancelled', True)
    assert db.execute('SELECT state,bytes FROM photo.quota_reservations WHERE id=%s',
                      (qid,)).fetchone() == ('released', 0)
    assert db.execute('SELECT state,bytes FROM photo.quota_reservations WHERE id=%s',
                      (pending_qid,)).fetchone() == ('released', 0)
    assert db.execute('SELECT status,cancel_requested FROM photo.runs WHERE id=%s',
                      (pending['run'],)).fetchone() == ('cancelled', True)
    assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s "
                      "AND type IN ('run.cancelled','v2.run.cancelled')",
                      (tid, pending['run'])).fetchone()[0] == 1
    assert db.execute('SELECT state,cancel_requested FROM photo.tasks WHERE id=%s',
                      (pending['task'],)).fetchone() == ('cancelled', True)
    assert db.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',
                      (tid,)).fetchone() == (usage_before[0] - reserved - pending_reserved, usage_before[1])
    assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s "
                      "AND type IN ('run.cancelled','v2.run.cancelled')", (tid, run)).fetchone()[0] == 1
    assert _consent_state(db, foreign[5]) == foreign_before
    assert _consent_state(db, neighbor[5]) == neighbor_before
    assert db.execute('SELECT count(*) FROM photo.consent_events WHERE consent_id=%s',
                      (neighbor[5],)).fetchone()[0] == neighbor_events
    assert db.execute('SELECT to_jsonb(a) FROM photo.artifacts a WHERE a.tenant_id=%s '
        'AND a.created_by_task_id IN (SELECT id FROM photo.tasks WHERE run_id=ANY(%s::uuid[])) ORDER BY a.id',
        (tid, [run, pending['run']])).fetchall() == outputs_before
    assert db.execute('SELECT to_jsonb(d) FROM photo.backend_documents d WHERE d.tenant_id=%s '
        'AND d.run_id=ANY(%s::uuid[]) ORDER BY d.id',
        (tid, [run, pending['run']])).fetchall() == documents_before
    assert db.execute('SELECT id,state,object_version,sha256 FROM photo.artifacts '
                      'WHERE id=ANY(%s::uuid[]) ORDER BY id', (state['sources'],)).fetchall() == source_before
    assert db.execute("SELECT count(*) FROM photo.tasks WHERE run_id=%s AND "
                      "(state<>'succeeded' OR canonical_result_id IS NULL)", (run,)).fetchone()[0] == 0

