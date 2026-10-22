import importlib.util,json,re
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
spec=importlib.util.spec_from_file_location('billing_workflow_generator',ROOT/'scripts/generate-billing-workflow.py')
gen=importlib.util.module_from_spec(spec);spec.loader.exec_module(gen)

def test_generated_workflow_migrations_and_acl_are_pinned():
    generated=gen.render();assert generated==gen.render()
    assert set(generated)=={'02602_billing_apply_bindings.sql','02603_billing_apply_workflow.sql'}
    for name,text in generated.items():
        assert (ROOT/'backend/db/migrations'/name).read_text()==text
        assert text.count('-- +goose Up')==text.count('-- +goose Down')==1
        assert 'Forward-only billing workflow migration' in text
    functions=json.loads((ROOT/'docs/billing/contracts/workflow-functions.json').read_text())['functions']
    api={f['signature'].split('(')[0] for f in functions if 'photo_api' in f['execute_roles']}
    assert api=={'billing_prepare_apply','billing_bind_apply'}
    assert all('photo_api' not in f['execute_roles'] for f in functions if 'settle' in f['signature'] or 'finalize' in f['signature'])

def test_standard_admission_hook_precedes_domain_locks():
    text=(ROOT/'backend/internal/run/apply.go').read_text()
    start=text.index('out, e = idempotency.Use(')
    assert start<text.index('billing.PrepareApply(',start)<text.index('var sourceName, state string',start)
    assert text.index('INSERT INTO photo.apply_builds',start)<text.index('billing.BindApply(',start)<text.index('accepted := ApplyAccepted',start)
