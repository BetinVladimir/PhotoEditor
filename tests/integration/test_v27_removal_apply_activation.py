"""Own activation SQL scenarios; synthetic object metadata, not physical proof."""
import pytest
import psycopg
from test_v27_removal_preview_sql_roles import database, isolated_database_config
from removal_preview_activation_fixture import published_transport_fixture, commit_preview


@pytest.mark.parametrize('with_alpha', [False, True])
def test_removal_activation_replays_exact_task_and_inputs(database, with_alpha):
    args, run, scope = published_transport_fixture(database, with_alpha=with_alpha)
    task = commit_preview(database, args, run, scope)
    assert str(task) == scope['preview_node']['id']
    before = database.execute('SELECT state,params_artifact_id,input_digest,inputs,execution_profile FROM photo.tasks WHERE id=%s',
        (task,)).fetchone()
    assert before[0] == 'ready'
    assert before[3] == [scope['preview_transport']['manifest'], *scope['preview_transport']['direct_roots']]
    assert commit_preview(database, args, run, scope) == task
    assert database.execute('SELECT state,params_artifact_id,input_digest,inputs,execution_profile FROM photo.tasks WHERE id=%s',
        (task,)).fetchone() == before
    assert database.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE task_id=%s',
        (task,)).fetchone()[0] == len(scope['preview_inventory']['entries'])+2
    assert database.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',
        (args[4],)).fetchone() == ('proposed', None, None)


@pytest.mark.parametrize('mutation', ['unverified_source', 'expired_quota'])
def test_removal_activation_failure_leaves_no_partial_task(database, mutation):
    args, run, scope = published_transport_fixture(database)
    if mutation == 'unverified_source':
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s', (scope['source'],))
    else:
        database.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s", (args[0], run))
    before = database.execute('SELECT status,graph_sealed,revision FROM photo.runs WHERE id=%s', (run,)).fetchone()
    with pytest.raises(psycopg.Error):commit_preview(database, args, run, scope)
    assert database.execute('SELECT status,graph_sealed,revision FROM photo.runs WHERE id=%s', (run,)).fetchone() == before
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s', (run,)).fetchone()[0] == 0
    assert database.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE task_id=%s', (scope['preview_node']['id'],)).fetchone()[0] == 0


@pytest.mark.parametrize('stage',['task_ready','run_queued'])
def test_removal_late_activation_failure_rolls_back_complete_tenant_state(database,stage):
    from psycopg import sql
    from removal_transaction_state import tenant_transaction_state
    args,run,scope=published_transport_fixture(database)
    task=scope['preview_node']['id']
    with database.transaction(force_rollback=True):
        database.execute("""CREATE FUNCTION photo.test_removal_activation_failure() RETURNS trigger
            LANGUAGE plpgsql AS $$ BEGIN
            IF TG_TABLE_NAME='tasks' AND NEW.id::text=TG_ARGV[0] AND NEW.state='ready' THEN
                IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE task_id=NEW.id) THEN
                    RAISE EXCEPTION 'fixture failed before registered inputs';
                END IF;
                RAISE EXCEPTION 'injected task ready failure' USING ERRCODE='23514';
            ELSIF TG_TABLE_NAME='runs' AND NEW.id::text=TG_ARGV[0] AND NEW.status='queued' THEN
                IF NOT EXISTS(SELECT 1 FROM photo.tasks WHERE run_id=NEW.id AND state='ready') THEN
                    RAISE EXCEPTION 'fixture failed before task ready';
                END IF;
                RAISE EXCEPTION 'injected run queued failure' USING ERRCODE='23514';
            END IF; RETURN NEW; END $$""")
        table='tasks' if stage=='task_ready' else 'runs'
        identity=task if stage=='task_ready' else str(run)
        database.execute(sql.SQL('CREATE TRIGGER test_removal_activation_failure BEFORE UPDATE ON photo.{} FOR EACH ROW EXECUTE FUNCTION photo.test_removal_activation_failure({})').format(sql.Identifier(table),sql.Literal(identity)))
        before=tenant_transaction_state(database,args[0])
        with pytest.raises(psycopg.Error) as error:
            commit_preview(database,args,run,scope)
        assert error.value.sqlstate=='23514'
        assert tenant_transaction_state(database,args[0])==before
        assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(run,)).fetchone()[0]==0
        assert database.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE task_id=%s',(task,)).fetchone()[0]==0


