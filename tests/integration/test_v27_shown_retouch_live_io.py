"""Real own lease→gateway/heartbeat authority; no physical IO or GPU execution."""
from uuid import uuid4
import pytest
import psycopg
from retouch_transaction_state import tenant_transaction_state
from psycopg.types.json import Jsonb
from test_v27_shown_retouch_worker_claim import dispatched_copy_fixture
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


def claimed_fixture(db,*,dense_warps=0,reference_dense_warps=None,with_alpha=False,schema_pin=None,historical_writer_head=None):
    tenant,task,run,worker,command,old_fence,values,plan=dispatched_copy_fixture(db,include_documents=True,dense_warps=dense_warps,reference_dense_warps=reference_dense_warps,with_alpha=with_alpha,schema_pin=schema_pin,historical_writer_head=historical_writer_head,forward_upgrade=historical_writer_head is not None)
    db.execute('UPDATE photo.worker_registrations SET certificate_sha256=%s WHERE tenant_id=%s AND worker_id=%s',('f'*64,tenant,worker))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_worker')
        row=db.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',(task,worker,Jsonb(command))).fetchone()
        db.execute('RESET ROLE')
        assert row[0]=='claimed' and row[1]==old_fence+1
    return dict(tenant=tenant,task=task,run=run,worker=worker,fence=row[1],command=command,values=values,plan=plan)


def gateway(db,x,artifact,*,fence=None,worker=None,certificate='f'*64):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_gateway')
        row=db.execute('SELECT * FROM photo.gateway_input(%s,1,%s,%s,%s,%s)',
            (x['task'],x['fence'] if fence is None else fence,x['worker'] if worker is None else worker,certificate,artifact)).fetchone()
        db.execute('RESET ROLE')
        return row


def heartbeat(db,x,*,fence=None):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_worker')
        row=db.execute('SELECT * FROM photo.heartbeat_task(%s,1,%s,%s,%s)',
            (x['task'],x['fence'] if fence is None else fence,x['worker'],Jsonb(dict(stage='copy')))).fetchone()
        db.execute('RESET ROLE')
        return row


def test_live_retouch_lease_grants_only_pinned_inputs_and_renews(database):
    x=claimed_fixture(database)
    for entry in x['plan']['inventory']['entries']:
        ref=entry['ref'];row=gateway(database,x,ref['artifact_id'])
        assert str(row[0])==ref['artifact_id'] and row[4]==ref['sha256']
        assert row[3] not in ('','null',None)
    assert gateway(database,x,uuid4()) is None
    assert gateway(database,x,x['command']['params']['artifact_id'],fence=x['fence']+1) is None
    assert gateway(database,x,x['command']['params']['artifact_id'],worker=uuid4()) is None
    assert gateway(database,x,x['command']['params']['artifact_id'],certificate='e'*64) is None
    renewed=heartbeat(database,x)
    assert renewed[0] is True and renewed[1] is not None and renewed[2] is False


@pytest.mark.parametrize('dense_warps',[1,32])
def test_persisted_dense_chain_survives_publication_claim_and_gateway(database,dense_warps):
    import json
    x=claimed_fixture(database,dense_warps=dense_warps)
    image=json.loads(x['plan']['documents']['source'])
    steps=image['transform_chain']['steps']
    assert len(steps)==dense_warps and all(s['kind']=='dense_warp' for s in steps)
    fields={s['inverse_field']['artifact_id']:s['inverse_field']['sha256'] for s in steps}
    assert len(fields)==dense_warps
    scope=x['values'][0][2]
    upstream={str(aid):digest for aid,digest,contract in scope['source_inputs'] if contract=='NpyArray'}
    assert fields.items()<=upstream.items()
    entries=x['plan']['inventory']['entries']
    expected={e['ref']['artifact_id']:e['ref']['sha256'] for e in entries}
    assert fields.items()<=expected.items()
    rows=database.execute('SELECT artifact_id,sha256,contract,ordinal FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s ORDER BY ordinal',
        (x['tenant'],x['task'])).fetchall()
    direct=[r for r in rows if r[3]<1000000]
    assert len(rows)==len(entries)+2
    assert [(str(r[0]),r[1],r[2],r[3]) for r in direct]==[
        (e['ref']['artifact_id'],e['ref']['sha256'],e['contract'],i) for i,e in enumerate(entries)]
    assert len(x['command']['inputs'])<=512
    for artifact,digest in fields.items():
        row=gateway(database,x,artifact)
        assert row is not None and row[4]==digest and row[3] not in (None,'','null')
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_fn_owner')
        assert database.execute('SELECT photo.semantic_shown_retouch_closure_ready(%s,%s)',
            (x['task'],Jsonb(json.loads(x['plan']['params_body'])))).fetchone()[0] is True
        database.execute('RESET ROLE')
    # Losing a declared physical input invalidates the full live closure.
    artifact=next(iter(fields))
    database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(artifact,))
    assert gateway(database,x,artifact) is None
    assert heartbeat(database,x)==(False,None,True)


