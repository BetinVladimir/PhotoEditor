"""Billing transport projection preserves the real authentication scopes."""
import importlib.util
import json
from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]
SPEC=importlib.util.spec_from_file_location('billing_openapi_projection',ROOT/'scripts/sync-v2-openapi.py')
projection=importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(projection)


def rows():
    paths={'/api/v1/billing/catalog','/api/v1/legal/requirements','/api/v1/webhooks/stripe/{account}','/api/v1/me/deletion/{request}/status','/api/v1/me/privacy','/api/v1/me/deletion/preflight'}
    result=[r for r in json.loads((ROOT/'docs/implementation-v2/contracts/endpoints.json').read_text())['endpoints'] if r['path'] in paths]
    assert len(result)==6
    assert all(r.get('role') for r in result)
    return result


def test_stripe_signature_and_status_capability_have_separate_security():
    result=projection.synchronize({},rows())
    stripe=result['paths']['/api/v1/webhooks/stripe/{account}']['post']
    status=result['paths']['/api/v1/me/deletion/{request}/status']['get']
    assert stripe['security']==[{'stripeSignature':[]}]
    assert status['security']==[{'deletionStatusBearer':[]}]
    assert stripe['x-web-mutation-csrf'] is False
    assert status['x-web-mutation-csrf'] is False
    assert result['paths']['/api/v1/billing/catalog']['get']['security']==[]
    assert result['components']['securitySchemes']['stripeSignature']['name']=='Stripe-Signature'
    assert projection.synchronize(result,rows())==result


def test_privacy_and_preflight_headers_match_the_working_handlers():
    result=projection.synchronize({},rows())
    privacy=result['paths']['/api/v1/me/privacy']['patch']
    preflight=result['paths']['/api/v1/me/deletion/preflight']['post']
    assert privacy['security']==[{'userBearer':[]}]
    assert privacy['x-web-mutation-csrf'] is True
    headers={p['name']:p for p in privacy['parameters'] if p['in']=='header'}
    assert headers['If-Match']['required'] is True
    assert headers['Idempotency-Key']['required'] is True
    preflight_headers={p['name'] for p in preflight['parameters'] if p['in']=='header'}
    assert preflight_headers=={'Idempotency-Key'}
    catalog=result['paths']['/api/v1/billing/catalog']['get']
    assert {p['name'] for p in catalog['parameters'] if p['in']=='query'}=={'locale','currency'}


def test_published_billing_openapi_has_no_projection_drift():
    current=json.loads((ROOT/'docs/implementation-v2/contracts/openapi.json').read_text())
    assert projection.synchronize(current,rows())==current