@pytest.mark.parametrize('mutation',['cancel_run','session','policy','report','quota_share'])
def test_removal_activation_rechecks_live_authority_before_materialization(database,mutation):
    from removal_transaction_state import tenant_transaction_state
    from test_v27_background_cleanup_preview_quota_authority import attach_share_scope
    args,run,scope=published_transport_fixture(database)
    if mutation=='cancel_run':
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,))
    elif mutation=='session':
        database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(args[3],))
    elif mutation=='policy':
        database.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(scope['preview_profile']['qualification']['policy_id'],))
    elif mutation=='report':
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(scope['preview_profile']['qualification']['report']['artifact_id'],))
    else:
        attach_share_scope(database,args,run)
    before=tenant_transaction_state(database,args[0])
    with pytest.raises(psycopg.Error):
        commit_preview(database,args,run,scope)
    assert tenant_transaction_state(database,args[0])==before
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(run,)).fetchone()[0]==0


@pytest.mark.parametrize('name,api_allowed',[
    ('seal_semantic_removal_preview_inputs',False),
    ('activate_semantic_removal_preview',False),
    ('commit_semantic_removal_preview',True),
])
def test_removal_activation_narrow_entrypoint_preserves_private_components(database,name,api_allowed):
    signature='photo.'+name+'(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb)'
    row=database.execute('SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    assert row[0]=='photo_fn_owner' and row[1] is True
    assert 'search_path=pg_catalog, photo' in row[2]
    assert database.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')",(signature,)).fetchone()[0] is api_allowed
    for role in ('photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is False
    assert database.execute("SELECT NOT EXISTS(SELECT 1 FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE p.oid=to_regprocedure(%s) AND a.grantee=0 AND a.privilege_type='EXECUTE')",(signature,)).fetchone()[0] is True


@pytest.mark.parametrize('with_alpha', [False, True])
def test_removal_multi_preview_keeps_distinct_reference_and_dense_inputs(database, with_alpha):
    from removal_apply_documents_fixture import prepared_removal_review_fixtures
    from removal_transaction_state import tenant_transaction_state
    import json
    reviews = prepared_removal_review_fixtures(database, with_alpha=with_alpha, dense_warps=2)
    first = published_transport_fixture(database, review=reviews[0])
    bundles = [first]
    for review in reviews[1:]:
        review[2]['removal_preview_qualification'] = first[2]['preview_profile']['qualification']
        bundles.append(published_transport_fixture(database, review=review))
    distinct_reference_seen = False
    for args, run, scope in bundles:
        source = json.loads(scope['source_body'])
        intent = json.loads(scope['intent_body'])
        reference = json.loads(scope.get('reference_body', scope['source_body']))
        entries = scope['preview_inventory']['entries']
        assert dict(ref=intent['reference']['source'], contract='WorkingImage') in entries
        for image in (source, reference):
            assert dict(ref=image['pixels'], contract='NpyArray') in entries
            for step in image['transform_chain']['steps']:
                if step['kind'] == 'dense_warp':
                    assert dict(ref=step['inverse_field'], contract='NpyArray') in entries
            if with_alpha:
                assert dict(ref=image['alpha'], contract='NpyArray') in entries
        assert len(entries) == len({entry['ref']['artifact_id'] for entry in entries})
        if intent['reference']['source'] != json.loads(args[5])['source']['source']:
            distinct_reference_seen = True
            changed = dict(scope, reference_body=scope['source_body'])
            before = tenant_transaction_state(database, args[0])
            with pytest.raises(psycopg.Error):
                commit_preview(database, args, run, changed)
            assert tenant_transaction_state(database, args[0]) == before
        assert str(commit_preview(database, args, run, scope)) == scope['preview_node']['id']
    assert distinct_reference_seen
