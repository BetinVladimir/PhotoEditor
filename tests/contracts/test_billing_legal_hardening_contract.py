"""The correction is generated and guards the exact function catalog preimage."""
import importlib.util
import json
from pathlib import Path
import shutil
import pytest

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('legal_hardening_generator', ROOT / 'scripts/generate-billing-legal-hardening.py')
generator = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(generator)


def test_generated_legal_correction_matches():
    for name, sql in generator.render().items():
        assert (ROOT / 'backend/db/migrations' / name).read_text() == sql
        assert 'to_jsonb(actual)' in sql
        assert 'aclexplode' in sql
        assert 'LEGAL_ACCEPTANCE_PREIMAGE_CHANGED' in sql


@pytest.mark.parametrize('mutation', ['body', 'owner', 'role', 'digest'])
def test_correction_rejects_manifest_or_source_drift(tmp_path, mutation):
    target = tmp_path / 'docs/billing/contracts'
    target.mkdir(parents=True)
    for suffix in ('json', 'sql'):
        shutil.copyfile(ROOT / 'docs/billing/contracts' / ('legal-hardening-functions.' + suffix), target / ('legal-hardening-functions.' + suffix))
    if mutation == 'body':
        path = target / 'legal-hardening-functions.sql'
        path.write_text(path.read_text().replace('LEGAL_NO_EVENTS', 'LEGAL_CHANGED'))
    else:
        path = target / 'legal-hardening-functions.json'
        data = json.loads(path.read_text())
        if mutation == 'owner': data['functions'][0]['owner'] = 'photo_api'
        elif mutation == 'role': data['functions'][0]['execute_roles'] = ['photo_api']
        else: data['functions'][0]['preimage_sha256'] = 'invalid'
        path.write_text(json.dumps(data))
    with pytest.raises(ValueError): generator.render(tmp_path)
