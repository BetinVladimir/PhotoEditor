import importlib.util
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
def test_routed_migration_matches_authoritative_source():
    spec=importlib.util.spec_from_file_location('routed_generator',ROOT/'scripts/generate-billing-routed-workflow.py')
    gen=importlib.util.module_from_spec(spec);spec.loader.exec_module(gen)
    assert (ROOT/'backend/db/migrations/02604_billing_routed_apply.sql').read_text()==gen.render()
def test_manual_save_edits_skips_quota_but_styled_and_parent_share_one_admission():
    text=(ROOT/'backend/internal/run/apply.go').read_text()
    assert 'if !(route == "Run.SaveEdits" && identity)' in text
    assert 'billing.BindApply(ctx, tx, tid, who, runID, key, route, fingerprint)' in text
    assert text.count('billing.BindApply(')==1
