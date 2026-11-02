"""Outbox worker schema/function generation and maintenance-only ACL."""
import importlib.util
import json
from pathlib import Path
import shutil
import pytest
ROOT=Path(__file__).resolve().parents[2]
SPEC=importlib.util.spec_from_file_location('outbox_generator',ROOT/'scripts/generate-billing-stripe-inbox.py')
generator=importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(generator)

def test_generated_outbox_matches():
    for name,sql in generator.render().items():assert (ROOT/'backend/db/migrations'/name).read_text()==sql

@pytest.mark.parametrize('mutation',['schema','acl','configuration'])
def test_unsafe_outbox_contract_rejected(tmp_path,mutation):
    target=tmp_path/'docs/billing/contracts';target.mkdir(parents=True)
    for name in ('stripe-inbox-schema.sql','stripe-inbox-functions.sql','stripe-inbox-functions.json'):
        shutil.copyfile(ROOT/'docs/billing/contracts'/name,target/name)
    if mutation=='schema':
        p=target/'stripe-inbox-schema.sql';p.write_text(p.read_text()+'GRANT DELETE ON photo.billing_outbox TO photo_api;')
    elif mutation=='acl':
        p=target/'stripe-inbox-functions.json';data=json.loads(p.read_text());data['functions'][0]['execute_roles']=['photo_api'];p.write_text(json.dumps(data))
    else:
        p=target/'stripe-inbox-functions.sql';p.write_text(p.read_text().replace('SECURITY DEFINER','SECURITY INVOKER'))
    with pytest.raises(ValueError):generator.render(tmp_path)
