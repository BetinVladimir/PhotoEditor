from contextlib import contextmanager, nullcontext
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.provenance.installation import InstalledSigningOperator
from photo_worker.v2 import adoption_service, provenance_adoption_prepare
from photo_worker.v2.adoption import AdoptionInvalid, PreparedAdoption
from photo_worker.v2.provenance_adoption import PreparedSigningAdoption, VerifiedSigningResult


@pytest.mark.parametrize('mode', ['valid', 'unconfigured', 'rejected', 'recovered', 'cancel-after-sql'])
def test_signer_coordinator_uses_dedicated_atomic_commit(monkeypatch, tmp_path, mode):
    tenant, task, result, root = [uuid4() for _ in range(4)]
    events = []
    live = {'value': True}
    class Read:
        autocommit = True
        def execute(self, query, args):
            if 'v31_signing_adoption_committed' in query:
                row = (mode == 'recovered',)
            else:
                assert 'v2_adoption_context' in query
                row = (1, 2, result, 'provenance.sign', '1', root, 'a'*64)
            return SimpleNamespace(fetchone=lambda: row)
    class Write:
        def transaction(self):
            events.append('transaction')
            if mode == 'cancel-after-sql':
                @contextmanager
                def scoped():
                    try:
                        yield
                    except BaseException:
                        events.append('rollback')
                        raise
                return scoped()
            return nullcontext()
        def execute(self, query, args):
            assert query.startswith('SELECT photo.adopt_v31_signed(') and len(args) == 12
            assert args[:5] == (tenant, task, 1, 2, result)
            events.append('commit')
            if mode == 'cancel-after-sql':
                live['value'] = False
            return SimpleNamespace(fetchone=lambda: (True,))
    raw = canonical_bytes(dict(schema_version=1, id=str(uuid4()), revision='1',
        sdk_lock_sha='a'*64, key_handle='fixture', certificate_chain_sha='b'*64,
        timestamp_mode='off', timestamp_service_id=None, allowed_mime=['image/png'],
        trust_bundle_sha='c'*64, revocation_mode='off'))
    operator = InstalledSigningOperator([raw], lambda *args: True, object())
    sdk = object()
    def prepare(*args, **kwargs):
        assert args[6] is operator
        assert kwargs['sdk'] is sdk
        events.append('verify')
        if mode == 'rejected':
            raise AdoptionInvalid('SIGNING_PHYSICAL_VERIFICATION_FAILED')
        return PreparedSigningAdoption(VerifiedSigningResult(b'{}', b'{}', b'{}', b'{}', 'f'*64, 10),
                                       PreparedAdoption((), 'd'*64))
    monkeypatch.setattr(provenance_adoption_prepare, 'prepare_current_signing', prepare)
    coordinator = adoption_service.AdoptionCoordinator(Read(), Write(), object(), 'bucket',
        layer_scratch_root=tmp_path, layer_scratch_bytes=2 << 20,
        signing_operator=None if mode == 'unconfigured' else operator,
        signing_sdk=None if mode == 'unconfigured' else sdk,
        check_active=lambda: live['value'])
    if mode in ('valid', 'recovered'):
        assert coordinator.adopt(tenant, task) is True
        assert events == (['verify', 'transaction', 'commit'] if mode == 'valid' else [])
    else:
        with pytest.raises(AdoptionInvalid):
            coordinator.adopt(tenant, task)
        assert events == (['verify'] if mode == 'rejected' else
                          ['verify', 'transaction', 'commit', 'rollback'] if mode == 'cancel-after-sql' else [])


def test_signer_coordinator_false_activity_refuses_before_sql():
    class Read:
        autocommit = True
        def execute(self, *_args):
            raise AssertionError('cancelled adoption reached SQL')
    coordinator = adoption_service.AdoptionCoordinator(Read(), object(), object(), 'bucket',
        check_active=lambda: False)
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        coordinator.adopt(uuid4(), uuid4())
