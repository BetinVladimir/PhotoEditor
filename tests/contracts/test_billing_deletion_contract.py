"""Deletion worker migrations are additive, bounded and restricted to maintenance."""
import importlib.util
import json
from pathlib import Path
import shutil
import pytest

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('deletion_generator', ROOT / 'scripts/generate-billing-deletion.py')
generator = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(generator)


def test_generated_deletion_contract_matches_new_migrations():
    outputs = generator.render()
    assert set(outputs) == {'02669_billing_deletion_steps.sql', '02670_billing_deletion_step_functions.sql'}
    for name, body in outputs.items():
        assert (ROOT / 'backend/db/migrations' / name).read_text() == body
        assert 'CREATE OR REPLACE' not in body
        assert 'FORWARD_ONLY' in body or 'Forward-only' in body
    assert 'SKIP LOCKED' in outputs[generator.FUNCTIONS]
    assert 'DELETION_FENCE_STALE' in outputs[generator.FUNCTIONS]


@pytest.mark.parametrize('mutation', ['function_acl', 'table_acl', 'table_type', 'security_definer'])
def test_generator_rejects_privilege_and_type_drift(tmp_path, mutation):
    target = tmp_path / 'docs/billing/contracts'
    target.mkdir(parents=True)
    for suffix in ('tables.json', 'functions.json', 'functions.sql'):
        shutil.copyfile(ROOT / 'docs/billing/contracts' / ('deletion-steps-' + suffix), target / ('deletion-steps-' + suffix))
    if mutation in ('function_acl', 'table_acl', 'table_type'):
        path = target / ('deletion-steps-functions.json' if mutation == 'function_acl' else 'deletion-steps-tables.json')
        value = json.loads(path.read_text())
        if mutation == 'function_acl': value['functions'][0]['execute_roles'] = ['photo_api']
        elif mutation == 'table_acl': value['tables'][0]['acl']['photo_api'] = ['DELETE']
        else: value['tables'][0]['columns'][0]['type'] = 'text; DROP TABLE photo.users'
        path.write_text(json.dumps(value))
    else:
        path = target / 'deletion-steps-functions.sql'
        path.write_text(path.read_text().replace('SECURITY DEFINER', 'SECURITY INVOKER', 1))
    with pytest.raises(ValueError): generator.render(tmp_path)
