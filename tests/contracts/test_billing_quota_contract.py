import importlib.util
import json
from pathlib import Path
import shutil
import pytest

ROOT=Path(__file__).resolve().parents[2]
spec=importlib.util.spec_from_file_location('quota_generator',ROOT/'scripts/generate-billing-quota.py')
generator=importlib.util.module_from_spec(spec);spec.loader.exec_module(generator)

def test_quota_generated_files_are_current_and_separate_from_catalog():
    rendered=generator.render()
    assert rendered==generator.render()
    assert set(rendered)=={'02600_billing_quota_accounting.sql','02601_billing_quota_functions.sql'}
    for name,text in rendered.items():
        assert (ROOT/'backend/db/migrations'/name).read_text()==text
        assert text.count('-- +goose Up')==text.count('-- +goose Down')==1
        assert 'Forward-only billing quota migration' in text
        assert 'CREATE OR REPLACE' not in text
    source=json.loads((ROOT/'docs/billing/contracts/quota-tables.json').read_text())
    assert len(source['tables'])==11
    for table in source['tables']:
        assert table['rls']=='force_trusted_functions'
        assert 'tenant_id' in {col['name'] for col in table['columns']}
        assert 'DELETE' not in table['acl']['photo_fn_owner']
    functions=json.loads((ROOT/'docs/billing/contracts/quota-functions.json').read_text())
    assert len(functions['functions'])==11
    assert all(f['execute_roles']==[] for f in functions['functions'])

@pytest.mark.parametrize('mutation',['runtime_grant','missing_signature','sql_owner','column_type','rls'])
def test_unreviewed_contract_or_security_drift_is_rejected(tmp_path,mutation):
    dest=tmp_path/'docs/billing/contracts';dest.mkdir(parents=True)
    for name in ['quota-tables.json','quota-functions.json','quota-functions.sql']:
        shutil.copyfile(ROOT/'docs/billing/contracts'/name,dest/name)
    if mutation in ('runtime_grant','missing_signature'):
        p=dest/'quota-functions.json';data=json.loads(p.read_text())
        if mutation=='runtime_grant':data['functions'][0]['execute_roles']=['photo_api']
        else:data['functions'].pop()
        p.write_text(json.dumps(data))
    elif mutation=='sql_owner':
        p=dest/'quota-functions.sql';p.write_text(p.read_text().replace('SECURITY DEFINER SET search_path=pg_catalog,photo','SECURITY DEFINER',1))
    else:
        p=dest/'quota-tables.json';data=json.loads(p.read_text())
        if mutation=='column_type':data['tables'][0]['columns'][0]['type']='text; DROP TABLE photo.users'
        else:data['tables'][0]['rls']='disabled'
        p.write_text(json.dumps(data))
    with pytest.raises(ValueError):generator.render(tmp_path)