def test_exact_admitted_inventory_refuses_padding_to_wire_ceiling(database):
    import copy
    import json
    from retouch_copy_apply_fixture import admission_task_fixture
    values,task=admission_task_fixture(database,dense_warps=32)
    params=json.loads(task['params_body'])
    presentation=json.loads(task['documents']['presentation'])
    preview=json.loads(values[0][0][5])
    docs=[json.loads(task['documents'][name]) for name in
        ('candidate','intent','source','reference','mask','protection')]
    original=copy.deepcopy(task['inventory'])
    assert len(original['entries'])<512
    padded=copy.deepcopy(original)
    while len(padded['entries'])<512:
        padded['entries'].append(dict(contract='NpyArray',ref=dict(artifact_id=str(uuid4()),sha256='e'*64)))
    padded['entries'].sort(key=lambda e:e['ref']['artifact_id'])
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_fn_owner')
        def exact(inventory):
            args=[params,presentation,preview,task['node'],inventory,*docs]
            return database.execute('SELECT photo.semantic_shown_retouch_inventory_exact('+','.join(['%s']*11)+')',
                tuple(Jsonb(a) for a in args)).fetchone()[0]
        assert exact(original) is True
        assert exact(padded) is False
        database.execute('RESET ROLE')


@pytest.mark.parametrize('mutation',['renderer','worker','session','run','task','policy','quota','input'])
def test_live_retouch_authority_loss_denies_io_and_does_not_extend_lease(database,mutation):
    x=claimed_fixture(database)
    args=x['values'][0][0]
    if mutation=='renderer':
        database.execute("UPDATE photo.worker_registrations SET v2_renderer_versions='[]' WHERE tenant_id=%s AND worker_id=%s",(x['tenant'],x['worker']))
    elif mutation=='worker':
        database.execute('UPDATE photo.worker_registrations SET enabled=false WHERE tenant_id=%s AND worker_id=%s',(x['tenant'],x['worker']))
    elif mutation=='session':
        database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(args[3],))
    elif mutation=='run':
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(x['run'],))
    elif mutation=='task':
        database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(x['task'],))
    elif mutation=='policy':
        policy=x['plan']['node']['execution_profile']['qualification']['policy_id']
        database.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(policy,))
    elif mutation=='quota':
        database.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(x['tenant'],x['run']))
    else:
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(x['command']['params']['artifact_id'],))
    def snapshot():
        return (database.execute('SELECT lease_until,deadline,progress,attempt,fencing_token FROM photo.tasks WHERE id=%s',(x['task'],)).fetchone(),
            database.execute('SELECT lease_until,fencing_token FROM photo.resource_slots WHERE task_id=%s',(x['task'],)).fetchone(),
            database.execute('SELECT expires_at,fencing_token FROM photo.resource_reservations WHERE task_id=%s',(x['task'],)).fetchone())
    before=snapshot()
    assert gateway(database,x,x['command']['params']['artifact_id']) is None
    assert heartbeat(database,x)==(False,None,True)
    assert snapshot()==before


