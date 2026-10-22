import importlib.util
import json
from pathlib import Path
import pytest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('billing_generator', ROOT/'scripts/generate-billing-catalog.py')
generator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(generator)

def test_additive_migrations_are_deterministic_and_current():
    first = generator.render()
    assert first == generator.render()
    for name, content in first.items():
        assert (ROOT/'backend/db/migrations'/name).read_text() == content
        assert name.startswith(('02598_', '02599_'))
        assert content.count('-- +goose Up') == content.count('-- +goose Down') == 1
        assert 'Forward-only billing migration' in content
        assert 'CREATE OR REPLACE' not in content

def test_seed_scope_no_external_accounts_and_closed_activation():
    catalog, seed = generator.load_contracts()
    assert len(catalog['tables']) == 10
    for table, rows in seed['rows'].items():
        assert len({r['id'] for r in rows}) == len(rows)
        assert all(r.get('catalog_scope', 'sandbox') == 'sandbox' for r in rows)
    assert seed['rows']['billing_provider_accounts'] == seed['rows']['billing_provider_prices'] == []
    assert seed['rows']['billing_operation_policies'] == []
    assert all(r['status']=='draft' for r in seed['rows']['billing_plan_versions'])
    assert not any(r['is_active'] for r in seed['rows']['billing_topup_offers'])
    assert not any(r['self_service_enabled'] for r in seed['rows']['billing_plans'])

@pytest.mark.parametrize('value', ['x; DROP TABLE photo.users', 'Upper', '', 'a.b', 'x"'])
def test_invalid_identifiers_rejected(value):
    with pytest.raises(ValueError): generator.identifier(value)

@pytest.mark.parametrize('value', [2**63, -(2**63)-1, {}, ['x'], 'x\x00y'])
def test_unrepresentable_seed_values_rejected(value):
    with pytest.raises(ValueError): generator.literal(value)

def test_literal_round_trip_source_escaping():
    assert generator.literal("a'b\\c") == "E'a''b\\\\c'"
    assert generator.literal(False) == 'false'
    assert generator.literal(None) == 'NULL'
