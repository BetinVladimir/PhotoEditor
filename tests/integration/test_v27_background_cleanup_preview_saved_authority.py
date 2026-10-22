"""Real SQL review authority and saved-draft identity, metadata fixtures only.

No authority function replacement. Does not cover resource quota, transport,
activation, physical documents or photographic qualification. All rows roll back.
"""
import hashlib
import json
from uuid import uuid4, uuid5

import psycopg
import pytest

from test_v27_removal_preview_sql_roles import database, isolated_database_config
from test_v27_semantic_final_producer_gate import fixture


def saved_authority_fixture(db, *, source_builder=None, metadata_bodies=False, protection_builder=None, transfer_params_builder=None, transfer_output_builder=None, snapshot_builder=None, calibration_body=None, assembly_output_builder=None,reference_builder=None,target_count=1,objects=None,kind="background_cleanup",qualification_policies=None):
    if kind not in ("background_cleanup", "remove_object", "local_exposure"):
        raise ValueError("unsupported saved authority fixture kind")
    tenant, assembly, result, _, _, candidate, scope = fixture(db, snapshot_authority=True,qualification_objects=objects,physical_closure=objects is not None,source_builder=source_builder,metadata_bodies=metadata_bodies,protection_builder=protection_builder,transfer_params_builder=transfer_params_builder,transfer_output_builder=transfer_output_builder,snapshot_builder=snapshot_builder,calibration_body=calibration_body,physical_assembly=assembly_output_builder is not None,assembly_output_builder=assembly_output_builder,reference_builder=reference_builder,target_count=target_count,qualification_policies=qualification_policies)
    workflow, parent = db.execute(
        'SELECT w.id,w.run_id FROM photo.v2_workflows w JOIN photo.tasks t ON t.tenant_id=w.tenant_id AND t.run_id=w.run_id WHERE t.id=%s',
        (assembly,)).fetchone()
    db.execute("UPDATE photo.runs SET status='waiting_review' WHERE id=%s", (parent,))
    db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',photo.v2_adoption_event_id(%s,%s),clock_timestamp())",
               (tenant,assembly,result))
    revision = db.execute('SELECT revision FROM photo.runs WHERE id=%s', (parent,)).fetchone()[0]
    intent_ref = scope.get('intent_ref',dict(artifact_id=str(scope['source']),sha256=scope['source_sha']))
    intent = db.execute('INSERT INTO photo.semantic_intents(tenant_id,project_id,workflow_id,reference_version_id,snapshot_id,intent_artifact_id,intent_sha) VALUES(%s,%s,%s,%s,%s,%s,%s) RETURNING id',
        (tenant,scope['project'],workflow,scope.get('reference_binding',{}).get('asset_version_id',scope['version']),scope['snapshot'],intent_ref['artifact_id'],intent_ref['sha256'])).fetchone()[0]
    target_rows=[]
    proposals=scope.get('target_producers') or [dict(scope=scope,candidate=candidate)]
    for proposal in proposals:
        row=db.execute("INSERT INTO photo.semantic_targets(tenant_id,project_id,intent_id,asset_version_id,candidate_artifact_id,state) VALUES(%s,%s,%s,%s,%s,'proposed') RETURNING id,revision",
            (tenant,scope['project'],intent,proposal['scope']['version'],proposal['candidate'])).fetchone()
        target_rows.append(dict(target=row[0],revision=row[1],version=proposal['scope']['version'],candidate=proposal['candidate']))
    target,target_revision=target_rows[0]['target'],target_rows[0]['revision']
    scope['review_targets']=target_rows
    sources = db.execute('SELECT contract,artifact_id,sha256 FROM photo.semantic_review_plan_sources(%s,%s,%s,%s)',
                         (tenant,workflow,scope['actor'],scope['session'])).fetchall()
    plan = next(row for row in sources if row[0] == 'SemanticPlan')
    candidate_sha = next(row[2] for row in sources if row[0] == 'TransferCandidate' and row[1] == candidate)
    current = (tenant,intent,target,scope['actor'],scope['session'],revision,target_revision,plan[2],candidate_sha)
    assert db.execute('SELECT photo.semantic_preapply_preview_current(%s,%s,%s,%s,%s,%s,%s,%s,%s)', current).fetchone()[0] is True
    # Minimal saved-intent boundary payload; not a compiled worker Params fixture.
    body = dict(schema_version=1,kind=kind,model_id='big_lama',
        tenant_id=str(tenant),intent_id=str(intent),target_id=str(target),workflow_id=str(workflow),
        snapshot_id=str(scope['snapshot']),workflow_revision=str(revision),target_revision=str(target_revision),
        source=dict(asset_version_id=str(scope['version'])),
        candidate=dict(artifact_id=str(candidate),sha256=candidate_sha),
        plan=dict(artifact_id=str(plan[1]),sha256=plan[2]),
        intent=intent_ref)
    if scope.get('source_body') is not None:
        from photo_worker.contracts.images import WorkingImage
        from photo_worker.contracts.base import canonical_hash
        source_image = WorkingImage.model_validate_json(scope['source_body'])
        body['source'].update(source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),
                              canvas_hash=canonical_hash(source_image.canvas))
    if kind == "local_exposure":
        # Exposure preview uses its own schema without removal model fields.
        body.pop('kind')
        body.pop('model_id')
    raw = json.dumps(body,sort_keys=True,separators=(',', ':'))
    digest = hashlib.sha256(raw.encode()).hexdigest()
    args = (tenant,intent,scope['actor'],scope['session'],target,raw,digest)
    return args, parent, scope, target