@pytest.mark.parametrize('function,predecessor',[
    ('gateway_input','gateway_input_before_shown_retouch'),
    ('gateway_input_before_shown_retouch','gateway_input_before_retouch_preview'),
    ('gateway_input_before_retouch_preview','gateway_input_before_shown_background_cleanup'),
    ('gateway_input_before_shown_background_cleanup','gateway_input_before_background_cleanup_preview'),
    ('gateway_input_before_background_cleanup_preview','gateway_input_before_shown_removal'),
    ('gateway_input_before_shown_removal','gateway_input_before_removal_preview'),
    ('gateway_input_before_removal_preview','gateway_input_pre_shown_media'),
    ('gateway_input_pre_shown_media','gateway_input_before_semantic_apply'),
    ('gateway_input_before_semantic_apply','gateway_input_before_render_cache'),
    ('heartbeat_task','heartbeat_task_before_shown_retouch'),
    ('heartbeat_task_before_shown_retouch','heartbeat_task_before_retouch_preview'),
    ('heartbeat_task_before_retouch_preview','heartbeat_task_before_shown_background_cleanup'),
    ('heartbeat_task_before_shown_background_cleanup','heartbeat_task_before_background_cleanup_preview'),
    ('heartbeat_task_before_background_cleanup_preview','heartbeat_task_before_shown_removal'),
    ('heartbeat_task_before_shown_removal','heartbeat_task_before_removal_preview'),
    ('heartbeat_task_before_removal_preview','heartbeat_task_pre_shown_media'),
    ('heartbeat_task_pre_shown_media','heartbeat_task_pre_v23_series_device'),
    # Preserve the historical chain scenario ID while checking its new first hop.
    pytest.param('authorize_gateway_task','authorize_gateway_task_before_execution_source',
        id='authorize_gateway_task-authorize_gateway_task_pre_v23_series_device'),
    ('authorize_gateway_task_before_execution_source','authorize_gateway_task_pre_v23_series_device'),
    ('authorize_gateway_task_pre_v23_series_device','authorize_gateway_task_pre_v28'),
])
def test_installed_live_io_predecessors_remain_connected(database,function,predecessor):
    # Check the installed migration chain as well as behavioral cases above.
    # No replacement authority functions or hand-written permission bypasses.
    definitions=database.execute(
        'SELECT pg_get_functiondef(p.oid) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname=%s AND p.proname=%s',
        ('photo',function)).fetchall()
    assert len(definitions)==1
    assert 'photo.'+predecessor+'(' in definitions[0][0]


def test_foreign_fence_cannot_renew_live_retouch_lease(database):
    x=claimed_fixture(database)
    before=database.execute('SELECT lease_until,progress FROM photo.tasks WHERE id=%s',(x['task'],)).fetchone()
    assert heartbeat(database,x,fence=x['fence']+1)[0] is False
    assert database.execute('SELECT lease_until,progress FROM photo.tasks WHERE id=%s',(x['task'],)).fetchone()==before


@pytest.mark.parametrize('mutation',['renderer','resource','backend','operation','freshness'])
def test_retouch_live_renderer_loss_refuses_all_inputs_without_renewal(database,mutation):
    x=claimed_fixture(database)
    # Fixed assignments only; mutate registration lifecycle, never object pins.
    assignments={
        'renderer':"v2_renderer_versions='[]'::jsonb",
        'resource':"resource_classes='[]'::jsonb",
        'backend':"backends='[]'::jsonb",
        'operation':"qualified_operations=qualified_operations-'semantic.adopt_shown_retouch_region'",
        'freshness':"last_seen='2000-01-01T00:00:00Z'::timestamptz",
    }
    def snapshot():
        return (
            database.execute('SELECT lease_until,deadline,progress,attempt,fencing_token FROM photo.tasks WHERE id=%s',(x['task'],)).fetchone(),
            database.execute('SELECT lease_until,fencing_token FROM photo.resource_slots WHERE task_id=%s',(x['task'],)).fetchone(),
            database.execute('SELECT expires_at,fencing_token FROM photo.resource_reservations WHERE task_id=%s',(x['task'],)).fetchone(),
            database.execute('SELECT last_seen FROM photo.worker_registrations WHERE tenant_id=%s AND worker_id=%s',(x['tenant'],x['worker'])).fetchone(),
        )
    with database.transaction(force_rollback=True):
        database.execute('UPDATE photo.worker_registrations SET '+assignments[mutation]+' WHERE tenant_id=%s AND worker_id=%s',
            (x['tenant'],x['worker']))
        before=snapshot()
        for entry in x['plan']['inventory']['entries']:
            assert gateway(database,x,entry['ref']['artifact_id']) is None
        assert gateway(database,x,x['command']['params']['artifact_id']) is None
        assert heartbeat(database,x)==(False,None,True)
        assert snapshot()==before
    assert gateway(database,x,x['command']['params']['artifact_id']) is not None
    assert heartbeat(database,x)[0] is True


