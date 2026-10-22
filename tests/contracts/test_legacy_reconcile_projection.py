"""Authored current-source contract scenarios; not executed in coding phase."""
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PATH = '/api/v1/tenants/{t}/legacy-imports/reconcile'

def test_legacy_reconciliation_remains_one_existing_editor_operation():
    for relative in ('docs/implementation-v2/contracts/endpoints.json', 'packages/contracts/v2-endpoints.json', 'packages/contracts/endpoints.json'):
        rows=json.loads((ROOT/relative).read_text())['endpoints']
        matches=[r for r in rows if r['method']=='POST' and r['path']==PATH]
        assert len(matches)==1
        row=matches[0]
        assert (row['role'],row['success_status'],row['idempotency'])==('editor',200,False)
        assert row['request_schema']=='LegacyImportReconcile' and row['response_schema']=='LegacyImportRecord'
    document=json.loads((ROOT/'docs/implementation-v2/contracts/openapi.json').read_text())
    operation=document['paths'][PATH]['post']
    assert operation['x-role']=='editor' and operation['x-web-mutation-csrf'] is True
    assert set(operation['responses'])=={'200'}
    assert all(p['name'] not in ('Idempotency-Key','If-Match') for p in operation['parameters'])

def test_legacy_reconciliation_preserves_natural_identity_and_original_provenance():
    defs=json.loads((ROOT/'docs/implementation-v2/contracts/schemas.json').read_text())['$defs']
    request,response=defs['LegacyImportReconcile'],defs['LegacyImportRecord']
    assert request['additionalProperties'] is False and response['additionalProperties'] is False
    assert set(request['required'])=={'plan_hash','source_relative','source_sha256','upload_id'}
    assert request['properties']['provenance']['type']=='object'
    assert set(response['required'])=={'id','plan_hash','source_relative','source_sha256','asset_version_id','upload_id','provenance'}
