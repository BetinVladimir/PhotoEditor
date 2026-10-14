import pytest


def test_review_storage_is_tenant_scoped_and_function_owned(db):
    for name in ('semantic_intents','semantic_targets'):
        assert db.execute("SELECT relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid=%s::regclass",('photo.'+name,)).fetchone()==(True,True)
        for role in ('photo_api','photo_worker','photo_scheduler','photo_portal'):
            assert not db.execute('SELECT has_table_privilege(%s,%s,\'INSERT,UPDATE,DELETE\')',(role,'photo.'+name)).fetchone()[0]
        for privilege in ('SELECT','INSERT','UPDATE','DELETE'):
            assert db.execute('SELECT has_table_privilege(\'photo_fn_owner\',%s,%s)',('photo.'+name,privilege)).fetchone()[0]
    body=db.execute("SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure)").fetchone()[0]
    assert "'semantic_targets'" in body and "'semantic_intents'" in body
    assert body.index("'semantic_targets'")<body.index("'semantic_intents'")<body.index("'v2_workflows'")


def test_semantic_review_bindings_and_state_transitions(db):
    from uuid import uuid4
    import psycopg
    from test_v27_semantic_final_producer_gate import fixture
    tid,assembly,_,_,_,candidate,scope=fixture(db,snapshot_authority=True)
    workflow,project,snapshot=db.execute('SELECT w.id,w.project_id,w.input_snapshot_id FROM photo.v2_workflows w JOIN photo.tasks t ON t.tenant_id=w.tenant_id AND t.run_id=w.run_id WHERE t.id=%s',(assembly,)).fetchone()
    version=db.execute('SELECT asset_version_id FROM photo.snapshot_items WHERE tenant_id=%s AND snapshot_id=%s',(tid,snapshot)).fetchone()[0]
    intent=db.execute("INSERT INTO photo.semantic_intents(tenant_id,project_id,workflow_id,reference_version_id,snapshot_id,intent_artifact_id,intent_sha) VALUES(%s,%s,%s,%s,%s,%s,%s) RETURNING id",(tid,project,workflow,version,snapshot,scope['source'],'5'*64)).fetchone()[0]
    target=db.execute("INSERT INTO photo.semantic_targets(tenant_id,project_id,intent_id,asset_version_id,candidate_artifact_id,state) VALUES(%s,%s,%s,%s,%s,'proposed') RETURNING id",(tid,project,intent,version,candidate)).fetchone()[0]
    for query,args in [
        ('UPDATE photo.semantic_intents SET intent_sha=%s WHERE id=%s',('0'*64,intent)),
        ('UPDATE photo.semantic_targets SET candidate_artifact_id=%s WHERE id=%s',(uuid4(),target)),
        ("UPDATE photo.semantic_targets SET state='applied' WHERE id=%s",(target,)),
    ]:
        with db.transaction(force_rollback=True):
            with __import__('pytest').raises(psycopg.errors.ObjectNotInPrerequisiteState):db.execute(query,args)
    with db.transaction(force_rollback=True):
        with __import__('pytest').raises(psycopg.errors.CheckViolation):db.execute("UPDATE photo.semantic_targets SET state='queued' WHERE id=%s",(target,))
    # Real relational batch fixture; byte-qualified decision/admission is separate.
    batch=db.execute("INSERT INTO photo.v2_review_batches(tenant_id,project_id,workflow_id,decisions_artifact_id,actor_id,action) VALUES(%s,%s,%s,%s,%s,'decide') RETURNING id",(tid,project,workflow,scope['source'],scope['actor'])).fetchone()[0]
    run=db.execute('SELECT run_id FROM photo.tasks WHERE id=%s',(assembly,)).fetchone()[0]
    def review(candidate_id):
        return db.execute("INSERT INTO photo.reviews(tenant_id,run_id,candidate_artifact_id,candidate_hash,kind,reasons) VALUES(%s,%s,%s,%s,'semantic','[]') RETURNING id",(tid,run,candidate_id,'5'*64)).fetchone()[0]
    wrong_review=review(scope['source'])
    correct_review=review(candidate)
    other_tid,other_assembly,_,_,_,other_candidate,other_scope=fixture(db,snapshot_authority=True,qualification=False)
    other_run=db.execute('SELECT run_id FROM photo.tasks WHERE id=%s',(other_assembly,)).fetchone()[0]
    foreign_review=db.execute("INSERT INTO photo.reviews(tenant_id,run_id,candidate_artifact_id,candidate_hash,kind,reasons) VALUES(%s,%s,%s,%s,'semantic','[]') RETURNING id",(other_tid,other_run,other_candidate,'5'*64)).fetchone()[0]
    wrong_version=db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state,version_no) VALUES(%s,%s,%s,%s,'{}','accepted',2) RETURNING id",(tid,scope['asset'],scope['source'],'5'*64)).fetchone()[0]
    def override(owner,asset_version,actor):
        return db.execute("INSERT INTO photo.overrides(tenant_id,asset_version_id,input_sha256,revision,config,config_hash,created_by) VALUES(%s,%s,%s,1,'{}',%s,%s) RETURNING id",(owner,asset_version,'5'*64,'6'*64,actor)).fetchone()[0]
    wrong_override=override(tid,wrong_version,scope['actor'])
    foreign_override=override(other_tid,other_scope['version'],other_scope['actor'])
    correct_override=override(tid,version,scope['actor'])
    # A producer-rejected candidate is already resolved before human batching.
    rejected_seed=db.execute("INSERT INTO photo.semantic_targets(tenant_id,project_id,intent_id,asset_version_id,candidate_artifact_id,state) VALUES(%s,%s,%s,%s,%s,'rejected') RETURNING id",(tid,project,intent,wrong_version,candidate)).fetchone()[0]
    assert db.execute('SELECT state,revision,batch_id,child_run_id FROM photo.semantic_targets WHERE id=%s',(rejected_seed,)).fetchone()==('rejected',1,None,None)
    with db.transaction(force_rollback=True):
        with pytest.raises(psycopg.errors.ObjectNotInPrerequisiteState):
            db.execute("UPDATE photo.semantic_targets SET state='proposed' WHERE id=%s",(rejected_seed,))
    # Force deferred source checks before rolling back the attempted transition.
    for column,value,constraint in (
        ('review_id',wrong_review,'semantic_target_review_source_fk'),
        ('review_id',foreign_review,'semantic_target_review_source_fk'),
        ('override_id',wrong_override,'semantic_target_override_source_fk'),
        ('override_id',foreign_override,'semantic_target_override_source_fk'),
    ):
        with db.transaction(force_rollback=True):
            with __import__('pytest').raises(psycopg.errors.ForeignKeyViolation):
                # Identifiers come exclusively from the static allowlist above.
                db.execute(f"UPDATE photo.semantic_targets SET state='rejected',batch_id=%s,{column}=%s WHERE id=%s",(batch,value,target))
                db.execute(f'SET CONSTRAINTS photo.{constraint} IMMEDIATE')
    with db.transaction(force_rollback=True):
        with __import__('pytest').raises(psycopg.errors.CheckViolation):
            db.execute("UPDATE photo.semantic_targets SET state='rejected',batch_id=%s WHERE id=%s",(uuid4(),target))
    db.execute("UPDATE photo.semantic_targets SET state='rejected',batch_id=%s,review_id=%s,override_id=%s WHERE id=%s",(batch,correct_review,correct_override,target))
    db.execute('SET CONSTRAINTS photo.semantic_target_review_source_fk,photo.semantic_target_override_source_fk IMMEDIATE')
    assert db.execute('SELECT state,revision,child_run_id FROM photo.semantic_targets WHERE id=%s',(target,)).fetchone()==('rejected',2,None)
    with db.transaction(force_rollback=True):
        with __import__('pytest').raises(psycopg.errors.ObjectNotInPrerequisiteState):db.execute("UPDATE photo.semantic_targets SET state='proposed' WHERE id=%s",(target,))


