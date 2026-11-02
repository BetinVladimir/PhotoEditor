"""Privacy worker schema/function generation and auth-only ACL."""
import importlib.util
import json
from pathlib import Path
import shutil
import pytest
ROOT=Path(__file__).resolve().parents[2]
SPEC=importlib.util.spec_from_file_location('privacy_generator',ROOT/'scripts/generate-billing-deletion-preflight.py')
generator=importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(generator)

def test_generated_privacy_matches():
    for name,sql in generator.render().items():assert (ROOT/'backend/db/migrations'/name).read_text()==sql

@pytest.mark.parametrize('mutation',['schema','acl','configuration'])
def test_unsafe_privacy_contract_rejected(tmp_path,mutation):
    target=tmp_path/'docs/billing/contracts';target.mkdir(parents=True)
    for name in ('deletion-preflight-schema.sql','deletion-preflight-functions.sql','deletion-preflight-functions.json'):
        shutil.copyfile(ROOT/'docs/billing/contracts'/name,target/name)
    if mutation=='schema':
        p=target/'deletion-preflight-schema.sql';p.write_text(p.read_text()+'GRANT DELETE ON photo.billing_privacy TO photo_api;')
    elif mutation=='acl':
        p=target/'deletion-preflight-functions.json';data=json.loads(p.read_text());data['functions'][0]['execute_roles']=['photo_api'];p.write_text(json.dumps(data))
    else:
        p=target/'deletion-preflight-functions.sql';p.write_text(p.read_text().replace('SECURITY DEFINER','SECURITY INVOKER'))
    with pytest.raises(ValueError):generator.render(tmp_path)
