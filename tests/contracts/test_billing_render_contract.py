import importlib.util,json,re
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]

def test_render_migration_contract_and_acl():
    spec=importlib.util.spec_from_file_location('render_generator',ROOT/'scripts/generate-billing-render.py');g=importlib.util.module_from_spec(spec);spec.loader.exec_module(g)
    assert (ROOT/'backend/db/migrations/02605_billing_render_workflow.sql').read_text()==g.render()
    source=(ROOT/'docs/billing/contracts/render-workflow.sql').read_text()
    signatures={name+'('+','.join(a.strip().split()[1] for a in args.split(',') if a.strip())+')' for name,args in re.findall(r'CREATE (?:OR REPLACE )?FUNCTION photo\.([a-z0-9_]+)\(([^)]*)\)',source)}
    functions=json.loads((ROOT/'docs/billing/contracts/render-functions.json').read_text())['functions']
    assert signatures=={f['signature'] for f in functions}
    for f in functions:
        assert 'REVOKE ALL ON FUNCTION photo.'+f['signature']+' FROM PUBLIC;' in source or f['signature']=='billing_settle_apply_run(uuid,uuid)'
        assert all(r=='photo_scheduler' for r in f['execute_roles'])
    assert 'DEFAULT false' in source and 'INSERT INTO photo.billing_render_controls' not in source
    assert "AND route<>'V2.Render'" in source

def test_render_hook_precedes_physical_admission_and_binds_before_commit():
    for filename in ['render_store.go','render_batch_store.go']:
        source=(ROOT/'backend/internal/v2render'/filename).read_text()
        prep=source.index('billing.PrepareRender(');bind=source.index('billing.BindRender(')
        assert prep<source.index('SELECT photo.begin_v26_render_',prep)<bind
    assert 'Billing: true' in (ROOT/'backend/internal/workflow/service.go').read_text()


def test_seal_wrapper_preserves_scheduler_only_definer_boundary():
    source=(ROOT/'docs/billing/contracts/render-workflow.sql').read_text()
    definition=source.index('CREATE FUNCTION photo.seal_v2_graph(')
    boundary=source.index('ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) OWNER TO photo_fn_owner;',definition)
    assert boundary>definition
    assert 'REVOKE ALL ON FUNCTION photo.seal_v2_graph(uuid,uuid) FROM PUBLIC;' in source[boundary:]
    assert 'GRANT EXECUTE ON FUNCTION photo.seal_v2_graph(uuid,uuid) TO photo_scheduler;' in source[boundary:]
