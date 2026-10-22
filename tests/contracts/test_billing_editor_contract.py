import importlib.util,json,re
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]

def test_editor_migration_and_function_manifest():
    spec=importlib.util.spec_from_file_location('editor_generator',ROOT/'scripts/generate-billing-editor.py');g=importlib.util.module_from_spec(spec);spec.loader.exec_module(g)
    assert (ROOT/'backend/db/migrations/02606_billing_editor_workflow.sql').read_text()==g.render()
    source=(ROOT/'docs/billing/contracts/editor-workflow.sql').read_text()
    signatures={name+'('+','.join(a.strip().split()[1] for a in args.split(',') if a.strip())+')' for name,args in re.findall(r'CREATE (?:OR REPLACE )?FUNCTION photo\.([a-z0-9_]+)\(([^)]*)\)',source)}
    manifest=json.loads((ROOT/'docs/billing/contracts/editor-functions.json').read_text())['functions']
    assert signatures=={f['signature'] for f in manifest}
    assert 'DEFAULT false' in source and 'INSERT INTO photo.billing_editor_controls' not in source
    assert "workflow_kind<>'editor_effects'" in source
    for f in manifest:
        assert set(f['execute_roles']) <= {'photo_api','photo_scheduler'}
        assert 'REVOKE ALL ON FUNCTION photo.'+f['signature']+' FROM PUBLIC;' in source

def test_editor_admission_order_and_coordinator_hook():
    source=(ROOT/'backend/internal/run/apply.go').read_text()
    prep=source.index('billing.PrepareEditor(')
    assert prep<source.index('editorMode.authorize(ctx, tx, who, tid, true)',prep)
    assert source.index('editorMode.commit(ctx, tx, who, tid, accepted)')<source.index('billing.BindEditor(')
    assert 'billable && editorMode == nil' in source
    assert 'billing_settle_next_editor()' in (ROOT/'backend/internal/workflow/editor_effect_seal_coordinator.go').read_text()
    mode=(ROOT/'backend/internal/run/editor_effect_mode.go').read_text()
    assert mode.index('INSERT INTO photo.snapshot_items')<mode.index('UPDATE photo.snapshots SET expires_at=NULL')


def test_seal_wrapper_preserves_scheduler_only_definer_boundary():
    source=(ROOT/'docs/billing/contracts/editor-workflow.sql').read_text()
    definition=source.index('CREATE FUNCTION photo.seal_v2_graph(')
    boundary=source.index('ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) OWNER TO photo_fn_owner;',definition)
    assert boundary>definition
    assert 'REVOKE ALL ON FUNCTION photo.seal_v2_graph(uuid,uuid) FROM PUBLIC;' in source[boundary:]
    assert 'GRANT EXECUTE ON FUNCTION photo.seal_v2_graph(uuid,uuid) TO photo_scheduler;' in source[boundary:]


def test_editor_generator_keeps_dynamic_sql_inside_manual_block_opaque():
    spec=importlib.util.spec_from_file_location('editor_generator_manual',ROOT/'scripts/generate-billing-editor.py')
    generator=importlib.util.module_from_spec(spec);spec.loader.exec_module(generator)
    manual="""-- +goose StatementBegin
DO $bridge$ BEGIN EXECUTE $create$CREATE FUNCTION photo.hidden() RETURNS boolean LANGUAGE plpgsql AS $native$BEGIN RETURN true; END $native$;$create$; END $bridge$;
-- +goose StatementEnd
"""
    ordinary="CREATE FUNCTION photo.visible() RETURNS boolean LANGUAGE plpgsql AS $$\nBEGIN RETURN true;\nEND $$;\n"
    output=generator.render('-- +goose Up\n'+manual+ordinary)
    assert manual in output
    assert output.count('-- +goose StatementBegin')==3
    assert output.count('-- +goose StatementEnd')==3
    depth=0
    for line in output.splitlines():
        if line=='-- +goose StatementBegin':
            assert depth==0;depth=1
        elif line=='-- +goose StatementEnd':
            assert depth==1;depth=0
    assert depth==0


def test_editor_generator_rejects_invalid_manual_boundaries():
    import pytest
    spec=importlib.util.spec_from_file_location('editor_generator_invalid',ROOT/'scripts/generate-billing-editor.py')
    generator=importlib.util.module_from_spec(spec);spec.loader.exec_module(generator)
    for body in ['-- +goose StatementEnd\n','-- +goose StatementBegin\n','-- +goose StatementBegin\n-- +goose StatementBegin\n-- +goose StatementEnd\n','  -- +goose StatementBegin\n']:
        with pytest.raises(ValueError):generator.render(body)


def test_editor_native_identity_alias_is_internal_only():
    source=(ROOT/'docs/billing/contracts/editor-workflow.sql').read_text()
    signature='lock_editor_effect_identity_before_billing(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,bigint,uuid,uuid)'
    functions=json.loads((ROOT/'docs/billing/contracts/editor-functions.json').read_text())['functions']
    assert next(f for f in functions if f['signature']==signature)['execute_roles']==[]
    assert 'REVOKE ALL ON FUNCTION photo.'+signature+' FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_auth,photo_portal,photo_maintenance;' in source
    assert 'PERFORM id FROM photo.users WHERE id=p_actor FOR SHARE;' in source
    assert 'PERFORM id FROM photo.sessions WHERE id=p_session AND user_id=p_actor FOR SHARE;' in source
