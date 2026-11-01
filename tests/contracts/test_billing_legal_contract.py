"""Contract tests for billing legal group (LEG-00 scaffold; tables added by LEG-T* tasks)."""
import importlib.util
import json
import shutil
from pathlib import Path
import pytest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location(
    'legal_generator', ROOT / 'scripts/generate-billing-legal.py'
)
generator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(generator)


def test_legal_contracts_parse_and_generator_is_idempotent():
    rendered = generator.render()
    assert rendered == generator.render()
    source = json.loads((ROOT / 'docs/billing/contracts/legal-tables.json').read_text())
    assert source['schema_version'] == 1
    funcs = json.loads((ROOT / 'docs/billing/contracts/legal-functions.json').read_text())
    assert funcs['schema_version'] == 1


def test_legal_no_migration_while_tables_empty():
    """Scaffold criterion C: no migration file written when tables list is empty."""
    source = json.loads((ROOT / 'docs/billing/contracts/legal-tables.json').read_text())
    funcs = json.loads((ROOT / 'docs/billing/contracts/legal-functions.json').read_text())
    if not source['tables'] and not funcs['functions']:
        rendered = generator.render()
        assert rendered == {}, 'No migrations should be generated with empty contracts'
        for name in [generator.SCHEMA, generator.FUNCTIONS]:
            assert not (ROOT / 'backend/db/migrations' / name).exists(), \
                f'Migration {name} must not exist while contracts are empty'


def test_legal_generated_files_match_disk_when_nonempty():
    """When contracts have content, generated files must match disk."""
    rendered = generator.render()
    for name, content in rendered.items():
        path = ROOT / 'backend/db/migrations' / name
        assert path.exists(), f'Migration {name} missing on disk'
        assert path.read_text() == content, f'Migration {name} has drift'
        assert 'Forward-only billing legal migration' in content
        assert 'CREATE OR REPLACE' not in content
        assert content.count('-- +goose Up') == 1
        assert content.count('-- +goose Down') == 1


@pytest.mark.parametrize('mutation', ['runtime_grant', 'bad_rls', 'invalid_type'])
def test_security_drift_rejected(tmp_path, mutation):
    source = json.loads((ROOT / 'docs/billing/contracts/legal-tables.json').read_text())
    if not source['tables']:
        pytest.skip('No tables yet — drift tests run after LEG-T* tasks')
    dest = tmp_path / 'docs/billing/contracts'
    dest.mkdir(parents=True)
    for name in ['legal-tables.json', 'legal-functions.json', 'legal-functions.sql']:
        shutil.copyfile(ROOT / 'docs/billing/contracts' / name, dest / name)
    p = dest / 'legal-tables.json'
    data = json.loads(p.read_text())
    if mutation == 'bad_rls':
        data['tables'][0]['rls'] = 'disabled'
    elif mutation == 'invalid_type':
        data['tables'][0]['columns'][0]['type'] = 'text; DROP TABLE photo.users'
    elif mutation == 'runtime_grant':
        funcs_data = json.loads((dest / 'legal-functions.json').read_text())
        if not funcs_data['functions']:
            pytest.skip('No functions yet')
        funcs_data['functions'][0]['execute_roles'] = ['photo_worker']
        (dest / 'legal-functions.json').write_text(json.dumps(funcs_data))
    p.write_text(json.dumps(data))
    with pytest.raises((ValueError, SystemExit)):
        generator.render(tmp_path)


def test_legal_schema_migration_exists():
    """LEG-T*: schema migration generated and present on disk."""
    import json as _json
    source = _json.loads((ROOT / 'docs/billing/contracts/legal-tables.json').read_text())
    if not source['tables']:
        pytest.skip('No tables yet')
    path = ROOT / 'backend/db/migrations/02666_billing_legal_schema.sql'
    assert path.exists(), 'Legal schema migration must exist'
    text = path.read_text()
    assert '-- +goose Up' in text
    assert 'Forward-only billing legal migration' in text
    assert 'CREATE OR REPLACE' not in text
    expected = ['legal_document_versions', 'privacy_retention_policies',
                'privacy_erasure_tombstones', 'privacy_retained_records']
    for t in expected:
        assert f'photo.{t}' in text


def test_legal_document_versions_constraints():
    """LEG-T01: document_key enum, state, UNIQUE, content_sha256 constraints."""
    source = json.loads((ROOT / 'docs/billing/contracts/legal-tables.json').read_text())
    if not source['tables']:
        pytest.skip('No tables yet')
    t = next(t for t in source['tables'] if t['name'] == 'legal_document_versions')
    constraints = ' '.join(t['constraints'])
    assert 'UNIQUE(document_key,locale,version)' in t['constraints']
    assert 'terms' in constraints  # document_key enum
    assert any('content_sha256' in c for c in t['constraints'])
    assert any('archive_ref' in c for c in t['constraints'])
    assert t.get('append_only') is False


def test_privacy_retention_policies_constraints():
    """LEG-T04: approved policies require retention_days and backup_max_days."""
    source = json.loads((ROOT / 'docs/billing/contracts/legal-tables.json').read_text())
    if not source['tables']:
        pytest.skip('No tables yet')
    t = next(t for t in source['tables'] if t['name'] == 'privacy_retention_policies')
    assert any('UNIQUE(code,version)' in c for c in t['constraints'])
    # approved must have non-null retention_days (R8: no unlimited NULL for approved)
    assert any('approved' in c and 'retention_days' in c for c in t['constraints'])
    assert any('approved' in c and 'backup_max_days' in c for c in t['constraints'])


def test_privacy_erasure_tombstones_append_only():
    """LEG-T02: erasure tombstones are append-only (no UPDATE, no DELETE)."""
    source = json.loads((ROOT / 'docs/billing/contracts/legal-tables.json').read_text())
    if not source['tables']:
        pytest.skip('No tables yet')
    t = next(t for t in source['tables'] if t['name'] == 'privacy_erasure_tombstones')
    assert t.get('append_only') is True
    assert 'UPDATE' not in t['acl'].get('photo_fn_owner', [])


def test_privacy_retained_records_fk_to_policy():
    """LEG-T03: retained_records references privacy_retention_policies."""
    source = json.loads((ROOT / 'docs/billing/contracts/legal-tables.json').read_text())
    if not source['tables']:
        pytest.skip('No tables yet')
    t = next(t for t in source['tables'] if t['name'] == 'privacy_retained_records')
    assert any('privacy_retention_policies' in c for c in t['constraints'])


def test_legal_tables_have_subject_identifier_and_no_delete():
    source = json.loads((ROOT / 'docs/billing/contracts/legal-tables.json').read_text())
    for table in source['tables']:
        col_names = {c['name'] for c in table['columns']}
        has_id = bool(col_names & {'tenant_id', 'subject_ref', 'document_key', 'code'})
        assert has_id, f'{table["name"]} must have tenant_id, subject_ref, document_key or code'
        assert 'DELETE' not in table['acl'].get('photo_fn_owner', []), \
            f'{table["name"]} must not grant DELETE'
        assert table['rls'] == 'force_trusted_functions', \
            f'{table["name"]} must use force_trusted_functions RLS'
