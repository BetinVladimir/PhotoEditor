"""Privacy worker schema/function generation and auth-only ACL."""
import importlib.util
import json
from pathlib import Path
import shutil
import pytest
ROOT=Path(__file__).resolve().parents[2]
SPEC=importlib.util.spec_from_file_location('privacy_generator',ROOT/'scripts/generate-billing-recent-auth-verifier.py')
generator=importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(generator)

def test_generated_privacy_matches():
    for name,sql in generator.render().items():assert (ROOT/'backend/db/migrations'/name).read_text()==sql

@pytest.mark.parametrize('mutation',['schema','acl','configuration'])
def test_unsafe_privacy_contract_rejected(tmp_path,mutation):
    target=tmp_path/'docs/billing/contracts';target.mkdir(parents=True)
    for name in ('recent-auth-verifier-schema.sql','recent-auth-verifier-functions.sql','recent-auth-verifier-functions.json'):
        shutil.copyfile(ROOT/'docs/billing/contracts'/name,target/name)
    if mutation=='schema':
        p=target/'recent-auth-verifier-schema.sql';p.write_text(p.read_text()+'GRANT DELETE ON photo.billing_privacy TO photo_api;')
    elif mutation=='acl':
        p=target/'recent-auth-verifier-functions.json';data=json.loads(p.read_text());data['functions'][0]['execute_roles']=['photo_api'];p.write_text(json.dumps(data))
    else:
        p=target/'recent-auth-verifier-functions.sql';p.write_text(p.read_text().replace('SECURITY DEFINER','SECURITY INVOKER'))
    with pytest.raises(ValueError):generator.render(tmp_path)


def test_consumer_is_not_executable_by_any_runtime_role(tmp_path):
    target=tmp_path/'docs/billing/contracts';target.mkdir(parents=True)
    for name in ('recent-auth-verifier-schema.sql','recent-auth-verifier-functions.sql','recent-auth-verifier-functions.json'):
        shutil.copyfile(ROOT/'docs/billing/contracts'/name,target/name)
    path=target/'recent-auth-verifier-functions.json';data=json.loads(path.read_text())
    data['functions'][-1]['execute_roles']=['photo_auth'];path.write_text(json.dumps(data))
    with pytest.raises(ValueError):generator.render(tmp_path)
    sql=generator.render()[generator.FUNCTIONS]
    assert 'GRANT EXECUTE ON FUNCTION photo.account_consume_recent_auth' not in sql