@pytest.mark.parametrize('expired_field',['lease_until','deadline'])
def test_expired_retouch_task_cannot_read_or_renew(database,expired_field):
    x=claimed_fixture(database)
    # Identifiers come only from the fixed parametrization above. Expire mutable
    # lifecycle state without rewriting any immutable artifact or result proof.
    assert expired_field in ('lease_until','deadline')
    database.execute(
        f"UPDATE photo.tasks SET {expired_field}=clock_timestamp()-interval '1 second' WHERE id=%s",
        (x['task'],))
    def snapshot():
        return (
            database.execute('SELECT lease_until,deadline,progress,attempt,fencing_token FROM photo.tasks WHERE id=%s',(x['task'],)).fetchone(),
            database.execute('SELECT lease_until,fencing_token FROM photo.resource_slots WHERE task_id=%s',(x['task'],)).fetchone(),
            database.execute('SELECT expires_at,fencing_token FROM photo.resource_reservations WHERE task_id=%s',(x['task'],)).fetchone(),
            database.execute('SELECT last_seen FROM photo.worker_registrations WHERE tenant_id=%s AND worker_id=%s',(x['tenant'],x['worker'])).fetchone(),
        )
    before=snapshot()
    for entry in x['plan']['inventory']['entries']:
        assert gateway(database,x,entry['ref']['artifact_id']) is None
    assert gateway(database,x,x['command']['params']['artifact_id']) is None
    result=heartbeat(database,x)
    assert result[0] is False and result[1] is None
    # Cancellation is separate from lease expiry; do not invent a cancel flag.
    assert snapshot()==before


@pytest.mark.parametrize('schema,expected', [
    pytest.param('886cb3e1b7316c40d6696ab57e59bcc2a8ec714b50ffd8d51c0a1cb9cf799f90', True, marks=pytest.mark.migration_head(2406)),
    pytest.param('7593be39f45ce3bf1aa8c5b3c42bc9defc82f2416def579fd2d3e6d3ea165ab0', True, marks=pytest.mark.migration_head(2415)),
    ('0'*64, False), ('', False), (None, False),
])
def test_live_retouch_schema_pin_preserves_independent_authority(database,schema,expected):
    # Historical positives are produced at their lawful original SQL head,
    # then forward-upgraded without changing the immutable task seal.
    head = (2406 if schema.startswith('886cb3') else 2415) if expected else None
    x=claimed_fixture(database,schema_pin=schema if expected else None,historical_writer_head=head)
    if not expected:
        # Invalid pins cannot be introduced into a sealed task. Keep that exact
        # invariant, then test compatibility independently and real live authority
        # on the unchanged healthy task (not a fabricated malformed admission).
        before=tenant_transaction_state(database,x['tenant'])
        with pytest.raises(psycopg.errors.ObjectNotInPrerequisiteState) as caught:
            with database.transaction():
                if schema is None:
                    database.execute("UPDATE photo.tasks SET execution_profile=execution_profile-'contracts_sha' WHERE id=%s",(x['task'],))
                else:
                    database.execute("UPDATE photo.tasks SET execution_profile=jsonb_set(execution_profile,'{contracts_sha}',%s::jsonb) WHERE id=%s",(Jsonb(schema),x['task']))
        assert caught.value.sqlstate == '55000' and caught.value.diag.message_primary == 'V2_TASK_INPUTS_IMMUTABLE'
        assert tenant_transaction_state(database,x['tenant']) == before
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_fn_owner')
            assert database.execute("SELECT photo.shown_copy_schema_wire_ready(%s,%s)",(schema,Jsonb(dict(preview_fence=1)))).fetchone()[0] is False
            database.execute('RESET ROLE')
    def ready():
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_fn_owner')
            result=database.execute('SELECT photo.semantic_shown_retouch_task_live(%s)',(x['task'],)).fetchone()[0]
            database.execute('RESET ROLE')
            return result
    assert ready() is True
    database.execute('UPDATE photo.worker_registrations SET enabled=false WHERE tenant_id=%s AND worker_id=%s',(x['tenant'],x['worker']))
    assert ready() is False


