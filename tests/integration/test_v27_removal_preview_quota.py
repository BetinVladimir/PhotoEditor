"""Own quota replay/refusal over real SQL; synthetic source/qualification metadata."""
from copy import deepcopy
import pytest
from removal_preview_activation_fixture import prepared_params_fixture, reserve
from test_v27_removal_preview_sql_roles import database, isolated_database_config


def state(db,tenant,run):
    return (
        db.execute('SELECT to_jsonb(u) FROM photo.tenant_usage u WHERE tenant_id=%s',(tenant,)).fetchall(),
        db.execute("SELECT to_jsonb(q) FROM photo.quota_reservations q WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(tenant,run)).fetchall(),
        db.execute('SELECT to_jsonb(r) FROM photo.runs r WHERE tenant_id=%s AND id=%s',(tenant,run)).fetchall(),
    )


@pytest.mark.parametrize('with_alpha',[False,True])
def test_removal_quota_exact_replay_never_reserves_twice(database,with_alpha):
    args,run,scope=prepared_params_fixture(database,with_alpha=with_alpha)
    profile=scope['preview_profile']
    assert reserve(database,args,run,scope['source_body'],profile) is True
    row=database.execute("SELECT bytes,files,principal_type,principal_id,share_id,state FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(args[0],run)).fetchone()
    assert row==(profile['resource_estimate']['output_bytes'],0,'user',args[2],None,'reserved')
    before=state(database,args[0],run)
    assert reserve(database,args,run,scope['source_body'],profile) is True
    assert state(database,args[0],run)==before


@pytest.mark.parametrize('mutation',['capacity','output_budget','device','source','policy','frozen_profile','expired'])
def test_removal_quota_refusal_preserves_accounting(database,mutation):
    args,run,scope=prepared_params_fixture(database)
    profile=deepcopy(scope['preview_profile']);source=scope['source_body']
    if mutation in ('frozen_profile','expired'):
        assert reserve(database,args,run,source,profile) is True
    if mutation=='capacity':
        database.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','0'::jsonb) WHERE id=%s",(args[0],))
    elif mutation=='output_budget':profile['resource_estimate']['output_bytes']-=1
    elif mutation=='device':profile['resource_estimate']['vram_bytes']=1
    elif mutation=='source':source=source+b'\n' if isinstance(source,bytes) else source+'\n'
    elif mutation=='policy':
        database.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(profile['qualification']['policy_id'],))
    elif mutation=='frozen_profile':profile['resource_estimate']['ram_bytes']+=1
    elif mutation=='expired':
        database.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(args[0],run))
    before=state(database,args[0],run)
    assert reserve(database,args,run,source,profile) is False
    assert state(database,args[0],run)==before


def test_removal_quota_replay_denies_real_share_scope(database):
    from test_v27_background_cleanup_preview_quota_authority import attach_share_scope
    args,run,scope=prepared_params_fixture(database)
    assert reserve(database,args,run,scope['source_body'],scope['preview_profile']) is True
    share=attach_share_scope(database,args,run)
    before=state(database,args[0],run)
    assert reserve(database,args,run,scope['source_body'],scope['preview_profile']) is False
    assert state(database,args[0],run)==before
    assert database.execute("SELECT share_id FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(args[0],run)).fetchone()[0]==share


@pytest.mark.parametrize('sqlstate',['22023','23514'])
def test_removal_quota_post_insert_failure_rolls_back_reservation_and_usage(database,sqlstate):
    import psycopg
    from psycopg import sql
    args,run,scope=prepared_params_fixture(database)
    # Disposable transaction only: inject failure at the UPDATE following the
    # real INSERT. No production authority function is replaced or bypassed.
    with database.transaction(force_rollback=True):
        database.execute("""CREATE FUNCTION photo.test_removal_quota_usage_failure() RETURNS trigger
            LANGUAGE plpgsql AS $$ BEGIN
            IF NEW.tenant_id::text=TG_ARGV[0] THEN
                RAISE EXCEPTION 'injected quota usage failure' USING ERRCODE=TG_ARGV[1];
            END IF; RETURN NEW; END $$""")
        database.execute(sql.SQL('CREATE TRIGGER test_removal_quota_usage_failure BEFORE UPDATE ON photo.tenant_usage FOR EACH ROW EXECUTE FUNCTION photo.test_removal_quota_usage_failure({},{})').format(sql.Literal(str(args[0])),sql.Literal(sqlstate)))
        before=state(database,args[0],run)
        if sqlstate=='22023':
            assert reserve(database,args,run,scope['source_body'],scope['preview_profile']) is False
        else:
            with pytest.raises(psycopg.Error) as error:
                reserve(database,args,run,scope['source_body'],scope['preview_profile'])
            assert error.value.sqlstate==sqlstate
        assert state(database,args[0],run)==before


@pytest.mark.parametrize('mutation',['principal_id','principal_type','released','bytes','files'])
def test_removal_quota_existing_reservation_must_match_exact_owner_and_state(database,mutation):
    from uuid import uuid4
    args,run,scope=prepared_params_fixture(database)
    assert reserve(database,args,run,scope['source_body'],scope['preview_profile']) is True
    updates={
        'principal_id':('principal_id=%s',(uuid4(),)),
        'principal_type':("principal_type='share'",()),
        'released':("state='released'",()),
        'bytes':('bytes=bytes+1',()),
        'files':('files=1',()),
    }
    assignment,values=updates[mutation]
    database.execute("UPDATE photo.quota_reservations SET "+assignment+" WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(*values,args[0],run))
    before=state(database,args[0],run)
    assert reserve(database,args,run,scope['source_body'],scope['preview_profile']) is False
    assert state(database,args[0],run)==before


def test_removal_quota_callable_only_by_api_with_fixed_definer_path(database):
    signature='photo.reserve_semantic_removal_preview_quota(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb)'
    row=database.execute('SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)',(signature,)).fetchone()
    assert row[0]=='photo_fn_owner' and row[1] is True
    assert 'search_path=pg_catalog, photo' in row[2]
    assert database.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')",(signature,)).fetchone()[0] is True
    for role in ('photo_worker','photo_scheduler','photo_portal','photo_gateway','photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is False
    assert database.execute("SELECT NOT EXISTS(SELECT 1 FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE p.oid=to_regprocedure(%s) AND a.grantee=0 AND a.privilege_type='EXECUTE')",(signature,)).fetchone()[0] is True
