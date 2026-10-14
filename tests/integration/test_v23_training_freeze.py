"""A complete training inventory freezes one manifest and its consent epoch."""

from hashlib import sha256
import json
from uuid import uuid4, uuid5

import pytest
from psycopg import errors
from psycopg.types.json import Jsonb

from photo_worker.operations.learning.contracts import TrainingManifest
from test_v23_record_correction import _fixture


def _jcs_simple(body):
    # This fixture uses ASCII, integral numbers and no floating-point values.
    return json.dumps(body, sort_keys=True, separators=(',', ':')).encode()


def _artifact(db, tenant, digest, role='source'):
    return db.execute(
        'INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,'
        'sha256,bytes,media_type,output_role,verified_at) '
        "VALUES(%s,'test',%s,'v1',%s,64,'application/json',%s,now()) RETURNING id",
        (tenant, str(uuid4()), digest, role)).fetchone()[0]


def test_training_freeze_happy_path_and_consent_pin(db):
    tenant, actor, auth_session, project, request, consent = _fixture(db)
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)', (tenant,))
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
            "VALUES(%s,%s,%s,%s,%s,%s,%s,'exposure',true,%s,1,%s,%s) RETURNING id",
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
                'consents': [{'consent_id': str(consent), 'epoch': '1'}],
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
                      (workflow,)).fetchone() == (consent, 1)
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
        'object_key,sha256,bytes,media_type,state,object_version) VALUES'
        "(%s,%s,1,1,%s,%s,%s,%s,'document','test',%s,%s,64,"
        "'application/json','ready','v1')",
        (tenant, fit_task, uuid4(), '8' * 64, uuid4(), candidate,
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
    assert db.execute('SELECT photo.close_v23_learning_evaluate(%s,%s,%s,%s,%s)',
                      (tenant, run, claim[0], claim[1], eval_task)).fetchone()[0] is False
    assert db.execute('SELECT photo.close_v23_learning_evaluate(%s,%s,%s,%s,%s)',
                      (tenant, run, claim[0], uuid4(), eval_task)).fetchone()[0] is False
    db.execute('RESET ROLE')
    # Exercise the successful state transition in a rolled-back savepoint.
    # The product catalog remains disabled outside this test branch.
    with pytest.raises(RuntimeError, match='discard evaluation fixture'):
        with db.transaction():
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
            assert db.execute('SELECT photo.close_v23_learning_evaluate(%s,%s,%s,%s,%s)',
                              (tenant, run, claim[0], claim[1], eval_task)).fetchone()[0] is True
            db.execute('RESET ROLE')
            assert db.execute('SELECT graph_sealed FROM photo.runs WHERE id=%s',
                              (run,)).fetchone()[0] is True
            assert db.execute('SELECT state FROM photo.tasks WHERE id=%s',
                              (eval_task,)).fetchone()[0] == 'ready'
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
                'object_key,sha256,bytes,media_type,state,object_version) VALUES'
                "(%s,%s,1,1,%s,%s,%s,%s,'document','test',%s,%s,64,"
                "'application/json','ready','v1')",
                (tenant, eval_task, uuid4(), '0' * 64, uuid4(),
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
            with pytest.raises(RuntimeError, match='discard purpose mutation'):
                with db.transaction():
                    db.execute("UPDATE photo.consent_grants SET purpose='learning_person' "
                               'WHERE id=%s', (matching,))
                    assert db.execute('SELECT photo.v23_learning_version_consents_live('
                                      '%s,%s)', (tenant, version)).fetchone()[0] is False
                    assert db.execute(activate, (tenant, profile, 1, version,
                                                 'promote', actor,
                                                 auth_session)).fetchone()[0] is False
                    raise RuntimeError('discard purpose mutation')
            assert db.execute(activate, (tenant, profile, 1, version, 'promote',
                                         actor, auth_session)).fetchone()[0] is True
            db.execute('RESET ROLE')
            assert db.execute('SELECT active_version_id,revision '
                              'FROM photo.learning_profiles WHERE id=%s',
                              (profile,)).fetchone() == (version, 2)
            assert db.execute('SELECT status,revision FROM photo.version_eligibility '
                              'WHERE version_id=%s', (version,)).fetchone() == (
                                  'eligible', 2)
            with pytest.raises(RuntimeError, match='discard identity revoke fixture'):
                with db.transaction():
                    db.execute("UPDATE photo.consent_grants SET status='revoked',"
                               'epoch=2 WHERE id=%s', (matching,))
                    assert db.execute('SELECT active_version_id FROM '
                                      'photo.learning_profiles WHERE id=%s',
                                      (profile,)).fetchone()[0] is None
                    assert db.execute('SELECT status,reason FROM '
                                      'photo.version_eligibility WHERE version_id=%s',
                                      (version,)).fetchone() == (
                                          'disabled', 'IDENTITY_CONSENT_REVOKED')
                    db.execute("UPDATE photo.consent_grants SET status='granted',"
                               'epoch=3 WHERE id=%s', (matching,))
                    assert db.execute('SELECT photo.v23_learning_version_consents_live('
                                      '%s,%s)', (tenant, version)).fetchone()[0] is False
                    raise RuntimeError('discard identity revoke fixture')
            assert db.execute('SELECT action FROM photo.audit_log '
                              'WHERE tenant_id=%s AND resource_id IN (%s,%s) '
                              'AND action LIKE %s ORDER BY action',
                              (tenant, version, profile,
                               'learning.%')).fetchall() == [
                                   ('learning.profile_activation',),
                                   ('learning.safety_review',)]
            first_version = examples[0]['render_case']['binding']['asset_version_id']
            draft_stream, draft_revision = db.execute(
                'SELECT id,revision FROM photo.v2_document_streams '
                'WHERE tenant_id=%s AND asset_version_id=%s',
                (tenant, first_version)).fetchone()
            linked_document, linked_artifact = uuid4(), _artifact(
                db, tenant, '2' * 64, role='document')
            db.execute('INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,'
                       'project_id,asset_version_id,artifact_id,sha256,bytes,'
                       'bucket,state,object_version,completed_at) VALUES('
                       "%s,%s,%s,%s,%s,%s,64,'test','ready','v1',now())",
                       (tenant, linked_document, project, first_version,
                        linked_artifact, '2' * 64))
            db.execute('SET LOCAL ROLE photo_api')
            assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                              ('photo_api',
                               'photo.replace_v2_draft(uuid,uuid,bigint,uuid,uuid,text,'
                               'uuid,uuid,jsonb,uuid,uuid,text,jsonb,uuid,uuid,text,jsonb)',
                               'EXECUTE')).fetchone()[0] is False
            published = db.execute('SELECT photo.replace_v23_draft('
                                   + ','.join(['%s'] * 18) + ')',
                                   (tenant, first_version, draft_revision,
                                    linked_document, linked_artifact, '2' * 64,
                                    actor, auth_session, Jsonb([]), None, None,
                                    None, Jsonb([]), None, None, None, None,
                                    version)).fetchone()[0]
            db.execute('RESET ROLE')
            assert published['document_id'] == str(linked_document)
            assert db.execute('SELECT version_id FROM photo.model_use_links '
                              'WHERE document_id=%s',
                              (linked_document,)).fetchone()[0] == version
            from render_model_scope_fixture import assert_registered_render_model_authority
            assert_registered_render_model_authority(db,tenant,project,examples[0]['render_case']['binding'],version,actor,auth_session,consent)
            resolved_document, resolved_artifact = uuid4(), _artifact(
                db, tenant, '4' * 64, role='document')
            db.execute('INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,'
                       'project_id,asset_version_id,artifact_id,sha256,bytes,'
                       'bucket,state,object_version,completed_at) VALUES('
                       "%s,%s,%s,%s,%s,%s,64,'test','ready','v1',now())",
                       (tenant, resolved_document, project, first_version,
                        resolved_artifact, '4' * 64))
            db.execute('SET LOCAL ROLE photo_api')
            assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                              ('photo_api',
                               'photo.approve_v2_draft(uuid,uuid,bigint,uuid,'
                               'uuid,uuid,uuid,text)',
                               'EXECUTE')).fetchone()[0] is False
            assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                              ('photo_api',
                               'photo.approve_v23_draft(uuid,uuid,bigint,uuid,'
                               'uuid,uuid,uuid,text,uuid)',
                               'EXECUTE')).fetchone()[0] is False
            assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                              ('photo_api',
                               'photo.approve_v23_draft_with_sources('
                               'uuid,uuid,bigint,uuid,uuid,uuid,uuid,text,'
                               'uuid,uuid,text,uuid,text,uuid,integer)',
                               'EXECUTE')).fetchone()[0] is True
            db.execute('RESET ROLE')
            with pytest.raises(errors.InvalidParameterValue), db.transaction():
                db.execute('SET LOCAL ROLE photo_api')
                db.execute('SELECT photo.approve_v23_draft_with_sources('
                           + ','.join(['%s'] * 15) + ')',
                           (tenant, first_version, int(published['revision']),
                            linked_document, actor, auth_session,
                            resolved_artifact, '4' * 64, version,
                            None, None, None, None, None, None))
            source_ref = examples[0]['render_case']['working_image']
            analysis_snapshot = db.execute(
                'INSERT INTO photo.snapshots(tenant_id,kind,collection_id,'
                'source_revision,principal_type,principal_id,digest,item_count,'
                'expires_at) '
                "VALUES(%s,'v2',%s,1,'user',%s,%s,1,now()+interval '1 hour') "
                'RETURNING id',
                (tenant, collection, actor, 'e' * 64)).fetchone()[0]
            db.execute('INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,'
                       'ordinal,asset_version_id,artifact_id,sha256,payload) '
                       "VALUES(%s,%s,0,%s,%s,%s,'{}')",
                       (tenant, analysis_snapshot, first_version,
                        source_ref['artifact_id'], source_ref['sha256']))
            db.execute('UPDATE photo.snapshots SET expires_at=NULL WHERE id=%s',
                       (analysis_snapshot,))
            plan = _artifact(db, tenant, '6' * 64, role='document')
            describe_run = db.execute(
                'INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,'
                'config_snapshot,config_hash,created_by,failure_policy,graph_sealed) '
                "VALUES(%s,'v2','waiting_review',%s,%s,%s,%s,'fail_fast',true) "
                'RETURNING id',
                (tenant, analysis_snapshot, Jsonb({'project_id': str(project),
                 'session_id': str(request['session_id']),
                 'v22_session_plan': {'snapshot_digest': 'e' * 64}}),
                 'c' * 64, actor)).fetchone()[0]
            describe_workflow = db.execute(
                'INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,'
                'input_snapshot_id,workflow_type,renderer_version,policy_sha) '
                "VALUES(%s,%s,%s,%s,'session','v2.2',%s) RETURNING id",
                (tenant, project, describe_run, analysis_snapshot,
                 'd' * 64)).fetchone()[0]
            analysis = db.execute(
                'INSERT INTO photo.session_analyses(tenant_id,project_id,'
                'session_id,workflow_id,snapshot_id,plan_artifact_id,policy_sha,'
                'model_lock_sha) VALUES(%s,%s,%s,%s,%s,%s,%s,%s) RETURNING id',
                (tenant, project, request['session_id'], describe_workflow,
                 analysis_snapshot, plan, 'd' * 64,
                 manifest['model_lock_sha'])).fetchone()[0]
            group = db.execute(
                'INSERT INTO photo.scene_groups(tenant_id,project_id,analysis_id,'
                'ordinal,reference_version_id,confidence) '
                'VALUES(%s,%s,%s,0,%s,1) RETURNING id',
                (tenant, project, analysis, first_version)).fetchone()[0]
            db.execute('INSERT INTO photo.scene_members(tenant_id,project_id,'
                       'group_id,asset_version_id,snapshot_ordinal) '
                       'VALUES(%s,%s,%s,%s,0)',
                       (tenant, project, group, first_version))
            describe_task = db.execute(
                'INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,'
                'operation_version,resource_class,state,inputs,params_artifact_id,'
                'input_digest,attempt,fencing_token,execution_profile) '
                "VALUES(%s,%s,'session.describe:0','session.describe','1','gpu',"
                "'blocked','[]',%s,%s,1,1,%s) RETURNING id",
                (tenant, describe_run, plan, 'a' * 64,
                 Jsonb({'renderer_version': 'v2.2'}))).fetchone()[0]
            db.execute('INSERT INTO photo.task_attempts(tenant_id,task_id,'
                       'attempt,fencing_token,worker_id,started_at,backend,profile) '
                       "VALUES(%s,%s,1,1,%s,now(),'cuda','{}')",
                       (tenant, describe_task, uuid4()))
            descriptor = db.execute(
                'INSERT INTO photo.artifacts(tenant_id,bucket,object_key,'
                'object_version,sha256,bytes,media_type,output_role,verified_at,'
                'created_by_task_id,produced_attempt,produced_fence) '
                "VALUES(%s,'test',%s,'v1',%s,64,'application/json','document',"
                'now(),%s,1,1) RETURNING id',
                (tenant, str(uuid4()), '7' * 64,
                 describe_task)).fetchone()[0]
            descriptor_key = db.execute(
                'SELECT object_key FROM photo.artifacts WHERE id=%s',
                (descriptor,)).fetchone()[0]
            db.execute('INSERT INTO photo.output_uploads(tenant_id,task_id,'
                       'attempt,fence,worker_id,certificate_sha256,'
                       'idempotency_key,artifact_id,role,bucket,object_key,'
                       'sha256,bytes,media_type,state,object_version) VALUES('
                       "%s,%s,1,1,%s,%s,%s,%s,'document','test',%s,%s,64,"
                       "'application/json','ready','v1')",
                       (tenant, describe_task, uuid4(), '0' * 64, uuid4(),
                        descriptor, descriptor_key, '7' * 64))
            result = db.execute(
                'INSERT INTO photo.task_results(tenant_id,task_id,attempt,'
                'fencing_token,outputs,metrics,result_digest) '
                "VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",
                (tenant, describe_task, Jsonb([{'artifact_id': str(descriptor),
                  'sha256': '7' * 64}]), 'f' * 64)).fetchone()[0]
            db.execute("UPDATE photo.tasks SET state='succeeded',"
                       'canonical_result_id=%s WHERE id=%s',
                       (result, describe_task))
            approval_args = (tenant, first_version,
                             int(published['revision']), linked_document,
                             actor, auth_session, resolved_artifact,
                             '4' * 64, version, candidate, '8' * 64,
                             descriptor, '7' * 64, analysis, 0)
            with pytest.raises(errors.CheckViolation), db.transaction():
                db.execute('SET LOCAL ROLE photo_api')
                db.execute('SELECT photo.approve_v23_draft_with_sources('
                           + ','.join(['%s'] * 15) + ')',
                           (*approval_args[:12], '0' * 64,
                            *approval_args[13:]))
            assert db.execute('SELECT count(*) FROM photo.v2_document_approvals '
                              'WHERE document_id=%s',
                              (linked_document,)).fetchone()[0] == 0
            db.execute('SET LOCAL ROLE photo_api')
            approved = db.execute('SELECT photo.approve_v23_draft_with_sources('
                                  + ','.join(['%s'] * 15) + ')',
                                  approval_args).fetchone()[0]
            db.execute('RESET ROLE')
            assert approved['document_id'] == str(linked_document)
            assert set(db.execute(
                'SELECT artifact_id,sha256,ordinal FROM photo.v2_document_refs '
                'WHERE tenant_id=%s AND document_id=%s AND artifact_id IN (%s,%s)',
                (tenant, linked_document, candidate, descriptor)).fetchall()) == {
                    (candidate, '8' * 64, 0), (descriptor, '7' * 64, 0)}
            with pytest.raises(errors.SerializationFailure), db.transaction():
                db.execute('UPDATE photo.learning_profiles SET active_version_id=NULL '
                           'WHERE tenant_id=%s AND id=%s', (tenant, profile))
                db.execute('SELECT photo.approve_v23_draft('
                           + ','.join(['%s'] * 9) + ')',
                           (tenant, first_version,
                            int(published['revision']), linked_document,
                            actor, auth_session, resolved_artifact,
                            '4' * 64, version))
            db.execute('SET LOCAL ROLE photo_api')
            revoked = db.execute('SELECT photo.change_v2_consent('
                                 + ','.join(['%s'] * 10) + ')',
                                 (tenant, project, consent, 1,
                                  'learning_photographer', None, 'revoke',
                                  'v2.2', actor, auth_session)).fetchone()[0]
            db.execute('RESET ROLE')
            assert revoked['status'] == 'revoked'
            assert db.execute('SELECT active_version_id,revision '
                              'FROM photo.learning_profiles WHERE id=%s',
                              (profile,)).fetchone() == (None, 3)
            assert db.execute('SELECT status,reason,revision '
                              'FROM photo.version_eligibility WHERE version_id=%s',
                              (version,)).fetchone() == (
                                  'disabled', 'CONSENT_REVOKED', 3)
            queued = {row[0] for row in db.execute(
                'SELECT artifact_id FROM photo.consent_cleanup '
                'WHERE tenant_id=%s AND consent_id=%s AND state=%s',
                (tenant, consent, 'pending')).fetchall()}
            derived = set(db.execute(
                'SELECT training_artifact_id,model_artifact_id,'
                'evaluation_artifact_id FROM photo.learning_versions '
                'WHERE id=%s', (version,)).fetchone())
            assert queued == derived
            with pytest.raises(errors.SerializationFailure), db.transaction():
                db.execute('SELECT photo.approve_v23_draft('
                           + ','.join(['%s'] * 9) + ')',
                           (tenant, first_version,
                            int(published['revision']), linked_document,
                            actor, auth_session, resolved_artifact,
                            '4' * 64, version))
            rejected_document, rejected_artifact = uuid4(), _artifact(
                db, tenant, '3' * 64, role='document')
            db.execute('INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,'
                       'project_id,asset_version_id,artifact_id,sha256,bytes,'
                       'bucket,state,object_version,completed_at) VALUES('
                       "%s,%s,%s,%s,%s,%s,64,'test','ready','v1',now())",
                       (tenant, rejected_document, project, first_version,
                        rejected_artifact, '3' * 64))
            with pytest.raises(errors.SerializationFailure), db.transaction():
                db.execute('SET LOCAL ROLE photo_api')
                db.execute('SELECT photo.replace_v23_draft('
                           + ','.join(['%s'] * 18) + ')',
                           (tenant, first_version,
                            int(published['revision']), rejected_document,
                            rejected_artifact, '3' * 64, actor, auth_session,
                            Jsonb([]), None, None, None, Jsonb([]), None,
                            None, None, None, version))
            assert db.execute('SELECT count(*) FROM photo.model_use_links '
                              'WHERE document_id=%s',
                              (rejected_document,)).fetchone()[0] == 0
            raise RuntimeError('discard evaluation fixture')
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.renew_v23_learning_evaluate(%s,%s)',
                      (claim[0], uuid4())).fetchone()[0] is False
    assert db.execute('SELECT photo.renew_v23_learning_evaluate(%s,%s)',
                      (claim[0], claim[1])).fetchone()[0] is True
    assert db.execute('SELECT photo.release_v23_learning_evaluate(%s,%s)',
                      (claim[0], uuid4())).fetchone()[0] is False
    assert db.execute('SELECT photo.release_v23_learning_evaluate(%s,%s)',
                      (claim[0], claim[1])).fetchone()[0] is True
    assert db.execute('SELECT * FROM photo.reserve_v23_evaluate_document('
                      + ','.join(['%s'] * 7) + ')',
                      (tenant, run, claim[0], 'v23_evaluate_params', eval_sha,
                       eval_size, 'test-bucket')).fetchone()[0] == 'forbidden'
    db.execute('RESET ROLE')
    assert db.execute('SELECT expansion_token,expansion_lease_until '
                      'FROM photo.graph_expansions WHERE id=%s',
                      (claim[0],)).fetchone() == (None, None)
    assert db.execute('SELECT node_key,operation,state,params_artifact_id '
                      'FROM photo.tasks WHERE id=%s', (eval_task,)).fetchone() == (
                          'learning.evaluate:0', 'learning.evaluate',
                          'blocked', eval_id)
    assert db.execute('SELECT prerequisite_id,dependent_id,condition '
                      'FROM photo.task_edges WHERE run_id=%s',
                      (run,)).fetchall() == [(fit_task, eval_task, 'success')]
    for signature in ('photo.v23_learning_candidate_input(uuid,uuid)',
                      'photo.claim_v23_learning_evaluate()',
                      'photo.renew_v23_learning_evaluate(uuid,uuid)',
                      'photo.release_v23_learning_evaluate(uuid,uuid)',
                      'photo.close_v23_learning_evaluate(uuid,uuid,uuid,uuid,uuid)'):
        assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                          ('photo_api', signature, 'EXECUTE')).fetchone()[0] is False
    for signature in ('photo.reserve_v23_evaluate_document(uuid,uuid,uuid,text,text,bigint,text)',
                      'photo.complete_v23_evaluate_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)',
                      'photo.adopt_v23_learning_version(uuid,uuid,jsonb,jsonb,uuid,text)'):
        assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                          ('photo_api', signature, 'EXECUTE')).fetchone()[0] is False
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.finalize_v2_run(%s,%s)',
                      (tenant, run)).fetchone()[0] == 'pending'
    db.execute('RESET ROLE')
    db.execute('SET LOCAL ROLE photo_api')
    changed = db.execute('SELECT photo.change_v2_consent('
                         + ','.join(['%s'] * 10) + ')',
                         (tenant, project, consent, 1, 'learning_photographer',
                          None, 'revoke', 'v2.2', actor, auth_session)).fetchone()[0]
    assert changed['status'] == 'revoked' and changed['epoch'] == '2'
    db.execute('RESET ROLE')
    assert db.execute('SELECT cancel_requested FROM photo.runs WHERE id=%s',
                      (run,)).fetchone()[0] is True
