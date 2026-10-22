import importlib.util,json,re
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
def test_operations_generated_migration_and_acl_manifest():
    spec=importlib.util.spec_from_file_location('operations_generator',ROOT/'scripts/generate-billing-operations.py');g=importlib.util.module_from_spec(spec);spec.loader.exec_module(g)
    assert (ROOT/'backend/db/migrations/02607_billing_operations_workflow.sql').read_text()==g.render()
    source=(ROOT/'docs/billing/contracts/operations-workflow.sql').read_text()
    signatures={name+'('+','.join(a.strip().split()[1] for a in args.split(',') if a.strip())+')' for name,args in re.findall(r'CREATE (?:OR REPLACE )?FUNCTION photo\.([a-z0-9_]+)\(([^)]*)\)',source)}
    manifest=json.loads((ROOT/'docs/billing/contracts/operations-functions.json').read_text())['functions']
    assert signatures=={f['signature'] for f in manifest}
    assert 'DEFAULT false' in source and 'INSERT INTO photo.billing_operation_controls' not in source
    assert 'INSERT INTO photo.billing_operation_policies' not in source
    for f in manifest:
        assert 'REVOKE ALL ON FUNCTION photo.'+f['signature']+' FROM PUBLIC,photo_api,photo_scheduler,photo_worker;' in source
        assert set(f['execute_roles'])<= {'photo_api','photo_scheduler'}
def test_operation_hook_precedes_input_locks_and_seal_keeps_legacy_authority():
    source=(ROOT/'backend/internal/v2input/admission.go').read_text()
    assert source.index('billing.PrepareOperations(')<source.index('if err := RegisterInTransaction')
    sql=(ROOT/'docs/billing/contracts/operations-workflow.sql').read_text()
    wrapper=sql[sql.index('CREATE FUNCTION photo.seal_v2_graph'):]
    assert wrapper.index('billing_bind_operations')<wrapper.index('seal_before_operation_billing')
    assert "BILLING_GRAPH_REFUSED" in wrapper
    assert 'OperationBilling: true' in (ROOT/'backend/internal/workflow/service.go').read_text()