def save(db,args):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        value = db.execute('SELECT photo.save_semantic_background_cleanup_preview(%s,%s,%s,%s,%s,%s,%s)',args).fetchone()[0]
        db.execute('RESET ROLE')
        return value


def test_background_cleanup_real_authority_saves_one_replayable_draft(database):
    args, parent, _, target = saved_authority_fixture(database)
    first = save(database,args)
    assert first == uuid5(parent,'semantic-background_cleanup-preview:'+str(target)+':'+args[6]+':'+str(args[2])+':'+str(args[3]))
    assert save(database,args) == first
    row = database.execute('SELECT status,graph_sealed,cancel_requested FROM photo.runs WHERE id=%s', (first,)).fetchone()
    assert row == ('draft',False,False)
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s', (first,)).fetchone()[0] == 0


@pytest.mark.parametrize('mutation', ['session','target','parent'])
def test_background_cleanup_saved_draft_rechecks_real_authority(database,mutation):
    args, parent, scope, target = saved_authority_fixture(database)
    run = save(database,args)
    if mutation == 'session':
        database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (scope['session'],))
    elif mutation == 'target':
        # Reject through the legal proposed -> rejected transition; the state
        # trigger advances revision and preserves the frozen saved request.
        workflow = database.execute(
            'SELECT workflow_id FROM photo.semantic_intents WHERE id=%s',
            (args[1],)).fetchone()[0]
        batch = database.execute(
            "INSERT INTO photo.v2_review_batches(tenant_id,project_id,workflow_id,decisions_artifact_id,actor_id,action) VALUES(%s,%s,%s,%s,%s,'decide') RETURNING id",
            (args[0],scope['project'],workflow,scope['source'],scope['actor'])).fetchone()[0]
        previous = database.execute(
            'SELECT revision FROM photo.semantic_targets WHERE id=%s', (target,)).fetchone()[0]
        changed = database.execute(
            "UPDATE photo.semantic_targets SET state='rejected',batch_id=%s WHERE id=%s",
            (batch,target))
        assert changed.rowcount == 1
        assert database.execute(
            'SELECT state,revision,batch_id,child_run_id FROM photo.semantic_targets WHERE id=%s',
            (target,)).fetchone() == ('rejected',previous+1,batch,None)
    else:
        database.execute("UPDATE photo.runs SET cancel_requested=true WHERE id=%s", (parent,))
    with pytest.raises((psycopg.errors.InsufficientPrivilege,psycopg.errors.SerializationFailure)):
        save(database,args)
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        inspect(database,args,run)
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s', (run,)).fetchone()[0] == 0



def inspect(db, args, run):
    tenant, _, actor, session, _, raw, digest = args
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        rows = db.execute('SELECT * FROM photo.inspect_semantic_background_cleanup_preview(%s,%s,%s,%s,%s,%s)',
                          (tenant,run,actor,session,raw,digest)).fetchall()
        db.execute('RESET ROLE')
        return rows


def test_background_cleanup_inspection_preserves_real_empty_draft_identity(database):
    args, _, _, _ = saved_authority_fixture(database)
    run = save(database,args)
    assert inspect(database,args,run) == [('draft','',False,False)]
    assert save(database,args) == run
    assert inspect(database,args,run) == [('draft','',False,False)]


@pytest.mark.parametrize('mutation', ['tenant','actor','session','params','missing_run'])
def test_background_cleanup_inspection_never_reports_inaccessible_as_empty_draft(database,mutation):
    args, _, _, _ = saved_authority_fixture(database)
    run = save(database,args)
    changed = list(args)
    if mutation == 'tenant':
        changed[0] = uuid4()
    elif mutation == 'actor':
        changed[2] = uuid4()
    elif mutation == 'session':
        changed[3] = uuid4()
    elif mutation == 'params':
        body = json.loads(changed[5])
        body['model_id'] = 'foreign_model'
        changed[5] = json.dumps(body,sort_keys=True,separators=(',', ':'))
        changed[6] = hashlib.sha256(changed[5].encode()).hexdigest()
    else:
        run = uuid4()
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        inspect(database,changed,run)
    # The original accessible identity remains recoverable.
    assert inspect(database,args,save(database,args)) == [('draft','',False,False)]