def test_sequential_outputs_preserve_live_heartbeat_and_exact_run_accounting(database):
    x=claimed_fixture(database)
    before=database.execute("SELECT id,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s AND state='reserved'",(x['tenant'],x['run'])).fetchone()
    usage=database.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(x['tenant'],)).fetchone()
    accepted=[]
    for role,media in [('pixels','application/x-npy'),('working_image','application/json')]:
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_gateway')
            ticket=database.execute('SELECT * FROM photo.begin_output(%s,1,%s,%s,%s,%s,%s,2,%s,%s,%s)',(x['task'],x['fence'],x['worker'],'f'*64,uuid4(),role,'a'*64,media,'test')).fetchone()
            assert ticket[0]=='created'
            completed=database.execute('SELECT * FROM photo.complete_output(%s,1,%s,%s,%s,%s,%s,2,%s,%s)',(x['task'],x['fence'],x['worker'],'f'*64,ticket[1],'output-version','a'*64,media)).fetchone()
            assert completed==('committed',ticket[2],'a'*64)
            database.execute('RESET ROLE')
        accepted.append(ticket)
        # Before02537 first debit made q.bytes != expected and broke renewal.
        assert heartbeat(database,x)[0] is True
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_fn_owner')
            assert database.execute('SELECT photo.execution_source_live(%s)',(x['task'],)).fetchone()==(True,)
            database.execute('RESET ROLE')
        q=database.execute('SELECT bytes FROM photo.quota_reservations WHERE id=%s',(before[0],)).fetchone()[0]
        allocated=database.execute('SELECT coalesce(sum(bytes),0) FROM photo.output_uploads WHERE run_reservation_id=%s',(before[0],)).fetchone()[0]
        assert q+allocated==before[1] and allocated==2*len(accepted)
    assert database.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(x['tenant'],)).fetchone()==(usage[0]-4,usage[1]+4)
    source=x['plan']['inventory']['entries'][0]['ref']['artifact_id']
    database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(source,))
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_gateway')
        replay=database.execute('SELECT * FROM photo.complete_output(%s,1,%s,%s,%s,%s,%s,2,%s,%s)',(x['task'],x['fence'],x['worker'],'f'*64,accepted[0][1],'output-version','a'*64,'application/x-npy')).fetchone()
        assert replay[0]=='stale'
        database.execute('RESET ROLE')
    assert heartbeat(database,x)==(False,None,True)


def test_registry_disable_revokes_renewal_and_output_authority(database):
    x=claimed_fixture(database)
    database.execute("UPDATE photo.operation_registry SET v2_enabled=false WHERE operation='semantic.adopt_shown_retouch_region' AND version='1'")
    assert heartbeat(database,x)==(False,None,True)
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_gateway')
        refused=database.execute('SELECT * FROM photo.begin_output(%s,1,%s,%s,%s,%s,%s,2,%s,%s,%s)',(x['task'],x['fence'],x['worker'],'f'*64,uuid4(),'pixels','a'*64,'application/x-npy','test')).fetchone()
        assert refused[0]=='stale'
        database.execute('RESET ROLE')
    assert database.execute('SELECT count(*) FROM photo.output_uploads WHERE task_id=%s',(x['task'],)).fetchone()==(0,)


def test_execution_source_helper_remains_private_and_final_fences_follow_waits(database):
    row=database.execute("SELECT pg_get_userbyid(proowner),prosecdef FROM pg_proc WHERE oid='photo.execution_source_live(uuid)'::regprocedure").fetchone()
    assert row==('photo_fn_owner',True)
    for role in ('photo_api','photo_worker','photo_gateway','photo_scheduler','photo_maintenance','photo_portal','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,'photo.execution_source_live(uuid)','EXECUTE')",(role,)).fetchone()==(False,)
    result=database.execute("SELECT prosrc FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' AND strpos(prosrc,' INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,review_required,result_digest)')>0").fetchone()[0]
    source=result.index('-- 02537:')
    assert result.index('photo.execution_source_live(p_task)',source)<result.index('t.lease_until<=clock_timestamp()',source)<result.index(' INSERT INTO photo.task_results(')
    heartbeat_body=database.execute("SELECT prosrc FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' AND strpos(prosrc,'UPDATE photo.tasks SET lease_until=until_time,progress=p_progress')>0").fetchone()[0]
    source=heartbeat_body.index('-- 02537:')
    assert heartbeat_body.index('t:=photo.lock_task(p_task);')<source<heartbeat_body.index('photo.execution_source_live(p_task)',source)<heartbeat_body.index('t.lease_until<=clock_timestamp()',source)<heartbeat_body.index(' until_time:=')


def test_claim_final_fence_stays_inside_atomic_claim_branch(database):
    source=database.execute("SELECT prosrc FROM pg_proc WHERE oid='photo.claim_task(uuid,integer,uuid,jsonb)'::regprocedure").fetchone()[0]
    assert source.index("IF claimed.outcome='claimed' THEN")<source.index('TASK_CLAIM_SOURCE_STALE')<source.index('TASK_CLAIM_LEASE_STALE')<source.index('RETURN QUERY SELECT claimed.outcome')
    assert "ERRCODE='40001'" in source[source.index('TASK_CLAIM_SOURCE_STALE'):]
