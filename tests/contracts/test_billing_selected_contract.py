import importlib.util,json,re
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
def test_operations_generated_migration_and_acl_manifest():
    spec=importlib.util.spec_from_file_location('selected_generator',ROOT/'scripts/generate-billing-selected.py');g=importlib.util.module_from_spec(spec);spec.loader.exec_module(g)
    assert (ROOT/'backend/db/migrations/02609_billing_selected_workflow.sql').read_text()==g.render()
    source=(ROOT/'docs/billing/contracts/selected-workflow.sql').read_text()
    signatures={name+'('+','.join(a.strip().split()[1] for a in args.split(',') if a.strip())+')' for name,args in re.findall(r'CREATE (?:OR REPLACE )?FUNCTION photo\.([a-z0-9_]+)\(([^)]*)\)',source)}
    manifest=json.loads((ROOT/'docs/billing/contracts/selected-functions.json').read_text())['functions']
    assert signatures=={f['signature'] for f in manifest}
    assert 'INSERT INTO photo.billing_operation_controls' not in source
    assert 'INSERT INTO photo.billing_operation_policies' not in source
    for f in manifest:
        assert 'REVOKE ALL ON FUNCTION photo.'+f['signature']+' FROM PUBLIC,photo_api,photo_scheduler,photo_worker;' in source
        assert set(f['execute_roles'])<= {'photo_api','photo_scheduler'}

def test_selected_preserves_paid_funding_and_guards_every_replacement():
    source=(ROOT/'docs/billing/contracts/selected-workflow.sql').read_text()
    names=re.findall(r'CREATE OR REPLACE FUNCTION photo\.([a-z0-9_]+)',source)
    assert len(names)==6
    for name in names:
        assert 'BILLING_SELECTED_PREDECESSOR_DRIFT: '+name+'(' in source
        assert 'BILLING_SELECTED_CALLER_DRIFT: '+name+'(' in source
    assert 'billing_allocate_paid_operation_lease(p_tenant,task.resource_code,period,instance,generation,1,\'op/\'||task.id,task.quantity)' in source
    assert 'photo.v28_open_eyes_selected_authority(p_tenant,v.id,r.created_by,session)' in source
    assert 'photo.v28_glare_completed_source_ready(p_tenant,p_task,t.canonical_result_id)' in source
    assert not re.search(r'CREATE (?:OR REPLACE )?FUNCTION photo\.(?:get_v28_|v28_)',source)

def test_zero_argument_guards_expect_postgres_null_argnames():
    source=(ROOT/'docs/billing/contracts/selected-workflow.sql').read_text()
    for name in ['billing_guard_operation_item','billing_settle_next_operations']:
        block=next(b for b in source.split('DO $$') if "to_regprocedure('photo."+name+"()')" in b)
        assert 'p.proargnames IS DISTINCT FROM NULL::text[]' in block
        assert 'p.proargnames IS DISTINCT FROM ARRAY[]::text[]' not in block
    for name in ['billing_prepare_operations','billing_bind_operations','seal_v2_graph','billing_settle_operations']:
        block=next(b for b in source.split('DO $$') if "to_regprocedure('photo."+name+"(uuid,uuid)')" in b)
        assert "p.proargnames IS DISTINCT FROM ARRAY['p_tenant','p_run']::text[]" in block

def test_prepare_guard_preserves_cold_lock_timeout():
    source=(ROOT/'docs/billing/contracts/selected-workflow.sql').read_text()
    block=next(b for b in source.split('DO $$') if "to_regprocedure('photo.billing_prepare_operations(uuid,uuid)')" in b)
    assert "p.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, photo','lock_timeout=200ms']::text[]" in block


def test_selected_owner_and_language_lookup_nulls_refuse_drift():
    source=(ROOT/'docs/billing/contracts/selected-workflow.sql').read_text()
    assert source.count("p.proowner IS DISTINCT FROM (SELECT oid FROM pg_catalog.pg_roles WHERE rolname='photo_fn_owner' AND NOT rolcanlogin)")==6
    assert source.count("p.prolang IS DISTINCT FROM (SELECT oid FROM pg_catalog.pg_language WHERE lanname='plpgsql')")==6
    assert 'p.proowner<>(SELECT' not in source
    assert 'p.prolang<>(SELECT' not in source


def test_selected_guard_regtypes_use_canonical_catalog_names():
    from collections import Counter
    source=(ROOT/'docs/billing/contracts/selected-workflow.sql').read_text()
    prefix=source.split('ALTER TABLE photo.billing_operation_items ADD COLUMN success_contract',1)[0]
    types=re.findall(r"p.prorettype<>'([^']+)'::pg_catalog.regtype",prefix)
    assert Counter(types)==Counter({'pg_catalog.bool':4,'pg_catalog.uuid':1,'pg_catalog.trigger':1})
    assert "'pg_catalog.boolean'::pg_catalog.regtype" not in source