@pytest.mark.parametrize('with_alpha', [False,True])
def test_background_cleanup_saved_authority_with_schema_valid_source(database,with_alpha):
    from background_cleanup_preview_documents_fixture import working_image_builder
    args, _, scope, _ = saved_authority_fixture(database,
        source_builder=working_image_builder(with_alpha=with_alpha),metadata_bodies=True)
    run = save(database,args)
    assert inspect(database,args,run) == [('draft','',False,False)]
    source = json.loads(args[5])['source']
    assert source['source']['sha256'] == hashlib.sha256(scope['source_body']).hexdigest()
    assert len(scope['source_inputs']) == 1+int(with_alpha)



def test_background_cleanup_saved_source_has_canonical_protection_producer(database):
    from background_cleanup_preview_documents_fixture import working_image_builder, protection_builder
    args, _, scope, _ = saved_authority_fixture(database,
        source_builder=working_image_builder(),metadata_bodies=True,
        protection_builder=protection_builder(database))
    run = save(database,args)
    assert inspect(database,args,run) == [('draft','',False,False)]
    doc = scope['protection_documents']['target_protection']
    assert hashlib.sha256(doc['body']).hexdigest() == doc['ref']['sha256']
    assert hashlib.sha256(doc['binary']).hexdigest() == doc['binary_ref']['sha256']
    origin_args = (args[0],scope['project'],scope['source'],scope['source_sha'],
                   doc['ref']['artifact_id'],doc['ref']['sha256'],
                   doc['binary_ref']['artifact_id'],doc['binary_ref']['sha256'])
    def ready():
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_fn_owner')
            value = database.execute('SELECT photo.semantic_protection_producer_ready(%s,%s,%s,%s,%s,%s,%s,%s)',origin_args).fetchone()[0]
            database.execute('RESET ROLE')
            return value
    assert ready() is True
    with database.transaction(force_rollback=True):
        database.execute('UPDATE photo.tasks SET fencing_token=fencing_token+1 WHERE id=%s',(scope['protection_producer'],))
        assert ready() is False



def test_background_cleanup_saved_authority_uses_own_edit_intent(database):
    from background_cleanup_preview_documents_fixture import working_image_builder, protection_builder, transfer_params_builder
    args, _, scope, _ = saved_authority_fixture(database,source_builder=working_image_builder(),
        metadata_bodies=True,protection_builder=protection_builder(database),transfer_params_builder=transfer_params_builder)
    run = save(database,args)
    assert inspect(database,args,run) == [('draft','',False,False)]
    assert json.loads(args[5])['intent'] == scope['intent_ref']
    assert json.loads(scope['intent_body'])['kind'] == 'background_cleanup'
    assert hashlib.sha256(scope['intent_body']).hexdigest() == scope['intent_ref']['sha256']



def test_background_cleanup_candidate_children_have_real_canonical_provenance(database):
    from background_cleanup_preview_documents_fixture import working_image_builder, protection_builder, transfer_params_builder, candidate_builder
    args, _, scope, target = saved_authority_fixture(database,source_builder=working_image_builder(),
        metadata_bodies=True,protection_builder=protection_builder(database),transfer_params_builder=transfer_params_builder,
        transfer_output_builder=candidate_builder(database))
    body = json.loads(args[5])
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_api')
        for kind in ('mask','transform'):
            rows = database.execute('SELECT contract,artifact_id,sha256 FROM photo.semantic_approved_candidate_child(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                (*args[:4],int(body['workflow_revision']),target,int(body['target_revision']),scope['version'],
                 body['candidate']['artifact_id'],body['candidate']['sha256'],scope['candidate_body'].decode(),kind)).fetchall()
            expected = scope['candidate_'+kind+'_ref']
            assert len(rows) == 1 and str(rows[0][1]) == expected['artifact_id'] and rows[0][2] == expected['sha256']
        database.execute('RESET ROLE')


@pytest.mark.parametrize('field', ['tenant', 'intent', 'actor', 'session', 'target', 'snapshot'])
def test_background_cleanup_save_rejects_foreign_scope_without_creating_another_run(database, field):
    args, _, _, _ = saved_authority_fixture(database)
    first = save(database, args)
    before = database.execute('SELECT count(*) FROM photo.runs').fetchone()[0]
    changed = list(args)
    if field == 'snapshot':
        body = json.loads(changed[5])
        body['snapshot_id'] = str(uuid4())
        changed[5] = json.dumps(body, sort_keys=True, separators=(',', ':'))
        changed[6] = hashlib.sha256(changed[5].encode()).hexdigest()
    else:
        changed[{'tenant':0, 'intent':1, 'actor':2, 'session':3, 'target':4}[field]] = uuid4()
    with pytest.raises((psycopg.errors.InsufficientPrivilege,
            psycopg.errors.SerializationFailure, psycopg.errors.InvalidParameterValue)):
        save(database, tuple(changed))
    assert database.execute('SELECT count(*) FROM photo.runs').fetchone()[0] == before
    assert save(database, args) == first
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s', (first,)).fetchone()[0] == 0
