"""Work-only SQL producer builder with actual Go domain closure compiler hooks.
Controlled relational canonical receipts, not physical/model qualification.
"""
from hashlib import sha256
from uuid import UUID,uuid4,uuid5
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from photo_worker.operations.learning.contracts import TrainingManifest
from test_consent_authorized_training_withdrawal import (_fixture, _artifact,
 _jcs_simple, _synthetic_finished_attempt, _check_constructed_receipt_constraints)
from detail_material import MaterialCatalog
from portrait_detail_producer_fixture import detail_producer,bind_current_event,freeze_linked_detail

def detail_training(db, *, qualification, compiler, base=None, before_detail_bind=None):
    detail_documents = []
    base = _fixture(db, qualification=qualification) if base is None else base
    tenant, actor, auth_session, project, request, consent = base
    material=MaterialCatalog(db,tenant)
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING', (tenant,))
    collection = db.execute('SELECT collection_id FROM photo.shoot_sessions '
                            'WHERE id=%s', (request['session_id'],)).fetchone()[0]
    sessions = [request['session_id']]
    for index in range(5):
        sessions.append(str(db.execute(
            'INSERT INTO photo.shoot_sessions(tenant_id,project_id,collection_id,'
            "name,state,policy,consent_epoch) VALUES(%s,%s,%s,%s,'open','{}',1) "
            'RETURNING id', (tenant, project, collection, f'Session {index}')).fetchone()[0]))
    # Production FreezeTrainingSplit/sessionOrder selects the first two of six
    # sessions by SHA256("2026:" + canonical UUID), with UUID bytes as tie-break.
    # Keep four train slots then two holdout slots so detail index4 is an actual
    # selected holdout, not merely a fixture-labelled one.
    ordered_sessions=sorted(sessions,key=lambda value:(
        sha256(('2026:'+str(UUID(str(value)))).encode()).digest(),UUID(str(value)).bytes))
    sessions=ordered_sessions[2:]+ordered_sessions[:2]
    assert len(set(map(str,sessions)))==6
    base_artifact, approved_artifact, resolved_artifact = db.execute(
        'SELECT base.artifact_id,approved.artifact_id,approval.resolved_artifact_id '
        'FROM photo.v2_documents base JOIN photo.v2_documents approved '
        'ON approved.tenant_id=base.tenant_id '
        'JOIN photo.v2_document_approvals approval '
        'ON approval.tenant_id=approved.tenant_id '
        'AND approval.document_id=approved.id '
        'WHERE base.id=%s AND approved.id=%s',
        (request['base_document_id'], request['approved_document_id'])).fetchone()
    resolved_artifact=UUID(material.style_ref['artifact_id'])
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
        source_ref=material.source(index)
        original,source_sha=UUID(source_ref['artifact_id']),source_ref['sha256']
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
        binding={'asset_version_id':str(version),'source':source_ref,'canvas_hash':material.images[source_ref['artifact_id']]['canvas_hash']}
        from photo_worker.v2.render_recipe import EditingRecipeV3
        from photo_worker.contracts.base import canonical_bytes
        authored=EditingRecipeV3.model_validate_json(canonical_bytes(dict(schema_version=3,source=binding,
            base_style=material.style_ref,session_corrections=None,learning_version_id=None,overrides=[],layer_stack=None,
            renderer_version='v2.2',parameter_delta={key:0 for key in ('ev','log_r','log_g','log_b','saturation_delta','retouch_delta')})))
        base_ref=material.publish(authored);approved_ref=material.publish(authored)
        base_artifact,approved_artifact=UUID(base_ref['artifact_id']),UUID(approved_ref['artifact_id'])
        base_doc, approved_doc = uuid4(), uuid4()
        for doc, number, artifact, digest in (
            (base_doc, 1, base_artifact, base_ref['sha256']),
            (approved_doc, 2, approved_artifact, approved_ref['sha256']),
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
            (tenant, project, approved_doc, actor, approved_ref['sha256'],
             resolved_artifact)).fetchone()[0]
        producer = None
        if index == 4:
            item = db.execute('SELECT id FROM photo.collection_items WHERE tenant_id=%s AND collection_id=%s AND asset_version_id=%s', (tenant, collection, version)).fetchone()[0]
            producer = detail_producer(db, tenant, project, actor, version, collection, item, material)
        event = db.execute(
            'INSERT INTO photo.correction_events(tenant_id,project_id,session_id,'
            'asset_version_id,base_document_id,approved_document_id,changed_paths,'
            'reason,learn,consent_id,consent_epoch,approved_by,client_event_id) '
            "VALUES(%s,%s,%s,%s,%s,%s,%s,'exposure',true,%s,2,%s,%s) RETURNING id",
            (tenant, project, sessions[index % 6], version, base_doc,
             approved_doc, Jsonb(['/ev']), consent, actor, uuid4())).fetchone()[0]
        if producer is not None:
            if before_detail_bind is not None:
                event=before_detail_bind(db,tenant,project,actor,auth_session,event,producer)
            from detail_producer_checks import check_current_producer
            check_current_producer(db,tenant,project,actor,auth_session,event,producer)
            bind_current_event(db, tenant, project, actor, auth_session, event, producer)
        ref = lambda aid, digest: {'artifact_id': str(aid), 'sha256': digest}
        source_ref = ref(original, source_sha)
        binding,faces_ref,descriptor_ref=material.case(version,source_ref,index)
        protected_ref=material.images[source_ref['artifact_id']]['protected']
        frozen={'schema_version':1,'source':{'asset_version_id':str(version),'source':source_ref,'canvas_hash':binding['canvas_hash']},'base_style':material.style_ref,'effective_delta':{key:0 for key in ('ev','log_r','log_g','log_b','saturation_delta','retouch_delta')},'renderer_version':'v2.2','icc_lock_sha':'f'*64,'model_lock_sha':'0'*64}
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
                            'source': source_ref, 'canvas_hash': binding['canvas_hash']},
                'working_image': source_ref,
                'base_recipe': frozen_refs['base'],
                'approved_recipe': frozen_refs['approved'],
                'base_document': base_ref,
                'approved_document': approved_ref,
                'descriptor_shard': descriptor_ref,
                'descriptor_analysis_id': str(uuid4()),
                'descriptor_ordinal': index,
                'protected': protected_ref,
                'faces': faces_ref,
                'approval_id': str(approval),
                'source_lineage_sha': source_sha}})
        if producer is not None:
            raw = freeze_linked_detail(db, tenant, project, actor, auth_session, event, producer, protected_ref)
            detail_documents.append((event, raw))
            examples[-1]['render_case']['detail_evidence'] = ref(uuid5(run, 'v23-detail/'+str(event)), sha256(raw).hexdigest())
    zero_body=_jcs_simple({'schema_version':1,'profile_id':str(profile),'training_sha':'0'*64,'feature_order':'scene12_v1','means':[0]*12,'scales':[1]*12,'coefficients':[[0]*6 for _ in range(12)],'intercept':[0]*6,'ridge_lambda':1})
    zero_sha=sha256(zero_body).hexdigest()
    from photo_worker.operations.session.contracts import Calibration
    calibration=Calibration.model_validate_json(_jcs_simple(dict(schema_version=1,model_lock_sha='0'*64,
        dataset_sha=sha256(_jcs_simple([item['render_case']['binding'] for item in examples])).hexdigest(),
        qualified=False,scene_cosine_min=.7,scene_log_luma_max=1.,cluster_diameter_max=.25,
        sharpness_scale=1.,clipping_penalty=1.,match_inlier_min=.5,match_error_px_max=2.,
        identity_similarity_min=.8,landmark_drift_max=.1,normalization_scales=[1.]*6,
        loss_weights=[1/6]*6,focus_warning=.3,focus_critical=.1,eye_open_min=.5,
        glare_warning_fraction=.1,clipping_warning_fraction=.1,clipping_critical_fraction=.3,
        min_face_edge_px=32,depth_sigma=.1,registration_error_px=1.,motion_threshold=.1)))
    calibration_ref=material.publish(calibration)

    policy_body=_jcs_simple(dict(schema_version=1,mode='photographer',min_examples=40,min_sessions=6,holdout_fraction=.2,seed=2026,calibration=calibration_ref))
    policy_sha=sha256(policy_body).hexdigest()
    manifest = {'schema_version': 1, 'profile_id': str(profile),
                'mode': 'photographer', 'person_id': None,
                'consents': [{'consent_id': str(consent), 'epoch': '2'}],
                'examples': examples, 'policy_sha': policy_sha,
                'baseline': {'artifact_id': str(uuid5(run,'v23-zero-profile/'+str(profile))), 'sha256': zero_sha},
                'renderer_version': 'v2.2', 'icc_lock_sha': 'f' * 64,
                'model_lock_sha': '0' * 64,
                'calibration': calibration_ref}
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
    recovery={'schema_version':1,'manifest_jcs':_jcs_simple(manifest).decode(),'manifest_sha':manifest_sha,'snapshot_jcs':_jcs_simple(root).decode(),'snapshot_body_sha':root_sha,'snapshot_digest':snapshot_digest,'policy_jcs':policy_body.decode(),'policy_sha':policy_sha,'reserved_bytes':'4096','expected_revision':'1','profile_id':str(profile),'project_id':str(project),'frozen_jcs':_jcs_simple([{'event_id':str(event),'base_jcs':body.decode(),'approved_jcs':body.decode()} for event,body in frozen_documents]).decode()}
    assert len(detail_documents) == 1
    recovery['detail_jcs'] = _jcs_simple([{'event_id':str(event),'body_jcs':raw.decode()} for event,raw in detail_documents]).decode()
    db.execute('RESET ROLE');db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.save_v23_training_recovery('+','.join(['%s']*7)+')',(tenant,actor,auth_session,run,key,'2'*64,Jsonb(recovery))).fetchone()[0]
    for event,raw in detail_documents:
        digest=sha256(raw).hexdigest()
        state,aid,token,ready=db.execute('SELECT * FROM photo.reserve_v23_detail_document('+','.join(['%s']*9)+')',(tenant,run,event,project,actor,auth_session,digest,len(raw),'test-bucket')).fetchone()
        assert state=='created' and aid==uuid5(run,'v23-detail/'+str(event)) and ready is False
        assert db.execute('SELECT photo.complete_v23_detail_document('+','.join(['%s']*12)+')',(tenant,run,event,project,actor,auth_session,aid,token,digest,len(raw),'test-bucket','v1')).fetchone()==(True,)
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
    fit_compiled=compiler.compile('fit',dict(tenant=tenant,actor=actor,session=auth_session,project=project,profile=profile,run=run,workflow=workflow,snapshot=snapshot,manifest=manifest,manifest_sha=manifest_sha,policy_sha=policy_sha,recovery=recovery,policy=params['policy'],material=material,frozen=frozen_documents,detail=detail_documents,params_id=params_id,params_sha=params_sha))
    args = (tenant, run, fit_task, params_id, params_sha,fit_compiled['input_digest'],Jsonb(fit_compiled['profile']))
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
    compiler.register(db, fit_compiled)
    fit_worker = _synthetic_finished_attempt(db, tenant, fit_task)
    from photo_worker.operations.learning.contracts import CandidateProfile
    candidate_model=CandidateProfile.model_validate_json(_jcs_simple(dict(schema_version=1,profile_id=str(profile),
        training_sha=manifest_sha,feature_order='scene12_v1',means=[0]*12,scales=[1]*12,
        coefficients=[[0]*6 for _ in range(12)],intercept=[0]*6,ridge_lambda=1.0)))
    candidate_body=_jcs_simple(candidate_model.model_dump(mode='json'))
    candidate_sha=sha256(candidate_body).hexdigest()
    candidate = db.execute(
        'INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,'
        'sha256,bytes,media_type,output_role,verified_at,created_by_task_id,'
        "produced_attempt,produced_fence) VALUES(%s,'test',%s,'v1',%s,%s,"
        "'application/json','document',now(),%s,1,1) RETURNING id",
        (tenant, str(uuid4()), candidate_sha, len(candidate_body), fit_task)).fetchone()[0]
    material.bodies[str(candidate)]=candidate_body
    candidate_key = db.execute('SELECT object_key FROM photo.artifacts WHERE id=%s',
                               (candidate,)).fetchone()[0]
    db.execute(
        'INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,'
        'worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,'
        'object_key,sha256,bytes,media_type,state,object_version,completed_at) VALUES'
        "(%s,%s,1,1,%s,%s,%s,%s,'document','test',%s,%s,%s,"
        "'application/json','ready','v1',clock_timestamp())",
        (tenant, fit_task, fit_worker, candidate_sha, uuid4(), candidate,
         candidate_key, candidate_sha, len(candidate_body)))
    result = db.execute(
        'INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,'
        "outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) "
        'RETURNING id', (tenant, fit_task, Jsonb([{
            'artifact_id': str(candidate), 'sha256': candidate_sha}]),
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
                      (tenant, run)).fetchone() == (fit_task, candidate, candidate_sha)
    claim = db.execute('SELECT * FROM photo.claim_v23_learning_evaluate()').fetchone()
    assert claim is not None and claim[3:] == (
        tenant, run, workflow, candidate, candidate_sha)
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
                   'candidate': {'artifact_id': str(candidate), 'sha256': candidate_sha},
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
    eval_compiled=compiler.compile('evaluate',dict(tenant=tenant,actor=actor,session=auth_session,project=project,profile=profile,run=run,workflow=workflow,snapshot=snapshot,manifest=manifest,manifest_sha=manifest_sha,policy_sha=policy_sha,recovery=recovery,policy=params['policy'],material=material,frozen=frozen_documents,detail=detail_documents,params_id=eval_id,params_sha=eval_sha,claim=claim))
    stage_args = (tenant, run, claim[0], claim[1], eval_task,
                  eval_id, eval_sha, candidate, candidate_sha,
                  eval_compiled['input_digest'], Jsonb(eval_compiled['profile']))
    assert db.execute(stage_eval, (*stage_args[:8], '6' * 64,
                                   *stage_args[9:])).fetchone()[0] is False
    assert db.execute(stage_eval, stage_args).fetchone()[0] is True
    assert db.execute(stage_eval, stage_args).fetchone()[0] is True
    assert db.execute('SELECT photo.close_v23_learning_evaluate_guarded(%s,%s,%s,%s,%s)',
                      (tenant, run, claim[0], claim[1], eval_task)).fetchone()[0] is False
    assert db.execute('SELECT photo.close_v23_learning_evaluate_guarded(%s,%s,%s,%s,%s)',
                      (tenant, run, claim[0], uuid4(), eval_task)).fetchone()[0] is False
    db.execute('RESET ROLE')
    compiler.register(db, eval_compiled)
    return (tenant, run, claim[0], claim[1], eval_task)
