from types import SimpleNamespace
from uuid import uuid4, uuid5, NAMESPACE_OID
import ssl
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_revoke_freeze as module


@pytest.mark.parametrize('mode', ['valid', 'foreign_frozen', 'foreign_draft', 'foreign_source',
    'wrong_revision', 'extra_field', 'invalid_recipe', 'replay', 'denial', 'network'])
def test_freeze_baseline_validates_actual_result_and_excludes_saved_replay(monkeypatch, mode):
    target = module.RestoreFreezeDenialTarget('https://restore.test', uuid4(), uuid4(), uuid4(), 7, 'baseline-key', 'Bearer private')
    binding = module.RestoreFreezeBaselineBinding(uuid4(), uuid4(), uuid4(), 'a'*64, 'b'*64)
    frozen = uuid5(NAMESPACE_OID, f'v26-render-freeze/{target.tenant_id}/{binding.actor_id}/{target.asset_id}/baseline-key')
    result = dict(document_id=str(frozen), draft_document_id=str(target.document_id), draft_revision='7',
        source=dict(asset_version_id=str(binding.asset_version_id), source=dict(artifact_id=str(binding.source_artifact_id),
            sha256='a'*64), canvas_hash='b'*64), recipe=dict(artifact_id=str(uuid4()), sha256='c'*64))
    if mode == 'foreign_frozen': result['document_id'] = str(uuid4())
    if mode == 'foreign_draft': result['draft_document_id'] = str(uuid4())
    if mode == 'foreign_source': result['source']['source']['sha256'] = 'd'*64
    if mode == 'wrong_revision': result['draft_revision'] = '8'
    if mode == 'extra_field': result['extra'] = True
    if mode == 'invalid_recipe': result['recipe']['artifact_id'] = 'invalid'
    calls = []; closed = []
    class Connection:
        def __init__(self, *args, **kwargs): pass
        def request(self, method, path, body, headers):
            calls.append(path)
            assert method == 'POST' and headers['Idempotency-Key'] == 'baseline-key'
            if mode == 'network': raise OSError('private')
        def getresponse(self):
            return SimpleNamespace(status=409 if mode == 'denial' else 200, read=lambda _:canonical_bytes(result),
                getheader=lambda name, default=None: 'application/json' if name == 'Content-Type' else
                'true' if mode == 'replay' and name == 'Idempotency-Replayed' else default)
        def close(self): closed.append(True)
    monkeypatch.setattr(module.http.client, 'HTTPSConnection', Connection)
    def run():
        return module.observe_fixture_freeze_baseline(target, binding, tls_context=ssl.create_default_context(),
            assert_current_fixture=lambda actual: actual is target, check_active=lambda:None)
    if mode == 'valid':
        evidence = run()
        assert evidence.frozen_document_id == frozen and evidence.recipe_sha256 == 'c'*64
    else:
        with pytest.raises(ValueError): run()
    assert len(calls) == 1 and closed == [True]
