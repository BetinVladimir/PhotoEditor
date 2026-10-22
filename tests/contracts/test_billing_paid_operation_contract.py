"""Permanent source contract tests; no database/runtime acceptance."""
from pathlib import Path
import importlib.util,json,re
ROOT=Path(__file__).resolve().parents[2]
def test_paid_operation_generated_forward_contract_and_acl_registry():
    s=importlib.util.spec_from_file_location('paid_operation_generator',ROOT/'scripts/generate-billing-paid-operation.py');g=importlib.util.module_from_spec(s);s.loader.exec_module(g)
    assert g.render()==(ROOT/'backend/db/migrations/02608_billing_paid_operation_funding.sql').read_text()
    sql=(ROOT/'docs/billing/contracts/paid-operation-funding.sql').read_text();registry=json.loads((ROOT/'docs/billing/contracts/paid-operation-functions.json').read_text())['functions']
    found={name+'('+','.join(a.strip().split()[1]for a in args.split(',')if a.strip())+')'for name,args in re.findall(r'CREATE (?:OR REPLACE )?FUNCTION photo\.([a-z0-9_]+)\(([^)]*)\)',sql)}
    assert found=={r['signature']for r in registry}
    for row in registry:
        assert row['execute_roles']==[]
        assert 'ALTER FUNCTION photo.'+row['signature']+' OWNER TO photo_fn_owner;'in sql
        assert 'REVOKE ALL ON FUNCTION photo.'+row['signature']+' FROM PUBLIC,photo_api,photo_scheduler,photo_worker'in sql
    assert 'BILLING_PAID_OPERATION_PREDECESSOR_DRIFT'in sql and 'old.proacl IS DISTINCT FROM'in sql
    assert sql.index('BILLING_PAID_OPERATION_PREDECESSOR_DRIFT')<sql.index('CREATE OR REPLACE FUNCTION')
def test_paid_oversize_is_item_pinned_not_cache_or_allowance_expansion():
    sql=(ROOT/'docs/billing/contracts/paid-operation-funding.sql').read_text()
    assert "i.quantity=p_quantity"in sql and "p_key='op/'||i.id"in sql
    assert "p_instance='operation/'||req.run_id"in sql and 'req.period_id=p_period'in sql
    assert 'FOR SHARE OF i,req;'in sql and 'FOR SHARE OF i,req,op'not in sql
    assert 'paid:=p_quantity;'in sql and 'extra:=0;'in sql
    assert 'old.allocated_quantity<>p_quantity OR old.allowance_quantity<>0'in sql
    assert "IF available<p_quantity THEN RAISE EXCEPTION 'BILLING_QUOTA_EXCEEDED'"in sql
    assert 'photo.billing_allocate_admission_lease(p_tenant,task.resource_code,period,instance,generation,1' in sql