def test_review_source_links_use_tenant_and_asset_version(db):
    for name in ('semantic_target_review_source_fk','semantic_target_override_source_fk'):
        definition=db.execute('SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname=%s',(name,)).fetchone()[0]
        assert ('FOREIGN KEY (tenant_id, candidate_artifact_id,' if 'review_source' in name else 'FOREIGN KEY (tenant_id, asset_version_id,') in definition
        assert 'DEFERRABLE INITIALLY DEFERRED' in definition


@pytest.mark.parametrize('child_status,target_state,parent_status',[
    ('completed','applied','waiting_review'),
    ('failed','failed','completed'),
    ('cancelled','cancelled','cancelled'),
])
def test_child_settlement_is_scoped_replayable_and_parent_independent(db,child_status,target_state,parent_status):
    from uuid import uuid4
    from psycopg.types.json import Jsonb
    from test_v27_semantic_final_producer_gate import fixture
    tid,assembly,_,_,_,candidate,scope=fixture(db,snapshot_authority=True)
    parent=db.execute('SELECT run_id FROM photo.tasks WHERE id=%s',(assembly,)).fetchone()[0]
    workflow=db.execute('SELECT id FROM photo.v2_workflows WHERE tenant_id=%s AND run_id=%s',(tid,parent)).fetchone()[0]
    intent=db.execute("INSERT INTO photo.semantic_intents(tenant_id,project_id,workflow_id,reference_version_id,snapshot_id,intent_artifact_id,intent_sha) VALUES(%s,%s,%s,%s,%s,%s,%s) RETURNING id",(tid,scope['project'],workflow,scope['version'],scope['snapshot'],scope['source'],'5'*64)).fetchone()[0]
    target=db.execute("INSERT INTO photo.semantic_targets(tenant_id,project_id,intent_id,asset_version_id,candidate_artifact_id,state) VALUES(%s,%s,%s,%s,%s,'proposed') RETURNING id",(tid,scope['project'],intent,scope['version'],candidate)).fetchone()[0]
    child=uuid4()
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'apply','running',%s,%s,%s,%s,'fail_fast')",(child,tid,scope['snapshot'],Jsonb({'project_id':str(scope['project'])}),'b'*64,scope['actor']))
    batch=db.execute("INSERT INTO photo.v2_review_batches(tenant_id,project_id,workflow_id,decisions_artifact_id,actor_id,action,child_run_id) VALUES(%s,%s,%s,%s,%s,'apply',%s) RETURNING id",(tid,scope['project'],workflow,scope['source'],scope['actor'],child)).fetchone()[0]
    db.execute("UPDATE photo.semantic_targets SET state='queued',batch_id=%s,child_run_id=%s WHERE id=%s",(batch,child,target))
    db.execute('UPDATE photo.runs SET status=%s WHERE id=%s',(parent_status,parent))
    before=db.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(parent,)).fetchone()
    assert db.execute('SELECT photo.settle_semantic_child(%s,%s)',(tid,child)).fetchone()[0]==0
    import psycopg
    with db.transaction(force_rollback=True):
        with pytest.raises(psycopg.errors.CheckViolation):
            db.execute('UPDATE photo.semantic_targets SET state=%s WHERE id=%s',(target_state,target))
    db.execute('UPDATE photo.runs SET status=%s WHERE id=%s',(child_status,child))
    assert db.execute('SELECT photo.settle_semantic_child(%s,%s)',(uuid4(),child)).fetchone()[0]==0
    assert db.execute('SELECT photo.settle_semantic_child(%s,%s)',(tid,uuid4())).fetchone()[0]==0
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.settle_next_semantic_child()').fetchone()[0]==1
    assert db.execute('SELECT photo.settle_next_semantic_child()').fetchone()[0]==0
    assert db.execute('SELECT photo.settle_semantic_child(%s,%s)',(tid,child)).fetchone()[0]==0
    db.execute('RESET ROLE')
    assert db.execute('SELECT state,revision,batch_id,child_run_id FROM photo.semantic_targets WHERE id=%s',(target,)).fetchone()==(target_state,3,batch,child)
    assert db.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(parent,)).fetchone()==before
    for role in ('photo_api','photo_worker','photo_portal'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.settle_semantic_child(uuid,uuid)','EXECUTE')",(role,)).fetchone()[0]
