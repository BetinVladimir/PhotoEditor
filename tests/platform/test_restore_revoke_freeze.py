from types import SimpleNamespace
from uuid import uuid4
import ssl
import pytest
from photo_worker.contracts.base import parse_json
from photo_worker.runtime import restore_revoke_freeze as module


@pytest.mark.parametrize('mode', ['valid', 'success', 'unauthorized', 'unavailable', 'wrong_code',
    'replayed', 'oversized', 'compressed', 'network', 'scope', 'scope_false', 'activity_false'])
def test_actual_freeze_denial_is_bounded_native_and_never_retried(monkeypatch, mode):
    target = module.RestoreFreezeDenialTarget('https://restore.test', uuid4(), uuid4(), uuid4(), 7, 'fresh-key', 'Bearer secret')
    calls = []; closed = []
    class Connection:
        def __init__(self, *args, **kwargs): assert kwargs['timeout'] == 5
        def request(self, method, path, body, headers):
            calls.append(path)
            assert method == 'POST' and path.endswith(f'/assets/{target.asset_id}/v2-draft/freeze')
            assert parse_json(body) == dict(document_id=str(target.document_id), expected_revision='7', assume_profile=None)
            assert headers['Idempotency-Key'] == 'fresh-key' and headers['X-Photo-Client'] == 'native'
            if mode == 'network': raise OSError('secret transport')
        def getresponse(self):
            raw = b'{"code":"FORBIDDEN"}' if mode == 'wrong_code' else b'{"code":"REVISION_CONFLICT"}'
            if mode == 'oversized': raw = b'x' * 16385
            return SimpleNamespace(status={'success':200, 'unauthorized':401, 'unavailable':503}.get(mode,409),
                read=lambda _:raw, getheader=lambda name, default=None:
                'application/json' if name == 'Content-Type' else
                'true' if name == 'Idempotency-Replayed' and mode == 'replayed' else
                'gzip' if name == 'Content-Encoding' and mode == 'compressed' else default)
        def close(self): closed.append(True)
    monkeypatch.setattr(module.http.client, 'HTTPSConnection', Connection)
    def scope(actual):
        assert actual is target
        if mode == 'scope': raise RuntimeError('fixture unavailable')
        if mode == 'scope_false': return False
    def run():
        return module.observe_fixture_freeze_denied(target, tls_context=ssl.create_default_context(),
            assert_current_fixture=scope, check_active=lambda:False if mode == 'activity_false' else None)
    if mode == 'valid': assert run().document_id == target.document_id
    else:
        with pytest.raises((ValueError, RuntimeError)): run()
    assert len(calls) <= 1 and len(closed) == len(calls)
    if mode in ('scope_false', 'activity_false'): assert calls == []
    assert 'Bearer secret' not in repr(target)
