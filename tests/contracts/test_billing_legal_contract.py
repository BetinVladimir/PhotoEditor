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
        for name in [generator.SCHEMA, generator.AUTH_SCHEMA, generator.FUNCTIONS]:
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
    source = json.loads((ROOT / 'docs/billing/contracts/legal-tables.json').read_text())
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


def test_legal_auth_schema_migration_exists():
    """LEG-T05: auth junction tables migration generated and present on disk."""
    source = json.loads((ROOT / 'docs/billing/contracts/legal-tables.json').read_text())
    auth_tables = [t for t in source['tables']
                   if t.get('schema_file') == generator.AUTH_SCHEMA]
    if not auth_tables:
        pytest.skip('No auth tables yet')
    path = ROOT / 'backend/db/migrations' / generator.AUTH_SCHEMA
    assert path.exists(), f'Auth tables migration {generator.AUTH_SCHEMA} missing'
    text = path.read_text()
    assert '-- +goose Up' in text
    assert 'Forward-only billing legal migration' in text
    assert 'CREATE OR REPLACE' not in text
    for t in auth_tables:
        assert f"photo.{t['name']}" in text


def test_legal_auth_tables_constraints():
    """LEG-T05: auth tables have correct PK, FK, state enums and hash checks."""
    source = json.loads((ROOT / 'docs/billing/contracts/legal-tables.json').read_text())
    by_name = {t['name']: t for t in source['tables']}

    for tname in ['account_onboarding_states', 'account_recent_auth_grants',
                  'privacy_cases', 'profile_deletion_requests']:
        if tname not in by_name:
            pytest.skip(f'Table {tname} not yet defined')

    ons = by_name['account_onboarding_states']
    assert ons['primary_key'] == ['user_id']
    ons_constr = ' '.join(ons['constraints'])
    assert 'complete' in ons_constr and 'reacceptance_required' in ons_constr
    assert any('proof' in c or 'required_document_set_hash' in c for c in ons['constraints'])

    arg = by_name['account_recent_auth_grants']
    assert 'grant_id' in [c['name'] for c in arg['columns']]
    assert any('delete_profile' in c for c in arg['constraints'])
    assert any('proof_hash' in c for c in arg['constraints'])

    pc = by_name['privacy_cases']
    assert 'case_id' in [c['name'] for c in pc['columns']]
    assert any('erasure' in c for c in pc['constraints'])

    pdr = by_name['profile_deletion_requests']
    assert any('privacy_cases' in c for c in pdr['constraints'])
    assert any('partially_retained' in c for c in pdr['constraints'])


def test_legal_tables_schema_file_field():
    """LEG-T05: every table has a schema_file field and no unknown migration targets."""
    allowed = {generator.SCHEMA, generator.AUTH_SCHEMA}
    source = json.loads((ROOT / 'docs/billing/contracts/legal-tables.json').read_text())
    for t in source['tables']:
        sf = t.get('schema_file')
        assert sf is not None, f'{t["name"]} missing schema_file'
        assert sf in allowed, f'{t["name"]} schema_file {sf!r} is not a known migration'


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


def test_legal_functions_acl_json_matches_grant():
    """LEG-F01: execute_roles in JSON must exactly equal GRANT EXECUTE in migration SQL."""
    funcs = json.loads((ROOT / 'docs/billing/contracts/legal-functions.json').read_text())
    if not funcs['functions']:
        pytest.skip('No functions yet')
    migration = ROOT / 'backend/db/migrations' / generator.FUNCTIONS
    assert migration.exists(), f'Functions migration {generator.FUNCTIONS} missing'
    text = migration.read_text()
    for f in funcs['functions']:
        sig = f['signature']
        roles = set(f.get('execute_roles', []))
        # Check each expected role has a GRANT line
        for role in roles:
            assert f'GRANT EXECUTE ON FUNCTION photo.{sig} TO {role};' in text, \
                f'Missing GRANT for {sig} → {role}'
        # Check REVOKE ALL is present (R16)
        assert f'REVOKE ALL ON FUNCTION photo.{sig} FROM PUBLIC;' in text, \
            f'Missing REVOKE ALL for {sig}'
        # Check no extra grants exist for this signature (grant matrix must match exactly)
        import re as _re
        actual_grants = set(_re.findall(
            rf'GRANT EXECUTE ON FUNCTION photo\.{_re.escape(sig)} TO (\w+);', text))
        assert actual_grants == roles, \
            f'Grant mismatch for {sig}: JSON={roles}, SQL={actual_grants}'


def test_legal_functions_have_security_definer():
    """LEG-F01: all functions in SQL have SECURITY DEFINER SET search_path (R16)."""
    funcs = json.loads((ROOT / 'docs/billing/contracts/legal-functions.json').read_text())
    if not funcs['functions']:
        pytest.skip('No functions yet')
    src = (ROOT / 'docs/billing/contracts/legal-functions.sql').read_text()
    for f in funcs['functions']:
        name = f['signature'].split('(')[0]
        assert 'SECURITY DEFINER SET search_path=pg_catalog,photo' in src, \
            f'Function {name} missing SECURITY DEFINER'


def test_legal_tables_have_subject_identifier_and_no_delete():
    source = json.loads((ROOT / 'docs/billing/contracts/legal-tables.json').read_text())
    SUBJECT_COLS = {'tenant_id', 'subject_ref', 'document_key', 'code', 'user_id', 'privacy_case_id'}
    for table in source['tables']:
        col_names = {c['name'] for c in table['columns']}
        has_id = bool(col_names & SUBJECT_COLS)
        assert has_id, f'{table["name"]} must have a subject identifier column'
        assert 'DELETE' not in table['acl'].get('photo_fn_owner', []), \
            f'{table["name"]} must not grant DELETE'
        assert table['rls'] == 'force_trusted_functions', \
            f'{table["name"]} must use force_trusted_functions RLS'
